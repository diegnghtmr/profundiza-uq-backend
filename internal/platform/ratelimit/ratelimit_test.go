package ratelimit_test

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/uniquindio/profundiza-uq/internal/platform/ratelimit"
)

// TestLimiter_Allow_BlocksAfterMax proves that after max requests the next one
// is denied. This is the core behaviour required by the OTP rate limiter.
func TestLimiter_Allow_BlocksAfterMax(t *testing.T) {
	const max = 3
	now := time.Now()
	l := ratelimit.New(max, 10*time.Second, func() time.Time { return now })

	for i := 0; i < max; i++ {
		if !l.Allow("192.168.1.1") {
			t.Fatalf("request %d/%d should be allowed", i+1, max)
		}
	}
	if l.Allow("192.168.1.1") {
		t.Fatal("request after threshold should be blocked")
	}
}

// TestLimiter_Allow_DifferentKeysAreIndependent verifies that rate counters are
// isolated per key so one IP exhausting its quota does not affect another IP.
func TestLimiter_Allow_DifferentKeysAreIndependent(t *testing.T) {
	now := time.Now()
	l := ratelimit.New(1, 10*time.Second, func() time.Time { return now })

	if !l.Allow("1.2.3.4") {
		t.Fatal("first key: first request should be allowed")
	}
	if l.Allow("1.2.3.4") {
		t.Fatal("first key: second request should be blocked")
	}
	// Completely different IP must start with a fresh counter.
	if !l.Allow("5.6.7.8") {
		t.Fatal("second key: first request should be allowed")
	}
}

// TestLimiter_Allow_WindowResets ensures the counter resets after the window
// period, allowing requests again (injectable clock makes this deterministic).
func TestLimiter_Allow_WindowResets(t *testing.T) {
	now := time.Now()
	l := ratelimit.New(1, 10*time.Second, func() time.Time { return now })

	if !l.Allow("1.2.3.4") {
		t.Fatal("should be allowed in first window")
	}
	if l.Allow("1.2.3.4") {
		t.Fatal("second request in same window should be blocked")
	}
	// Advance time past the window boundary.
	now = now.Add(11 * time.Second)
	if !l.Allow("1.2.3.4") {
		t.Fatal("first request in new window should be allowed")
	}
}

// TestIPMiddleware_Returns429WhenExceeded is the HTTP-level proof required by
// Fix C TDD: N rapid requests from the same IP eventually yield 429.
func TestIPMiddleware_Returns429WhenExceeded(t *testing.T) {
	const max = 2
	now := time.Now()
	l := ratelimit.New(max, 10*time.Second, func() time.Time { return now })

	inner := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	handler := ratelimit.IPMiddleware(l)(inner)

	for i := 0; i < max; i++ {
		req := httptest.NewRequest(http.MethodPost, "/auth/login/start", nil)
		req.RemoteAddr = "10.0.0.1:9999"
		rr := httptest.NewRecorder()
		handler.ServeHTTP(rr, req)
		if rr.Code != http.StatusOK {
			t.Fatalf("request %d/%d: want 200, got %d", i+1, max, rr.Code)
		}
	}

	// The (max+1)th request must be rate-limited.
	req := httptest.NewRequest(http.MethodPost, "/auth/login/start", nil)
	req.RemoteAddr = "10.0.0.1:9999"
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)
	if rr.Code != http.StatusTooManyRequests {
		t.Fatalf("want 429 after limit exceeded, got %d", rr.Code)
	}
}

// TestIPMiddleware_DifferentIPsAreIndependent ensures that a different client
// IP is not affected when another IP hits its limit.
func TestIPMiddleware_DifferentIPsAreIndependent(t *testing.T) {
	now := time.Now()
	l := ratelimit.New(1, 10*time.Second, func() time.Time { return now })

	inner := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	handler := ratelimit.IPMiddleware(l)(inner)

	// Exhaust quota for IP A.
	for i := 0; i < 2; i++ {
		req := httptest.NewRequest(http.MethodPost, "/auth/login/start", nil)
		req.RemoteAddr = "10.0.0.1:1111"
		rr := httptest.NewRecorder()
		handler.ServeHTTP(rr, req)
		_ = rr
	}

	// IP B should still be allowed.
	req := httptest.NewRequest(http.MethodPost, "/auth/login/start", nil)
	req.RemoteAddr = "10.0.0.2:2222"
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("unrelated IP should not be rate-limited, got %d", rr.Code)
	}
}
