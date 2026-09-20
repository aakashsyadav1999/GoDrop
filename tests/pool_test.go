package tests

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/aakash/godrop/internal/pool"
)

func TestPoolProcessesJobs(t *testing.T) {
	double := pool.ProcessorFunc[int, int](
		func(ctx context.Context, n int) (int, error) { return n * 2, nil },
	)

	p := pool.New(double, 3, 10, time.Second)
	p.Start(context.Background())

	for i := 1; i <= 6; i++ {
		if err := p.Submit(i); err != nil {
			t.Fatalf("submit %d: %v", i, err)
		}
	}
	p.Shutdown()

	got := map[int]int{}
	for r := range p.Results() { // ends when Shutdown has drained the queue
		got[r.Job] = r.Value
	}

	if len(got) != 6 {
		t.Fatalf("got %d results, want 6", len(got))
	}
	for i := 1; i <= 6; i++ {
		if got[i] != i*2 {
			t.Errorf("job %d: got %d, want %d", i, got[i], i*2)
		}
	}
}

func TestPoolQueueFull(t *testing.T) {
	started := make(chan struct{}, 1)
	release := make(chan struct{})
	blocked := pool.ProcessorFunc[int, int](
		func(ctx context.Context, n int) (int, error) {
			select {
			case started <- struct{}{}:
			default:
			}
			<-release
			return n, nil
		},
	)

	p := pool.New(blocked, 1, 1, time.Second) // 1 worker, room for 1 queued job
	p.Start(context.Background())

	if err := p.Submit(1); err != nil {
		t.Fatal(err)
	}
	<-started // the worker has taken job 1, so the queue is empty again

	if err := p.Submit(2); err != nil { // fills the queue
		t.Fatalf("submit 2: %v", err)
	}
	if err := p.Submit(3); !errors.Is(err, pool.ErrQueueFull) {
		t.Fatalf("submit 3: got %v, want ErrQueueFull", err)
	}

	close(release)
	p.Shutdown()

	n := 0
	for range p.Results() {
		n++
	}
	if n != 2 {
		t.Fatalf("got %d results, want 2", n)
	}
}

func TestPoolSubmitAfterShutdown(t *testing.T) {
	noop := pool.ProcessorFunc[int, int](
		func(ctx context.Context, n int) (int, error) { return n, nil },
	)

	p := pool.New(noop, 2, 5, time.Second)
	p.Start(context.Background())

	p.Shutdown()
	p.Shutdown() // calling it twice must be safe

	if err := p.Submit(1); !errors.Is(err, pool.ErrClosed) {
		t.Fatalf("got %v, want ErrClosed", err)
	}

	if _, ok := <-p.Results(); ok {
		t.Fatal("Results should be closed once the pool has drained")
	}
}
