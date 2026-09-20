package pool

import (
	"context"
	"time"
)

// Processor turns one job of type T into a result of type R.
type Processor[T, R any] interface {
	Process(ctx context.Context, job T) (R, error)
}

// ProcessorFunc lets a plain function act as a Processor.
type ProcessorFunc[T, R any] func(ctx context.Context, job T) (R, error)

// Process calls f itself, which is what makes ProcessorFunc satisfy Processor.
func (f ProcessorFunc[T, R]) Process(ctx context.Context, job T) (R, error) {
	return f(ctx, job)
}

// Compile-time check: fails to build if ProcessorFunc stops satisfying Processor.
var _ Processor[string, int] = ProcessorFunc[string, int](nil)

// JobResult ties an outcome to the job that produced it.
type JobResult[T, R any] struct {
	Job      T
	Value    R
	Err      error
	Duration time.Duration
}
