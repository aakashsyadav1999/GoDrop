package processor

import (
	"context"
	"io"
	"net/http"

	"github.com/aakash/godrop/internal/netguard"
	"github.com/aakash/godrop/internal/pool"
)

// maxBodyBytes caps how much of a response is read, so one URL cannot tie a worker up downloading forever.
const maxBodyBytes = 1 << 20

// Page is what a successful fetch produces. It has room to grow (body size, title, ...).
type Page struct {
	StatusCode int
}

// Fetcher fetches a URL and reports its status code.
type Fetcher struct {
	client *http.Client
}

// Compile-time check that *Fetcher is a pool.Processor.
var _ pool.Processor[string, Page] = (*Fetcher)(nil)

// NewFetcher allows private and loopback addresses; use it for the CLI and development.
func NewFetcher() *Fetcher {
	return &Fetcher{client: &http.Client{}}
}

// NewSafeFetcher refuses to connect to anything that is not a public address.
func NewSafeFetcher() *Fetcher {
	return &Fetcher{client: &http.Client{Transport: netguard.NewTransport()}}
}

// Process fetches url. A 404 is not an error: the server answered, so it comes
// back as a Page with StatusCode 404. Errors mean the request itself failed.
func (f *Fetcher) Process(ctx context.Context, url string) (Page, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return Page{}, err
	}

	resp, err := f.client.Do(req)
	if err != nil {
		return Page{}, err
	}
	defer resp.Body.Close()

	// Read the body so the connection can be reused for the next request.
	if _, err := io.Copy(io.Discard, io.LimitReader(resp.Body, maxBodyBytes)); err != nil {
		return Page{}, err
	}

	return Page{StatusCode: resp.StatusCode}, nil
}
