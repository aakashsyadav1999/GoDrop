package pool

import "context"

// Feed sends every item to job, then close jobs. so workers finish the feed is the producer
func Feed[T any](ctx context.Context, items []T, jobs chan<- T) {
	defer close(jobs)

	for _, item := range items {
		select {
		case jobs <- item:
		case <-ctx.Done():
			return
		}
	}
}
