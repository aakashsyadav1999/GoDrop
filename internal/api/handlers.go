package api

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/url"
	"strconv"

	"github.com/aakash/godrop/internal/job"
	"github.com/aakash/godrop/internal/pool"
	"github.com/aakash/godrop/internal/store"
)

type submitRequest struct {
	URL string `json:"url"`
}

func (s *Server) handleSubmit(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)

	var req submitRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, `body must be JSON like {"url": "https://example.com"}`)
		return
	}
	if err := validateURL(req.URL); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	j := job.NewURLJob(req.URL)

	// Save before Submit: once the job is queued a worker can finish it at any
	// moment, and a late "queued" write would overwrite its "done".
	rec := store.Record{ID: j.ID, URL: j.URL, Status: job.StatusQueued, CreatedAt: j.CreatedAt}
	if err := s.store.Save(r.Context(), rec); err != nil {
		slog.Error("save queued job", "job_id", j.ID, "err", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	if err := s.pool.Submit(j); err != nil {
		_ = s.store.Delete(r.Context(), j.ID) // nobody will ever process it

		if errors.Is(err, pool.ErrQueueFull) || errors.Is(err, pool.ErrClosed) {
			w.Header().Set("Retry-After", "1")
			writeError(w, http.StatusServiceUnavailable, "server is busy, try again shortly")
			return
		}
		slog.Error("submit job", "job_id", j.ID, "err", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	slog.Info("job queued", "job_id", j.ID, "host", hostOf(j.URL))
	w.Header().Set("Location", "/jobs/"+j.ID)
	writeJSON(w, http.StatusAccepted, map[string]string{"id": j.ID})
}

func (s *Server) handleGet(w http.ResponseWriter, r *http.Request) {
	rec, err := s.store.Get(r.Context(), r.PathValue("id"))
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "job not found")
		return
	}
	if err != nil {
		slog.Error("get job", "err", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	writeJSON(w, http.StatusOK, rec)
}

func (s *Server) handleHistory(w http.ResponseWriter, r *http.Request) {
	limit := 20
	if v := r.URL.Query().Get("limit"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 || n > 100 {
			writeError(w, http.StatusBadRequest, "limit must be a number from 1 to 100")
			return
		}
		limit = n
	}

	recs, err := s.history.List(r.Context(), limit)
	if err != nil {
		slog.Error("list history", "err", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	if recs == nil {
		recs = []store.Record{} // encode as [] and not null
	}

	writeJSON(w, http.StatusOK, recs)
}

// handleHealth reports that the process is up and serving. Docker and Caddy call it.
func handleHealth(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func validateURL(raw string) error {
	u, err := url.Parse(raw)
	if err != nil {
		return errors.New("url is not valid")
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return errors.New("url must start with http:// or https://")
	}
	if u.Host == "" {
		return errors.New("url has no host")
	}
	return nil
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		slog.Error("write response", "err", err)
	}
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}
