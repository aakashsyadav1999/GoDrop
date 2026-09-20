package store

import (
	"context"
	"errors"
	"time"

	"github.com/aakash/godrop/internal/job"
)

var ErrNotFound = errors.New("Store: job not found")

// Record is what the API reports about a job.
type Record struct {
	ID         string     `json:"id"`
	URL        string     `json:"url"`
	Status     job.Status `json:"status"`
	StatusCode int        `json:"status_code,omitempty"`
	Error      string     `json:"error,omitempty"`
	DurationMS int64      `json:"duration_ms,omitempty"`
	CreatedAt  time.Time  `json:"created_at"`
}

// JobStore keeps job records. The methods take a context and return errors
// because a Redis or Postgres implementation will need both.
type JobStore interface {
	Save(ctx context.Context, rec Record) error
	Get(ctx context.Context, id string) (Record, error)
	Delete(ctx context.Context, id string) error
}
