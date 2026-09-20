package pool

import (
	"context"
	"sync"
	"time"
)

// RunWorker pulls jobs until the jobs channel is closed, processes each one,
// and sends the outcome to results. It never closes either channel.
func RunWorker[T, R any](
	ctx context.Context,
	p Processor[T, R],
	timeout time.Duration,
	jobs <-chan T,
	results chan<- JobResult[T, R],
	wg *sync.WaitGroup,
) {
	defer wg.Done()

	for job := range jobs {
		start := time.Now()

		jobCtx, cancel := context.WithTimeout(ctx, timeout)
		value, err := p.Process(jobCtx, job)
		cancel()

		results <- JobResult[T, R]{
			Job:      job,
			Value:    value,
			Err:      err,
			Duration: time.Since(start),
		}
	}
}
