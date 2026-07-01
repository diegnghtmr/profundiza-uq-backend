// Package ratelimit provides a fixed-window in-memory rate limiter and an
// HTTP middleware that applies it per client IP address. It is intentionally
// dependency-free: no external packages required beyond the standard library.
//
// Design notes:
//   - Fixed-window per IP so each window is independent and trivially testable.
//   - The clock is injectable so tests can run without sleeping.
//   - Client IP is taken from r.RemoteAddr only. X-Forwarded-For is NOT trusted
//     here because doing so safely requires knowing the trusted-proxy list; using
//     RemoteAddr is always correct when the app is behind a single trusted proxy
//     that rewrites RemoteAddr (e.g. a Kubernetes ingress).
package ratelimit

import (
	"net"
	"net/http"
	"sync"
	"time"

	"github.com/uniquindio/profundiza-uq/internal/platform/httpx"
)

// slot is a single fixed-window counter for one key.
type slot struct {
	count   int
	resetAt time.Time
}

// Limiter is a fixed-window in-memory rate limiter keyed by arbitrary strings
// (typically client IP addresses). It is safe for concurrent use.
type Limiter struct {
	mu      sync.Mutex
	windows map[string]*slot
	max     int
	period  time.Duration
	now     func() time.Time
}

// New creates a Limiter that allows at most max requests per period for each
// distinct key. Pass a non-nil now function to control time in tests; nil uses
// time.Now.
func New(max int, period time.Duration, now func() time.Time) *Limiter {
	if now == nil {
		now = time.Now
	}
	return &Limiter{
		windows: make(map[string]*slot),
		max:     max,
		period:  period,
		now:     now,
	}
}

// Allow returns true if the key is within the rate limit for the current
// fixed window, false if the limit has been reached. Each distinct key has its
// own independent counter.
func (l *Limiter) Allow(key string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()

	now := l.now()
	s, ok := l.windows[key]
	if !ok || now.After(s.resetAt) {
		// First request in a new window — or window just expired.
		l.windows[key] = &slot{count: 1, resetAt: now.Add(l.period)}
		return true
	}
	if s.count >= l.max {
		return false
	}
	s.count++
	return true
}

// IPMiddleware wraps h with per-IP rate limiting using l. When the limit is
// exceeded for the requesting IP, it writes 429 Too Many Requests and stops
// the middleware chain.
func IPMiddleware(l *Limiter) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ip, _, err := net.SplitHostPort(r.RemoteAddr)
			if err != nil {
				// Fallback: treat RemoteAddr as a bare IP (no port).
				ip = r.RemoteAddr
			}
			if !l.Allow(ip) {
				httpx.WriteError(w, r, http.StatusTooManyRequests, httpx.CodeTooManyRequests,
					"Too many requests. Please try again later.", nil)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}
