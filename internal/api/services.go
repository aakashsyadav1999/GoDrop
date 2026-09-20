package api

import (
	"context"
	"log/slog"
	"net/http"
	"time"

	"github.com/aakash/godrop/internal/job"
	"github.com/aakash/godrop/internal/pool"
	"github.com/aakash/godrop/internal/processor"
	"github.com/aakash/godrop/internal/store"
)

type URLPool = pool.Pool[job.URLJob, processor.Page]

// historyTimeout bounds one history write, so a slow database cannot stall the workers for long.
const historyTimeout = 5 * time.Second

type Server struct {
	store   store.JobStore
	history store.HistoryStore
	pool    *URLPool
}

func NewServer(s store.JobStore, h store.HistoryStore, p *URLPool) *Server {
	return &Server{store: s, history: h, pool: p}
}

func (s *Server) Routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /jobs", s.handleSubmit)
	mux.HandleFunc("GET /jobs/{id}", s.handleGet)
	mux.HandleFunc("GET /history", s.handleHistory)
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

		// History is best effort: the job's state is already saved above, so a
		// failure here is logged and does not affect polling.
		ctx, cancel := context.WithTimeout(context.Background(), historyTimeout)
		if err := s.history.Append(ctx, rec); err != nil {
			slog.Error("record job history", "id", rec.ID, "err", err)
		}
		cancel()
	}
}
