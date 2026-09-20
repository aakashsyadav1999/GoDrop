package job

import (
	"crypto/rand"
	"time"
)

type Status string

const (
	StatusQueued Status = "queued"
	StatusDone   Status = "done"
	StatusFailed Status = "failed"
)

// BaseJob holds the fields and methods every kind of job shares.
type BaseJob struct {
	ID        string
	CreatedAt time.Time
}

func NewBaseJob() BaseJob {
	return BaseJob{ID: rand.Text(), CreatedAt: time.Now()}
}

// Age is promoted to every type that embeds BaseJob.
func (b BaseJob) Age() time.Duration {
	return time.Since(b.CreatedAt)
}

type URLJob struct {
	BaseJob
	URL string
}

func NewURLJob(url string) URLJob {
	return URLJob{BaseJob: NewBaseJob(), URL: url}
}
