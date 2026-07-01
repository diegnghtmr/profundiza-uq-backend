package authn_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/uniquindio/profundiza-uq/internal/platform/authn"
	"github.com/uniquindio/profundiza-uq/internal/shared/reqmeta"
)

// noopAuth always returns nil to simulate an unauthenticated (or
// unrecognised) session; we only care about the IP/UA side-effect here.
type noopAuth struct{}

func (noopAuth) Authenticate(_ context.Context, _ string) (*authn.Principal, error) {
	return nil, nil
}

// TestMiddleware_populatesReqMeta proves that authn.Middleware stores the
// client IP (port stripped) and User-Agent in the context via reqmeta.
func TestMiddleware_populatesReqMeta(t *testing.T) {
	var capturedMeta reqmeta.Meta

	inner := http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		capturedMeta = reqmeta.RequestMetaFrom(r.Context())
	})

	h := authn.Middleware(noopAuth{})(inner)

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.RemoteAddr = "203.0.113.42:54321"
	req.Header.Set("User-Agent", "TestBrowser/9.0")

	h.ServeHTTP(httptest.NewRecorder(), req)

	if capturedMeta.IPAddress != "203.0.113.42" {
		t.Fatalf("want IPAddress=203.0.113.42 got %q", capturedMeta.IPAddress)
	}
	if capturedMeta.UserAgent != "TestBrowser/9.0" {
		t.Fatalf("want UserAgent=TestBrowser/9.0 got %q", capturedMeta.UserAgent)
	}
}

// TestMiddleware_bareRemoteAddr proves the middleware handles a RemoteAddr
// that is already a bare IP (no port), as can happen in some proxy setups.
func TestMiddleware_bareRemoteAddr(t *testing.T) {
	var capturedMeta reqmeta.Meta

	inner := http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		capturedMeta = reqmeta.RequestMetaFrom(r.Context())
	})

	h := authn.Middleware(noopAuth{})(inner)

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.RemoteAddr = "203.0.113.1" // no port
	req.Header.Set("User-Agent", "CurlBot/1")

	h.ServeHTTP(httptest.NewRecorder(), req)

	if capturedMeta.IPAddress != "203.0.113.1" {
		t.Fatalf("want IPAddress=203.0.113.1 got %q", capturedMeta.IPAddress)
	}
}
