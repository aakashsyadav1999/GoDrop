package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/aakash/godrop/internal/api"
	"github.com/aakash/godrop/internal/job"
	"github.com/aakash/godrop/internal/pool"
	"github.com/aakash/godrop/internal/processor"
	"github.com/aakash/godrop/internal/store"
	"github.com/redis/go-redis/v9"
)

const (
	shutdownTimeout = 10 * time.Second // wait for in-flight HTTP requests
	drainTimeout    = 30 * time.Second // wait for queued jobs to finish
	jobTTL          = 24 * time.Hour   // how long a job record is kept in Redis

)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func run() error {
	addr := flag.String("addr", ":8080", "address to listen on")
	workers := flag.Int("workers", 5, "number of concurrent workers")
	queue := flag.Int("queue", 100, "how many jobs may wait for a free worker")
	timeout := flag.Duration("timeout", 10*time.Second, "per-job timeout")
	redisAddr := flag.String("redis", "", "Redis address, e.g. 127.0.0.1:6380; empty keeps job state in memory")

	flag.Parse()

	if *workers < 1 || *queue < 1 || *timeout <= 0 {
		return fmt.Errorf("workers and queue must be at least 1, and timeout must be positive")
	}

	var jobStore store.JobStore = store.NewMemoryStore()
	if *redisAddr != "" {
		rdb := redis.NewClient(&redis.Options{Addr: *redisAddr})
		defer rdb.Close()

		pingCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		err := rdb.Ping(pingCtx).Err()
		cancel()
		if err != nil {
			return fmt.Errorf("connect to redis at %s: %w", *redisAddr, err)
		}
		jobStore = store.NewRedisStore(rdb, jobTTL)
	}

	// The Fetcher takes a string; the pool carries URLJobs. ProcessorFunc adapts one to the other.
	fetcher := processor.NewFetcher()
	fetchURL := pool.ProcessorFunc[job.URLJob, processor.Page](
		func(ctx context.Context, j job.URLJob) (processor.Page, error) {
			return fetcher.Process(ctx, j.URL)
		},
	)

	p := pool.New(fetchURL, *workers, *queue, *timeout)
	p.Start(context.Background())

	srv := api.NewServer(jobStore, p)

	consumerDone := make(chan struct{})
	go func() {
		defer close(consumerDone)
		srv.ConsumeResults()
	}()

	httpServer := &http.Server{
		Addr:              *addr,
		Handler:           srv.Routes(),
		ReadHeaderTimeout: 5 * time.Second,
	}

	stopCtx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	serverErr := make(chan error, 1)
	go func() {
		slog.Info("listening", "addr", *addr, "workers", *workers, "queue", *queue)
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
	shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
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
	case <-time.After(drainTimeout):
		slog.Warn("drain timed out; unfinished jobs are lost")
	}

	return nil
}
