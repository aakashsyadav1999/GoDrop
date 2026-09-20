package tests

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/aakash/godrop/internal/pool"
)

func TestRunWorker(t *testing.T) {
	p := pool.ProcessorFunc[string, int](
		func(ctx context.Context, s string) (int, error) {
			if s == "" {
				return 0, errors.New("empty")
			}
			return len(s), nil
		},
	)

	inputs := []string{"a", "bb", "", "dddd", "eeeee", "ffffff"}

	jobs := make(chan string)
	results := make(chan pool.JobResult[string, int])

	var wg sync.WaitGroup
	for range 3 {
		wg.Add(1)
		go pool.RunWorker(context.Background(), p, time.Second, jobs, results, &wg)
	}

	go func() {
		defer close(jobs)
		for _, in := range inputs {
			jobs <- in
		}
	}()

	go func() {
		wg.Wait()
		close(results)
	}()

	got := map[string]pool.JobResult[string, int]{}
	for r := range results {
		got[r.Job] = r
	}

	if len(got) != len(inputs) {
		t.Fatalf("got %d results, want %d", len(got), len(inputs))
	}
	if got["dddd"].Value != 4 || got["dddd"].Err != nil {
		t.Errorf("dddd: got %+v", got["dddd"])
	}
	if got[""].Err == nil {
		t.Errorf("empty input should carry an error, got %+v", got[""])
	}
}

func TestRunWorkerTimeout(t *testing.T) {
	// This processor respects its context: it gives up as soon as ctx is done.
	p := pool.ProcessorFunc[string, int](
		func(ctx context.Context, s string) (int, error) {
			select {
			case <-time.After(time.Second): // pretend work
				return 1, nil
			case <-ctx.Done():
				return 0, ctx.Err()
			}
		},
	)

	jobs := make(chan string, 1)
	results := make(chan pool.JobResult[string, int], 1)
	jobs <- "slow"
	close(jobs)

	var wg sync.WaitGroup
	wg.Add(1)
	pool.RunWorker(context.Background(), p, 50*time.Millisecond, jobs, results, &wg)

	r := <-results
	if !errors.Is(r.Err, context.DeadlineExceeded) {
		t.Fatalf("got err %v, want context.DeadlineExceeded", r.Err)
	}
	if r.Duration > 500*time.Millisecond {
		t.Fatalf("job ran %v, the timeout should have stopped it near 50ms", r.Duration)
	}
}
