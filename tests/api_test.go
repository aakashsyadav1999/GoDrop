package tests

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/aakash/godrop/internal/api"
	"github.com/aakash/godrop/internal/job"
	"github.com/aakash/godrop/internal/pool"
	"github.com/aakash/godrop/internal/processor"
	"github.com/aakash/godrop/internal/store"
)

// newAPI starts a real pool and store behind an httptest server.
func newAPI(t *testing.T, proc pool.Processor[job.URLJob, processor.Page], workers, queue int) *httptest.Server {
	t.Helper()
	return newAPIWithHistory(t, proc, workers, queue, store.NopHistory{})
}

func postJob(t *testing.T, base, body string) *http.Response {
	t.Helper()
	resp, err := http.Post(base+"/jobs", "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { resp.Body.Close() })
	return resp
}

func getJob(t *testing.T, base, id string) (*http.Response, store.Record) {
	t.Helper()
	resp, err := http.Get(base + "/jobs/" + id)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	var rec store.Record
	_ = json.NewDecoder(resp.Body).Decode(&rec)
	return resp, rec
}

func submitOK(t *testing.T, base, url string) string {
	t.Helper()
	resp := postJob(t, base, `{"url": "`+url+`"}`)
	if resp.StatusCode != http.StatusAccepted {
		t.Fatalf("POST /jobs: got %d, want 202", resp.StatusCode)
	}
	var out struct{ ID string }
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil || out.ID == "" {
		t.Fatalf("no id in response: %v", err)
	}
	if loc := resp.Header.Get("Location"); loc != "/jobs/"+out.ID {
		t.Fatalf("Location = %q", loc)
	}
	return out.ID
}

func waitForStatus(t *testing.T, base, id string, want job.Status) store.Record {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		_, rec := getJob(t, base, id)
		if rec.Status == want {
			return rec
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("job %s never reached status %q", id, want)
	return store.Record{}
}

func TestAPISubmitThenPoll(t *testing.T) {
	ok := pool.ProcessorFunc[job.URLJob, processor.Page](
		func(ctx context.Context, j job.URLJob) (processor.Page, error) {
			return processor.Page{StatusCode: 200}, nil
		},
	)
	ts := newAPI(t, ok, 2, 10)

	id := submitOK(t, ts.URL, "http://example.com")
	rec := waitForStatus(t, ts.URL, id, job.StatusDone)

	if rec.StatusCode != 200 || rec.URL != "http://example.com" || rec.ID != id {
		t.Fatalf("unexpected record: %+v", rec)
	}
}

func TestAPIFailedJob(t *testing.T) {
	bad := pool.ProcessorFunc[job.URLJob, processor.Page](
		func(ctx context.Context, j job.URLJob) (processor.Page, error) {
			return processor.Page{}, errors.New("boom")
		},
	)
	ts := newAPI(t, bad, 1, 10)

	id := submitOK(t, ts.URL, "http://example.com")
	rec := waitForStatus(t, ts.URL, id, job.StatusFailed)

	if rec.Error != "boom" {
		t.Fatalf("Error = %q, want boom", rec.Error)
	}
}

func TestAPIRejectsBadRequests(t *testing.T) {
	noop := pool.ProcessorFunc[job.URLJob, processor.Page](
		func(ctx context.Context, j job.URLJob) (processor.Page, error) { return processor.Page{}, nil },
	)
	ts := newAPI(t, noop, 1, 10)

	for _, body := range []string{
		`not json`,
		`{}`,
		`{"url": ""}`,
		`{"url": "ftp://example.com"}`,
		`{"url": "http://"}`,
		`{"url": "example.com"}`,
	} {
		if resp := postJob(t, ts.URL, body); resp.StatusCode != http.StatusBadRequest {
			t.Errorf("body %q: got %d, want 400", body, resp.StatusCode)
		}
	}
}

func TestAPIUnknownJobIs404(t *testing.T) {
	noop := pool.ProcessorFunc[job.URLJob, processor.Page](
		func(ctx context.Context, j job.URLJob) (processor.Page, error) { return processor.Page{}, nil },
	)
	ts := newAPI(t, noop, 1, 10)

	if resp, _ := getJob(t, ts.URL, "nope"); resp.StatusCode != http.StatusNotFound {
		t.Fatalf("got %d, want 404", resp.StatusCode)
	}
}

func TestAPIQueueFullIs503(t *testing.T) {
	started := make(chan struct{}, 1)
	release := make(chan struct{})
	blocked := pool.ProcessorFunc[job.URLJob, processor.Page](
		func(ctx context.Context, j job.URLJob) (processor.Page, error) {
			select {
			case started <- struct{}{}:
			default:
			}
			<-release
			return processor.Page{StatusCode: 200}, nil
		},
	)
	ts := newAPI(t, blocked, 1, 1) // 1 worker, room for 1 queued job
	defer close(release)

	submitOK(t, ts.URL, "http://example.com/1")
	<-started // the worker holds job 1; the queue is empty
	submitOK(t, ts.URL, "http://example.com/2")

	resp := postJob(t, ts.URL, `{"url": "http://example.com/3"}`)
	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("got %d, want 503", resp.StatusCode)
	}
	if resp.Header.Get("Retry-After") == "" {
		t.Error("503 should carry a Retry-After header")
	}
}

func newAPIWithHistory(t *testing.T, proc pool.Processor[job.URLJob, processor.Page], workers, queue int, h store.HistoryStore) *httptest.Server {
	t.Helper()

	p := pool.New(proc, workers, queue, time.Second)
	p.Start(context.Background())

	srv := api.NewServer(store.NewMemoryStore(), h, p)
	go srv.ConsumeResults()

	ts := httptest.NewServer(srv.Routes())
	t.Cleanup(func() {
		ts.Close()
		p.Shutdown()
	})
	return ts
}

func TestAPIHealthz(t *testing.T) {
	ts := newAPI(t, okProcessor(), 1, 1)

	resp, err := http.Get(ts.URL + "/healthz")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	var body map[string]string
	_ = json.NewDecoder(resp.Body).Decode(&body)
	if resp.StatusCode != http.StatusOK || body["status"] != "ok" {
		t.Fatalf("got %d %v, want 200 {status: ok}", resp.StatusCode, body)
	}
}
