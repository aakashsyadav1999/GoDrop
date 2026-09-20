package tests

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/aakash/godrop/internal/api"
	"github.com/aakash/godrop/internal/logging"
)

// jsonLogger returns a logger that writes JSON lines into the returned buffer.
func jsonLogger(t *testing.T, level string) (*slog.Logger, *bytes.Buffer) {
	t.Helper()
	var buf bytes.Buffer
	logger, err := logging.New(&buf, level, "json")
	if err != nil {
		t.Fatal(err)
	}
	return logger, &buf
}

// lastLine decodes the last JSON log line in buf.
func lastLine(t *testing.T, buf *bytes.Buffer) map[string]any {
	t.Helper()
	lines := strings.Split(strings.TrimSpace(buf.String()), "\n")
	var m map[string]any
	if err := json.Unmarshal([]byte(lines[len(lines)-1]), &m); err != nil {
		t.Fatalf("log line is not JSON: %q: %v", lines[len(lines)-1], err)
	}
	return m
}

func TestLoggingJSONAndLevels(t *testing.T) {
	logger, buf := jsonLogger(t, "info")

	logger.Debug("too quiet to show")
	logger.Info("hello", "job_id", "abc")

	if n := strings.Count(strings.TrimSpace(buf.String()), "\n") + 1; n != 1 {
		t.Fatalf("got %d log lines, want 1 (debug should be filtered out)", n)
	}
	m := lastLine(t, buf)
	if m["msg"] != "hello" || m["level"] != "INFO" || m["job_id"] != "abc" {
		t.Fatalf("unexpected log line: %v", m)
	}
}

func TestLoggingRejectsBadValues(t *testing.T) {
	var buf bytes.Buffer
	if _, err := logging.New(&buf, "loud", "json"); err == nil {
		t.Error("an unknown level should be rejected")
	}
	if _, err := logging.New(&buf, "info", "xml"); err == nil {
		t.Error("an unknown format should be rejected")
	}
}

func TestRequestLoggerRecordsStatusRouteAndDuration(t *testing.T) {
	logger, buf := jsonLogger(t, "info")

	mux := http.NewServeMux()
	mux.HandleFunc("GET /jobs/{id}", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	})
	handler := api.RequestLogger(logger, mux)

	handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", "/jobs/abc123", nil))

	m := lastLine(t, buf)
	if m["msg"] != "request" || m["method"] != "GET" || m["path"] != "/jobs/abc123" ||
		m["route"] != "GET /jobs/{id}" || m["status"] != float64(404) || m["level"] != "INFO" {
		t.Fatalf("unexpected request log: %v", m)
	}
	if _, ok := m["duration_ms"]; !ok {
		t.Fatal("request log should include duration_ms")
	}
}

func TestRequestLoggerDefaultsTo200(t *testing.T) {
	logger, buf := jsonLogger(t, "info")

	handler := api.RequestLogger(logger, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("hi")) // never calls WriteHeader
	}))
	handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", "/x", nil))

	if m := lastLine(t, buf); m["status"] != float64(200) {
		t.Fatalf("status = %v, want 200", m["status"])
	}
}

func TestRequestLoggerLogsServerErrorsAtErrorLevel(t *testing.T) {
	logger, buf := jsonLogger(t, "info")

	handler := api.RequestLogger(logger, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", "/x", nil))

	if m := lastLine(t, buf); m["level"] != "ERROR" || m["status"] != float64(500) {
		t.Fatalf("unexpected log: %v", m)
	}
}

func TestRequestLoggerKeepsHealthChecksOutOfTheInfoLog(t *testing.T) {
	handler := func(logger *slog.Logger) http.Handler {
		return api.RequestLogger(logger, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	}

	logger, buf := jsonLogger(t, "info")
	handler(logger).ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", "/healthz", nil))
	if buf.Len() != 0 {
		t.Fatalf("a health check should not be logged at info level, got %q", buf.String())
	}

	logger, buf = jsonLogger(t, "debug")
	handler(logger).ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", "/healthz", nil))
	if m := lastLine(t, buf); m["level"] != "DEBUG" || m["path"] != "/healthz" {
		t.Fatalf("at debug level the health check should be logged: %v", m)
	}
}
