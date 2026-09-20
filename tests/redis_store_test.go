package tests

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/aakash/godrop/internal/job"
	"github.com/aakash/godrop/internal/store"
)

// newRedisStore connects to the compose Redis, and skips the test if it is not running.
func newRedisStore(t *testing.T, ttl time.Duration) *store.RedisStore {
	t.Helper()

	addr := os.Getenv("REDIS_TEST_ADDR")
	if addr == "" {
		addr = "127.0.0.1:6380"
	}

	client := redis.NewClient(&redis.Options{Addr: addr})
	t.Cleanup(func() { client.Close() })

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := client.Ping(ctx).Err(); err != nil {
		t.Skipf("redis not reachable at %s: %v", addr, err)
	}

	return store.NewRedisStore(client, ttl)
}

func TestRedisStoreSaveGet(t *testing.T) {
	ctx := context.Background()
	s := newRedisStore(t, time.Minute)

	want := store.Record{
		ID:         job.NewBaseJob().ID, // random, so tests never touch other keys
		URL:        "https://example.com",
		Status:     job.StatusDone,
		StatusCode: 200,
		DurationMS: 42,
		CreatedAt:  time.Now(),
	}
	t.Cleanup(func() { _ = s.Delete(ctx, want.ID) })

	if err := s.Save(ctx, want); err != nil {
		t.Fatal(err)
	}

	got, err := s.Get(ctx, want.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.ID != want.ID || got.URL != want.URL || got.Status != want.Status ||
		got.StatusCode != want.StatusCode || got.DurationMS != want.DurationMS ||
		!got.CreatedAt.Equal(want.CreatedAt) {
		t.Fatalf("got %+v, want %+v", got, want)
	}
}

func TestRedisStoreMissingAndDelete(t *testing.T) {
	ctx := context.Background()
	s := newRedisStore(t, time.Minute)

	if _, err := s.Get(ctx, "no-such-job"); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("got %v, want ErrNotFound", err)
	}

	rec := store.Record{ID: job.NewBaseJob().ID, Status: job.StatusQueued}
	_ = s.Save(ctx, rec)
	if err := s.Delete(ctx, rec.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Get(ctx, rec.ID); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("got %v, want ErrNotFound after delete", err)
	}
	if err := s.Delete(ctx, rec.ID); err != nil {
		t.Fatalf("deleting a missing id should be a no-op, got %v", err)
	}
}

func TestRedisStoreExpires(t *testing.T) {
	ctx := context.Background()
	s := newRedisStore(t, 100*time.Millisecond)

	rec := store.Record{ID: job.NewBaseJob().ID, Status: job.StatusDone}
	if err := s.Save(ctx, rec); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Get(ctx, rec.ID); err != nil {
		t.Fatalf("record should exist right after Save: %v", err)
	}

	time.Sleep(300 * time.Millisecond)

	if _, err := s.Get(ctx, rec.ID); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("got %v, want ErrNotFound after the TTL", err)
	}
}
