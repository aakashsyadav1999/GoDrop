package api

import (
	"context"
	"math"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"golang.org/x/time/rate"
)

const (
	clientIdleTimeout = 10 * time.Minute // forget a client after this long without a request
	sweepInterval     = time.Minute
)

type RateLimitConfig struct {
	RPS        float64 // sustained requests per second, per client
	Burst      int     // how many requests a client may make at once
	TrustProxy bool    // read the client address from X-Forwarded-For (only behind a proxy you control)
}

type client struct {
	limiter  *rate.Limiter
	lastSeen time.Time
}

// clientLimiters holds one token-bucket limiter per client address.
type clientLimiters struct {
	mu      sync.Mutex
	clients map[string]*client
	rps     rate.Limit
	burst   int
}

func (l *clientLimiters) get(addr string) *rate.Limiter {
	l.mu.Lock()
	defer l.mu.Unlock()

	c, ok := l.clients[addr]
	if !ok {
		c = &client{limiter: rate.NewLimiter(l.rps, l.burst)}
		l.clients[addr] = c
	}
	c.lastSeen = time.Now()
	return c.limiter
}

// sweep forgets clients that have been idle for longer than idle, so the map
// cannot grow without bound.
func (l *clientLimiters) sweep(idle time.Duration) {
	l.mu.Lock()
	defer l.mu.Unlock()

	cutoff := time.Now().Add(-idle)
	for addr, c := range l.clients {
		if c.lastSeen.Before(cutoff) {
			delete(l.clients, addr)
		}
	}
}

func (l *clientLimiters) janitor(ctx context.Context) {
	ticker := time.NewTicker(sweepInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			l.sweep(clientIdleTimeout)
		case <-ctx.Done():
			return
		}
	}
}

// RateLimit answers 429 to clients that send requests faster than cfg allows.
// ctx stops the background cleanup goroutine. /healthz is never limited.
func RateLimit(ctx context.Context, cfg RateLimitConfig, next http.Handler) http.Handler {
	limiters := &clientLimiters{
		clients: make(map[string]*client),
		rps:     rate.Limit(cfg.RPS),
		burst:   cfg.Burst,
	}
	go limiters.janitor(ctx)

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/healthz" {
			next.ServeHTTP(w, r)
			return
		}

		res := limiters.get(clientIP(r, cfg.TrustProxy)).Reserve()
		if delay := res.Delay(); delay > 0 {
			res.Cancel() // give the token back: this request is not going ahead
			w.Header().Set("Retry-After", strconv.Itoa(int(math.Ceil(delay.Seconds()))))
			writeError(w, http.StatusTooManyRequests, "rate limit exceeded, slow down")
			return
		}

		next.ServeHTTP(w, r)
	})
}

// clientIP identifies who is calling. X-Forwarded-For is only believed when
// trustProxy is set, because any client can send that header. The last entry is
// the one added by our own proxy; earlier entries could have been forged.
func clientIP(r *http.Request, trustProxy bool) string {
	if trustProxy {
		if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
			parts := strings.Split(xff, ",")
			return strings.TrimSpace(parts[len(parts)-1])
		}
	}

	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}
