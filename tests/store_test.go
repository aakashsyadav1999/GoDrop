package tests

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"

	"github.com/aakash/godrop/internal/job"
	"github.com/aakash/godrop/internal/store"
)

func TestMemoryStoreSaveGet(t *testing.T) {
	ctx := context.Background()
	s := store.NewMemoryStore()

	want := store.Record{ID: "abc", URL: "https://example.com", Status: job.StatusQueued}
	if err := s.Save(ctx, want); err != nil {
		t.Fatal(err)
	}

	got, err := s.Get(ctx, "abc")
	if err != nil || got != want {
		t.Fatalf("got (%+v, %v), want (%+v, nil)", got, err, want)
	}

	if _, err := s.Get(ctx, "missing"); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("got %v, want ErrNotFound", err)
	}
}

func TestMemoryStoreConcurrent(t *testing.T) {
	ctx := context.Background()
	s := store.NewMemoryStore()

	var wg sync.WaitGroup
	for i := range 50 {
		id := fmt.Sprintf("job-%d", i)
		wg.Add(2)
		go func() {
			defer wg.Done()
			_ = s.Save(ctx, store.Record{ID: id, Status: job.StatusQueued})
		}()
		go func() {
			defer wg.Done()
			_, _ = s.Get(ctx, id) // may or may not find it yet; must not race
		}()
	}
	wg.Wait()

	for i := range 50 {
		if _, err := s.Get(ctx, fmt.Sprintf("job-%d", i)); err != nil {
			t.Fatalf("job-%d: %v", i, err)
		}
	}
}

func TestMemoryStoreDelete(t *testing.T) {
	ctx := context.Background()
	s := store.NewMemoryStore()

	_ = s.Save(ctx, store.Record{ID: "abc"})
	if err := s.Delete(ctx, "abc"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Get(ctx, "abc"); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("got %v, want ErrNotFound after delete", err)
	}
	if err := s.Delete(ctx, "abc"); err != nil {
		t.Fatalf("deleting a missing id should be a no-op, got %v", err)
	}
}
