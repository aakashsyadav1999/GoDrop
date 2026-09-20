package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/aakash/godrop/internal/api"
	"github.com/aakash/godrop/internal/config"
	"github.com/aakash/godrop/internal/database"
	"github.com/aakash/godrop/internal/job"
	"github.com/aakash/godrop/internal/logging"
	"github.com/aakash/godrop/internal/pool"
	"github.com/aakash/godrop/internal/processor"
	"github.com/aakash/godrop/internal/store"
	"github.com/joho/godotenv"
	"github.com/redis/go-redis/v9"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func run() error {
	// Optional .env for local development. Variables already set in the real
	// environment win, and a missing file is fine.
	_ = godotenv.Load()

	cfg, err := config.Load(os.Args[1:], os.Getenv)
	if errors.Is(err, flag.ErrHelp) {
		return nil
	}
	if err != nil {
		return err
	}

	storeKind := "memory"
	logger, err := logging.New(os.Stderr, cfg.LogLevel, cfg.LogFormat)
	if err != nil {
		return err
	}
	slog.SetDefault(logger)

	var jobStore store.JobStore = store.NewMemoryStore()
	if cfg.RedisAddr != "" {
		rdb := redis.NewClient(&redis.Options{Addr: cfg.RedisAddr})
		defer rdb.Close()

		pingCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		err := rdb.Ping(pingCtx).Err()
		cancel()
		if err != nil {
			return fmt.Errorf("connect to redis at %s: %w", cfg.RedisAddr, err)
		}
		jobStore = store.NewRedisStore(rdb, cfg.JobTTL)
		storeKind = "redis"
	}

	var history store.HistoryStore = store.NopHistory{}
	if cfg.PostgresDSN != "" {
		db, err := database.Open(context.Background(), cfg.PostgresDSN)
		if err != nil {
			return fmt.Errorf("connect to postgres: %w", err) // the DSN holds a password, so it is never logged
		}
		defer db.Close()

		if err := database.Migrate(context.Background(), db); err != nil {
			return err
		}
		history = database.NewHistory(db)
	}

	// The Fetcher takes a string; the pool carries URLJobs. ProcessorFunc adapts one to the other.
	fetcher := processor.NewFetcher()
	fetchURL := pool.ProcessorFunc[job.URLJob, processor.Page](
		func(ctx context.Context, j job.URLJob) (processor.Page, error) {
			return fetcher.Process(ctx, j.URL)
		},
	)

	p := pool.New(fetchURL, cfg.Workers, cfg.QueueSize, cfg.JobTimeout)
	p.Start(context.Background())

	srv := api.NewServer(jobStore, history, p)

	consumerDone := make(chan struct{})
	go func() {
		defer close(consumerDone)
		srv.ConsumeResults()
	}()

	limiterCtx, stopLimiter := context.WithCancel(context.Background())
	defer stopLimiter()

	var handler http.Handler = srv.Routes()
	if cfg.RateLimit > 0 {
		handler = api.RateLimit(limiterCtx, api.RateLimitConfig{
			RPS:        cfg.RateLimit,
			Burst:      cfg.RateBurst,
			TrustProxy: cfg.TrustProxy,
		}, handler)
	}
	handler = api.RequestLogger(logger, handler)

	httpServer := &http.Server{
		Addr:              cfg.Addr,
		Handler:           handler,
		ReadHeaderTimeout: 5 * time.Second,
	}

	stopCtx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	serverErr := make(chan error, 1)
	go func() {
		slog.Info("listening", "addr", cfg.Addr, "workers", cfg.Workers, "queue", cfg.QueueSize, "store", storeKind, "history", cfg.PostgresDSN != "")
		serverErr <- httpServer.ListenAndServe()
	}()

	select {
	case err := <-serverErr:
		return fmt.Errorf("http server: %w", err)
	case <-stopCtx.Done():
	}
	stop() // restore default signal behaviour: a second Ctrl+C kills the process

	slog.Info("shutting down")

	// 1. Stop taking requests, and wait for the ones in flight (they may still Submit).
	shutdownCtx, cancel := context.WithTimeout(context.Background(), cfg.ShutdownTimeout)
	defer cancel()
	if err := httpServer.Shutdown(shutdownCtx); err != nil {
		slog.Error("http shutdown", "err", err)
	}

	// 2. Nobody can Submit any more: close the pool and let workers drain the queue.
	p.Shutdown()

	// 3. Wait until every result has been saved, but not forever.
	select {
	case <-consumerDone:
		slog.Info("all jobs finished")
	case <-time.After(cfg.DrainTimeout):
		slog.Warn("drain timed out; unfinished jobs are lost")
	}

	return nil
}
