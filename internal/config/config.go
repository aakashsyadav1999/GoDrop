// Package config gathers the server's settings from three places. In order of
// increasing priority: built-in defaults, GODROP_* environment variables, and
// command-line flags.
package config

import (
	"errors"
	"flag"
	"fmt"
	"strconv"
	"time"
)

type Config struct {
	Addr            string        // address the HTTP server listens on
	Workers         int           // concurrent workers in the pool
	QueueSize       int           // jobs that may wait for a free worker
	JobTimeout      time.Duration // per-job timeout
	RedisAddr       string        // empty means keep job state in memory
	JobTTL          time.Duration // how long a finished job record is kept
	ShutdownTimeout time.Duration // wait for in-flight HTTP requests on shutdown
	DrainTimeout    time.Duration // wait for queued jobs to finish on shutdown
	PostgresDSN     string        // empty means do not record job history

}

func defaults() Config {
	return Config{
		Addr:            ":8080",
		Workers:         5,
		QueueSize:       100,
		JobTimeout:      10 * time.Second,
		JobTTL:          24 * time.Hour,
		ShutdownTimeout: 10 * time.Second,
		DrainTimeout:    30 * time.Second,
	}
}

// Load builds a Config from args (usually os.Args[1:]) and getenv (usually
// os.Getenv). Taking both as parameters keeps it testable: tests pass their
// own instead of touching the real process environment.
func Load(args []string, getenv func(string) string) (Config, error) {
	cfg := defaults()

	if err := cfg.applyEnv(getenv); err != nil {
		return Config{}, err
	}

	// The flags' defaults are the values so far, so a flag overrides the
	// environment, and -h shows what would actually be used.
	fs := flag.NewFlagSet("godrop-server", flag.ContinueOnError)
	fs.StringVar(&cfg.Addr, "addr", cfg.Addr, "address to listen on (env GODROP_ADDR)")
	fs.IntVar(&cfg.Workers, "workers", cfg.Workers, "number of concurrent workers (env GODROP_WORKERS)")
	fs.IntVar(&cfg.QueueSize, "queue", cfg.QueueSize, "jobs that may wait for a free worker (env GODROP_QUEUE)")
	fs.DurationVar(&cfg.JobTimeout, "timeout", cfg.JobTimeout, "per-job timeout (env GODROP_JOB_TIMEOUT)")
	fs.StringVar(&cfg.RedisAddr, "redis", cfg.RedisAddr, "Redis address; empty keeps job state in memory (env GODROP_REDIS_ADDR)")
	fs.DurationVar(&cfg.JobTTL, "job-ttl", cfg.JobTTL, "how long a job record is kept (env GODROP_JOB_TTL)")
	fs.StringVar(&cfg.PostgresDSN, "postgres", cfg.PostgresDSN, "Postgres DSN for job history; empty disables history (env GODROP_POSTGRES_DSN)")

	if err := fs.Parse(args); err != nil {
		return Config{}, err
	}
	if fs.NArg() > 0 {
		return Config{}, fmt.Errorf("unexpected argument %q", fs.Arg(0))
	}

	if err := cfg.validate(); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

func (c *Config) applyEnv(getenv func(string) string) error {
	var errs []error

	str := func(dst *string, key string) {
		if v := getenv(key); v != "" {
			*dst = v
		}
	}
	num := func(dst *int, key string) {
		if v := getenv(key); v != "" {
			n, err := strconv.Atoi(v)
			if err != nil {
				errs = append(errs, fmt.Errorf("%s: %q is not a number", key, v))
				return
			}
			*dst = n
		}
	}
	dur := func(dst *time.Duration, key string) {
		if v := getenv(key); v != "" {
			d, err := time.ParseDuration(v)
			if err != nil {
				errs = append(errs, fmt.Errorf("%s: %q is not a duration like 10s or 2m", key, v))
				return
			}
			*dst = d
		}
	}

	str(&c.Addr, "GODROP_ADDR")
	num(&c.Workers, "GODROP_WORKERS")
	num(&c.QueueSize, "GODROP_QUEUE")
	dur(&c.JobTimeout, "GODROP_JOB_TIMEOUT")
	str(&c.RedisAddr, "GODROP_REDIS_ADDR")
	str(&c.PostgresDSN, "GODROP_POSTGRES_DSN")
	dur(&c.JobTTL, "GODROP_JOB_TTL")
	dur(&c.ShutdownTimeout, "GODROP_SHUTDOWN_TIMEOUT")
	dur(&c.DrainTimeout, "GODROP_DRAIN_TIMEOUT")

	return errors.Join(errs...)
}

func (c Config) validate() error {
	var errs []error

	if c.Workers < 1 {
		errs = append(errs, errors.New("workers must be at least 1"))
	}
	if c.QueueSize < 1 {
		errs = append(errs, errors.New("queue must be at least 1"))
	}
	for _, d := range []struct {
		name  string
		value time.Duration
	}{
		{"job timeout", c.JobTimeout},
		{"job ttl", c.JobTTL},
		{"shutdown timeout", c.ShutdownTimeout},
		{"drain timeout", c.DrainTimeout},
	} {
		if d.value <= 0 {
			errs = append(errs, fmt.Errorf("%s must be positive", d.name))
		}
	}

	return errors.Join(errs...)
}
