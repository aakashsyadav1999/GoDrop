package tests

import (
	"errors"
	"flag"
	"strings"
	"testing"
	"time"

	"github.com/aakash/godrop/internal/config"
)

// fakeEnv returns a getenv function backed by a map, so tests never touch the real environment.
func fakeEnv(m map[string]string) func(string) string {
	return func(key string) string { return m[key] }
}

func TestConfigDefaults(t *testing.T) {
	cfg, err := config.Load(nil, fakeEnv(nil))
	if err != nil {
		t.Fatal(err)
	}

	if cfg.Addr != ":8080" || cfg.Workers != 5 || cfg.QueueSize != 100 ||
		cfg.JobTimeout != 10*time.Second || cfg.RedisAddr != "" || cfg.JobTTL != 24*time.Hour {
		t.Fatalf("unexpected defaults: %+v", cfg)
	}
}

func TestConfigEnvOverridesDefaults(t *testing.T) {
	cfg, err := config.Load(nil, fakeEnv(map[string]string{
		"GODROP_ADDR":         "127.0.0.1:9000",
		"GODROP_WORKERS":      "3",
		"GODROP_JOB_TIMEOUT":  "2s",
		"GODROP_REDIS_ADDR":   "redis:6379",
		"GODROP_POSTGRES_DSN": "postgres://u:p@db:5432/x",
	}))
	if err != nil {
		t.Fatal(err)
	}

	if cfg.Addr != "127.0.0.1:9000" || cfg.Workers != 3 ||
		cfg.JobTimeout != 2*time.Second || cfg.RedisAddr != "redis:6379" ||
		cfg.PostgresDSN != "postgres://u:p@db:5432/x" {
		t.Fatalf("env not applied: %+v", cfg)
	}

	if cfg.QueueSize != 100 {
		t.Fatalf("unset env should keep the default, got queue %d", cfg.QueueSize)
	}

}

func TestConfigFlagsOverrideEnv(t *testing.T) {
	cfg, err := config.Load(
		[]string{"-workers", "7", "-timeout", "500ms"},
		fakeEnv(map[string]string{
			"GODROP_WORKERS": "3",
			"GODROP_QUEUE":   "50",
		}),
	)
	if err != nil {
		t.Fatal(err)
	}

	if cfg.Workers != 7 || cfg.JobTimeout != 500*time.Millisecond {
		t.Fatalf("flags should win over env: %+v", cfg)
	}
	if cfg.QueueSize != 50 {
		t.Fatalf("env should still apply where no flag is given, got queue %d", cfg.QueueSize)
	}
}

func TestConfigRejectsBadValues(t *testing.T) {
	tests := []struct {
		name    string
		args    []string
		env     map[string]string
		wantErr string
	}{
		{"non-numeric env", nil, map[string]string{"GODROP_WORKERS": "abc"}, "GODROP_WORKERS"},
		{"bad duration env", nil, map[string]string{"GODROP_JOB_TIMEOUT": "soon"}, "GODROP_JOB_TIMEOUT"},
		{"zero workers", []string{"-workers", "0"}, nil, "workers must be at least 1"},
		{"zero queue via env", nil, map[string]string{"GODROP_QUEUE": "0"}, "queue must be at least 1"},
		{"negative timeout", []string{"-timeout=-1s"}, nil, "job timeout must be positive"},
		{"unknown flag", []string{"-nope"}, nil, "nope"},
		{"stray argument", []string{"extra"}, nil, "unexpected argument"},
		{"bad log level", nil, map[string]string{"GODROP_LOG_LEVEL": "loud"}, "log level"},
		{"bad log format", []string{"-log-format", "xml"}, nil, "log format"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := config.Load(tc.args, fakeEnv(tc.env))
			if err == nil {
				t.Fatal("expected an error")
			}
			if !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("error %q should mention %q", err, tc.wantErr)
			}
		})
	}
}

func TestConfigHelp(t *testing.T) {
	_, err := config.Load([]string{"-h"}, fakeEnv(nil))
	if !errors.Is(err, flag.ErrHelp) {
		t.Fatalf("got %v, want flag.ErrHelp", err)
	}
}
