package tests

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"strings"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"github.com/aakash/godrop/internal/job"
	"github.com/aakash/godrop/internal/netguard"
	"github.com/aakash/godrop/internal/pool"
	"github.com/aakash/godrop/internal/processor"
)

func TestIsPublic(t *testing.T) {
	tests := []struct {
		addr string
		want bool
	}{
		{"93.184.216.34", true},
		{"8.8.8.8", true},
		{"1.1.1.1", true},
		{"2606:4700:4700::1111", true},
		{"::ffff:8.8.8.8", true},
		{"172.15.255.255", true},
		{"172.32.0.1", true},

		{"127.0.0.1", false},
		{"127.1.2.3", false},
		{"::1", false},
		{"10.0.0.1", false},
		{"172.16.0.1", false},
		{"172.31.255.255", false},
		{"192.168.1.1", false},
		{"fd00::1", false},
		{"169.254.169.254", false}, // cloud metadata address
		{"169.254.0.1", false},
		{"fe80::1", false},
		{"0.0.0.0", false},
		{"::", false},
		{"224.0.0.1", false},
		{"ff02::1", false},
		{"255.255.255.255", false},
		{"240.0.0.1", false},
		{"100.64.0.1", false},
		{"192.0.2.1", false},
		{"198.51.100.1", false},
		{"203.0.113.9", false},
		{"::ffff:127.0.0.1", false},
		{"::ffff:10.0.0.1", false},
		{"::ffff:169.254.169.254", false},
		{"64:ff9b::7f00:1", false}, // NAT64 form of 127.0.0.1
		{"2002:7f00:1::", false},   // 6to4 form of 127.0.0.1
	}

	for _, tc := range tests {
		t.Run(tc.addr, func(t *testing.T) {
			if got := netguard.IsPublic(netip.MustParseAddr(tc.addr)); got != tc.want {
				t.Fatalf("IsPublic(%s) = %v, want %v", tc.addr, got, tc.want)
			}
		})
	}
}

func TestControlFailsClosed(t *testing.T) {
	if err := netguard.Control("tcp", "8.8.8.8:443", nil); err != nil {
		t.Errorf("public address: %v", err)
	}
	for _, addr := range []string{"127.0.0.1:80", "[::1]:80", "10.0.0.1:5432", "not-an-address", "", "localhost:80"} {
		if err := netguard.Control("tcp", addr, nil); !errors.Is(err, netguard.ErrBlockedAddress) {
			t.Errorf("Control(%q) = %v, want ErrBlockedAddress", addr, err)
		}
	}
}

func countingServer(t *testing.T) (*httptest.Server, *atomic.Int32) {
	t.Helper()
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
	}))
	t.Cleanup(srv.Close)
	return srv, &hits
}

func TestSafeTransportNeverConnectsToLoopback(t *testing.T) {
	srv, hits := countingServer(t)
	client := &http.Client{Transport: netguard.NewTransport(), Timeout: 3 * time.Second}

	_, err := client.Get(srv.URL)
	if !errors.Is(err, netguard.ErrBlockedAddress) {
		t.Fatalf("got %v, want ErrBlockedAddress", err)
	}
	if hits.Load() != 0 {
		t.Fatalf("server received %d requests: connected before the check ran", hits.Load())
	}
}

func TestRedirectToAnInternalAddressIsBlocked(t *testing.T) {
	b, hitsB := countingServer(t) // stands in for an internal service
	a := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, b.URL, http.StatusFound) // "the public site" redirects inwards
	}))
	t.Cleanup(a.Close)
	aAddr := strings.TrimPrefix(a.URL, "http://")

	// control: with no address check, the redirect is followed
	open := &http.Client{Transport: netguard.NewTransportWithControl(
		func(string, string, syscall.RawConn) error { return nil },
	)}
	if resp, err := open.Get(a.URL); err != nil {
		t.Fatalf("unguarded redirect should work: %v", err)
	} else {
		resp.Body.Close()
	}
	if hitsB.Load() != 1 {
		t.Fatalf("unguarded client should have reached b, hits = %d", hitsB.Load())
	}

	// guarded: let the first hop (a) through, apply real rules to everything else
	control := func(network, address string, c syscall.RawConn) error {
		if address == aAddr {
			return nil
		}
		return netguard.Control(network, address, c)
	}
	guarded := &http.Client{Transport: netguard.NewTransportWithControl(control)}

	_, err := guarded.Get(a.URL)
	if !errors.Is(err, netguard.ErrBlockedAddress) {
		t.Fatalf("got %v, want ErrBlockedAddress on the redirect", err)
	}
	if hitsB.Load() != 1 {
		t.Fatalf("b was reached through the redirect, hits = %d, want 1", hitsB.Load())
	}
}

func TestSafeFetcherBlocksLoopbackByAddressAndByName(t *testing.T) {
	srv, hits := countingServer(t)
	f := processor.NewSafeFetcher()

	byName := strings.Replace(srv.URL, "127.0.0.1", "localhost", 1)
	for _, url := range []string{srv.URL, byName} {
		if _, err := f.Process(context.Background(), url); !errors.Is(err, netguard.ErrBlockedAddress) {
			t.Errorf("%s: got %v, want ErrBlockedAddress", url, err)
		}
	}
	if hits.Load() != 0 {
		t.Fatalf("server received %d requests", hits.Load())
	}

	// the permissive fetcher is unchanged
	if page, err := processor.NewFetcher().Process(context.Background(), srv.URL); err != nil || page.StatusCode != 200 {
		t.Fatalf("permissive fetcher: got (%+v, %v)", page, err)
	}
}

func TestAPIHidesTheAddressOfBlockedURLs(t *testing.T) {
	blocked := pool.ProcessorFunc[job.URLJob, processor.Page](
		func(ctx context.Context, j job.URLJob) (processor.Page, error) {
			// what the HTTP client actually returns: names the resolved IP
			return processor.Page{}, fmt.Errorf(`Get "http://db.internal/": dial tcp 10.1.2.3:5432: %w`, netguard.ErrBlockedAddress)
		},
	)
	ts := newAPI(t, blocked, 1, 10)

	id := submitOK(t, ts.URL, "http://db.internal/")
	rec := waitForStatus(t, ts.URL, id, job.StatusFailed)

	if strings.Contains(rec.Error, "10.1.2.3") || !strings.Contains(rec.Error, "non-public") {
		t.Fatalf("Error = %q: must not reveal the address", rec.Error)
	}
}
