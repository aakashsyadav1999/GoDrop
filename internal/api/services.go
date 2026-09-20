package api

import (
	"context"
	"log/slog"
	"net/http"
	"net/url"
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

func (s *Server) ConsumeResults() {
	for r := range s.pool.Results() {
		rec := store.Record{
			ID:         r.Job.ID,
			URL:        r.Job.URL,
			CreatedAt:  r.Job.CreatedAt,
			DurationMS: r.Duration.Milliseconds(),
		}

		// Every line about this job carries its ID, so one job can be followed
		// through the logs. Only the host is logged, since a URL may hold secrets.
		log := slog.With("job_id", rec.ID, "host", hostOf(rec.URL))

		if r.Err != nil {
			rec.Status = job.StatusFailed
			rec.Error = r.Err.Error()
			log.Warn("job failed", "err", r.Err, "duration_ms", rec.DurationMS)
		} else {
			rec.Status = job.StatusDone
			rec.StatusCode = r.Value.StatusCode
			log.Info("job done", "status_code", rec.StatusCode, "duration_ms", rec.DurationMS)
		}

		if err := s.store.Save(context.Background(), rec); err != nil {
			log.Error("save job result", "err", err)
		}

		// History is best effort: the job's state is already saved above, so a
		// failure here is logged and does not affect polling.
		ctx, cancel := context.WithTimeout(context.Background(), historyTimeout)
		if err := s.history.Append(ctx, rec); err != nil {
			log.Error("record job history", "err", err)
		}
		cancel()
	}
}

// hostOf returns just the host of a URL, for logging.
func hostOf(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	return u.Host
}
