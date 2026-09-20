package api

import (
	"context"
	"log/slog"
	"net/http"

	"github.com/aakash/godrop/internal/job"
	"github.com/aakash/godrop/internal/pool"
	"github.com/aakash/godrop/internal/processor"
	"github.com/aakash/godrop/internal/store"
)

type URLPool = pool.Pool[job.URLJob, processor.Page]

type Server struct {
	store store.JobStore
	pool  *URLPool
}

func NewServer(s store.JobStore, p *URLPool) *Server {
	return &Server{store: s, pool: p}
}

func (s *Server) Routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /jobs", s.handleSubmit)
	mux.HandleFunc("GET /jobs/{id}", s.handleGet)
	return mux
}

// ConsumeResults turns finished jobs into store records. Run it in its own
// goroutine: it returns once the pool has shut down and drained.
func (s *Server) ConsumeResults() {
	for r := range s.pool.Results() {
		rec := store.Record{
			ID:         r.Job.ID,
			URL:        r.Job.URL,
			CreatedAt:  r.Job.CreatedAt,
			DurationMS: r.Duration.Milliseconds(),
		}

		if r.Err != nil {
			rec.Status = job.StatusFailed
			rec.Error = r.Err.Error()
		} else {
			rec.Status = job.StatusDone
			rec.StatusCode = r.Value.StatusCode
		}

		if err := s.store.Save(context.Background(), rec); err != nil {
			slog.Error("save job result", "id", rec.ID, "err", err)
		}
	}
}
