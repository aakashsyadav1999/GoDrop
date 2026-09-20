package tests

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"sync"
	"testing"
	"time"

	"github.com/aakash/godrop/internal/job"
	"github.com/aakash/godrop/internal/pool"
	"github.com/aakash/godrop/internal/processor"
	"github.com/aakash/godrop/internal/store"
)

// fakeHistory is an in-memory store.HistoryStore for testing the API without Postgres.
type fakeHistory struct {
	mu   sync.Mutex
	recs []store.Record
	err  error
}

func (f *fakeHistory) Append(_ context.Context, rec store.Record) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.err != nil {
		return f.err
	}
	f.recs = append([]store.Record{rec}, f.recs...) // newest first
	return nil
}

func (f *fakeHistory) List(_ context.Context, limit int) ([]store.Record, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.recs) > limit {
		return f.recs[:limit], nil
	}
	return f.recs, nil
}

func okProcessor() pool.Processor[job.URLJob, processor.Page] {
	return pool.ProcessorFunc[job.URLJob, processor.Page](
		func(ctx context.Context, j job.URLJob) (processor.Page, error) {
			return processor.Page{StatusCode: 200}, nil
		},
	)
}

func TestAPIRecordsHistoryAndServesIt(t *testing.T) {
	h := &fakeHistory{}
	ts := newAPIWithHistory(t, okProcessor(), 2, 10, h)

	id := submitOK(t, ts.URL, "http://example.com")
	waitForStatus(t, ts.URL, id, job.StatusDone)

	// the consumer saves the job state first and history right after, so poll
	deadline := time.Now().Add(2 * time.Second)
	var got []store.Record
	for time.Now().Before(deadline) {
		resp, err := http.Get(ts.URL + "/history?limit=5")
		if err != nil {
			t.Fatal(err)
		}
		got = nil
		_ = json.NewDecoder(resp.Body).Decode(&got)
		resp.Body.Close()
		if len(got) == 1 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	if len(got) != 1 || got[0].ID != id || got[0].Status != job.StatusDone {
		t.Fatalf("history = %+v, want the finished job %s", got, id)
	}
}

func TestAPIHistoryFailureDoesNotAffectJobs(t *testing.T) {
	h := &fakeHistory{err: errors.New("database is down")}
	ts := newAPIWithHistory(t, okProcessor(), 1, 10, h)

	id := submitOK(t, ts.URL, "http://example.com")
	waitForStatus(t, ts.URL, id, job.StatusDone) // still completes and is pollable
}

func TestAPIHistoryValidatesLimitAndReturnsEmptyList(t *testing.T) {
	ts := newAPI(t, okProcessor(), 1, 10)

	for _, q := range []string{"?limit=0", "?limit=abc", "?limit=101"} {
		resp, err := http.Get(ts.URL + "/history" + q)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusBadRequest {
			t.Errorf("/history%s: got %d, want 400", q, resp.StatusCode)
		}
	}

	resp, err := http.Get(ts.URL + "/history")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var raw json.RawMessage
	_ = json.NewDecoder(resp.Body).Decode(&raw)
	if string(raw) != "[]" {
		t.Fatalf("empty history should encode as [], got %s", raw)
	}
}
