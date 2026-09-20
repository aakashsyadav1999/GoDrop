package pool

import (
	"context"
	"errors"
	"sync"
	"time"
)

var (
	ErrQueueFull = errors.New("pool: queue is full")
	ErrClosed    = errors.New("pool: closed")
)

// Pool is a long-lived worker pool. Jobs go in through Submit, outcomes come
// out of Results. Start launches the workers, Shutdown drains and stops them.
type Pool[T, R any] struct {
	proc    Processor[T, R]
	workers int
	timeout time.Duration

	jobs    chan T
	results chan JobResult[T, R]
	wg      sync.WaitGroup

	mu     sync.RWMutex // guards closed, and makes Submit vs Shutdown safe
	closed bool
}

func New[T, R any](proc Processor[T, R], workers, queueSize int, timeout time.Duration) *Pool[T, R] {
	return &Pool[T, R]{
		proc:    proc,
		workers: workers,
		timeout: timeout,
		jobs:    make(chan T, queueSize),
		results: make(chan JobResult[T, R]),
	}
}

// Start launches the workers. ctx is the parent of every per-job context.
func (p *Pool[T, R]) Start(ctx context.Context) {
	for range p.workers {
		p.wg.Add(1)
		go RunWorker(ctx, p.proc, p.timeout, p.jobs, p.results, &p.wg)
	}

	go func() {
		p.wg.Wait()
		close(p.results)
	}()
}

// Submit queues a job without blocking. It returns ErrQueueFull when the queue
// has no room, and ErrClosed after Shutdown.
func (p *Pool[T, R]) Submit(job T) error {
	p.mu.RLock()
	defer p.mu.RUnlock()

	if p.closed {
		return ErrClosed
	}

	select {
	case p.jobs <- job:
		return nil
	default:
		return ErrQueueFull
	}
}

// Results delivers one JobResult per finished job. Someone must keep reading it,
// or workers block. It is closed once Shutdown has been called and the queue drained.
func (p *Pool[T, R]) Results() <-chan JobResult[T, R] {
	return p.results
}

// Shutdown stops accepting jobs. Workers finish everything already queued, then exit.
func (p *Pool[T, R]) Shutdown() {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.closed {
		return
	}
	p.closed = true
	close(p.jobs)
}
