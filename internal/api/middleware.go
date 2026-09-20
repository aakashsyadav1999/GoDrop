package api

import (
	"log/slog"
	"net/http"
	"time"
)

// statusRecorder remembers the status code a handler wrote. Embedding
// http.ResponseWriter gives it every other method for free; only WriteHeader
// is overridden.
type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (r *statusRecorder) WriteHeader(code int) {
	r.status = code
	r.ResponseWriter.WriteHeader(code)
}

// Unwrap lets http.ResponseController reach the real ResponseWriter.
func (r *statusRecorder) Unwrap() http.ResponseWriter {
	return r.ResponseWriter
}

// RequestLogger logs one line per request, after it has been handled.
func RequestLogger(logger *slog.Logger, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK} // a handler that never calls WriteHeader sends 200

		next.ServeHTTP(rec, r)

		level := slog.LevelInfo
		switch {
		case rec.status >= 500:
			level = slog.LevelError
		case r.URL.Path == "/healthz":
			level = slog.LevelDebug // health checks run every few seconds: keep them out of the normal log
		}
		logger.Log(r.Context(), level, "request",
			"method", r.Method,
			"path", r.URL.Path,
			"route", r.Pattern, // e.g. "GET /jobs/{id}", set by the mux; empty if nothing matched
			"status", rec.status,
			"duration_ms", time.Since(start).Milliseconds(),
		)
	})
}
