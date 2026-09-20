package tests

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"sync"
	"testing"

	"github.com/aakash/godrop/internal/api"
)

// limited wraps a trivial handler in the rate limiter.
func limited(t *testing.T, cfg api.RateLimitConfig) http.Handler {
	t.Helper()

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	ok := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) })
	return api.RateLimit(ctx, cfg, ok)
}

// call sends one request from remoteAddr, with an optional X-Forwarded-For, and returns the response.
func call(h http.Handler, path, remoteAddr, xff string) *httptest.ResponseRecorder {
	req := httptest.NewRequest("GET", path, nil)
	req.RemoteAddr = remoteAddr
	if xff != "" {
		req.Header.Set("X-Forwarded-For", xff)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func TestRateLimitBlocksAfterTheBurst(t *testing.T) {
	// 0.5 requests/second refills so slowly that no token comes back during the test.
	h := limited(t, api.RateLimitConfig{RPS: 0.5, Burst: 3})

	for i := 1; i <= 3; i++ {
		if rec := call(h, "/x", "10.0.0.1:5000", ""); rec.Code != http.StatusOK {
			t.Fatalf("request %d within the burst: got %d, want 200", i, rec.Code)
		}
	}

	rec := call(h, "/x", "10.0.0.1:5000", "")
	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("request past the burst: got %d, want 429", rec.Code)
	}
	if secs, err := strconv.Atoi(rec.Header().Get("Retry-After")); err != nil || secs < 1 {
		t.Fatalf("Retry-After = %q, want a whole number of seconds >= 1", rec.Header().Get("Retry-After"))
	}
}

func TestRateLimitIsPerClient(t *testing.T) {
	h := limited(t, api.RateLimitConfig{RPS: 0.5, Burst: 1})

	if rec := call(h, "/x", "10.0.0.1:5000", ""); rec.Code != http.StatusOK {
		t.Fatalf("first client: got %d", rec.Code)
	}
	if rec := call(h, "/x", "10.0.0.1:5001", ""); rec.Code != http.StatusTooManyRequests {
		t.Fatalf("same client, another port: got %d, want 429 (the port is not part of the identity)", rec.Code)
	}
	if rec := call(h, "/x", "10.0.0.2:5000", ""); rec.Code != http.StatusOK {
		t.Fatalf("a different client should have its own bucket, got %d", rec.Code)
	}
}

func TestRateLimitIgnoresForwardedForUnlessProxyIsTrusted(t *testing.T) {
	h := limited(t, api.RateLimitConfig{RPS: 0.5, Burst: 1, TrustProxy: false})

	call(h, "/x", "10.0.0.1:5000", "1.1.1.1")
	// same real client, but claiming to be someone else: must not get a fresh bucket
	if rec := call(h, "/x", "10.0.0.1:5000", "2.2.2.2"); rec.Code != http.StatusTooManyRequests {
		t.Fatalf("a forged X-Forwarded-For got %d, want 429", rec.Code)
	}
}

func TestRateLimitUsesForwardedForBehindATrustedProxy(t *testing.T) {
	h := limited(t, api.RateLimitConfig{RPS: 0.5, Burst: 1, TrustProxy: true})

	// every request arrives from the proxy (10.0.0.9); the real client is in the header
	if rec := call(h, "/x", "10.0.0.9:5000", "203.0.113.5"); rec.Code != http.StatusOK {
		t.Fatalf("client A: got %d", rec.Code)
	}
	if rec := call(h, "/x", "10.0.0.9:5000", "203.0.113.5"); rec.Code != http.StatusTooManyRequests {
		t.Fatalf("client A again: got %d, want 429", rec.Code)
	}
	if rec := call(h, "/x", "10.0.0.9:5000", "203.0.113.6"); rec.Code != http.StatusOK {
		t.Fatalf("client B behind the same proxy: got %d, want 200", rec.Code)
	}
	// the last entry is the one our proxy added; an earlier, forged one is ignored
	if rec := call(h, "/x", "10.0.0.9:5000", "9.9.9.9, 203.0.113.5"); rec.Code != http.StatusTooManyRequests {
		t.Fatalf("forged first entry: got %d, want 429 (keyed on 203.0.113.5)", rec.Code)
	}
}

func TestRateLimitNeverLimitsHealthChecks(t *testing.T) {
	h := limited(t, api.RateLimitConfig{RPS: 0.5, Burst: 1})

	for i := 0; i < 20; i++ {
		if rec := call(h, "/healthz", "10.0.0.1:5000", ""); rec.Code != http.StatusOK {
			t.Fatalf("health check %d: got %d, want 200", i, rec.Code)
		}
	}
}

func TestRateLimitConcurrentClients(t *testing.T) {
	h := limited(t, api.RateLimitConfig{RPS: 0.5, Burst: 1})

	var wg sync.WaitGroup
	codes := make([]int, 100)
	for i := range codes {
		wg.Add(1)
		go func() {
			defer wg.Done()
			// 100 different clients, each sending 1 request: every one has a fresh bucket
			codes[i] = call(h, "/x", fmt.Sprintf("10.1.%d.%d:5000", i/250, i%250), "").Code
		}()
	}
	wg.Wait()

	for i, c := range codes {
		if c != http.StatusOK {
			t.Fatalf("client %d: got %d, want 200", i, c)
		}
	}
}
