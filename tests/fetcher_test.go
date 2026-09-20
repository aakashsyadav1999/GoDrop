package tests

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/aakash/godrop/internal/processor"
)

func TestFetcher(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/missing" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	f := processor.NewFetcher()

	page, err := f.Process(context.Background(), srv.URL+"/ok")
	if err != nil || page.StatusCode != 200 {
		t.Fatalf("/ok: got (%+v, %v), want (200, nil)", page, err)
	}

	page, err = f.Process(context.Background(), srv.URL+"/missing")
	if err != nil || page.StatusCode != 404 {
		t.Fatalf("/missing: got (%+v, %v), want (404, nil)", page, err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := f.Process(ctx, srv.URL+"/ok"); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled ctx: got %v, want context.Canceled", err)
	}
}
