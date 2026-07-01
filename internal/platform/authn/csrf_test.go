package authn_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/uniquindio/profundiza-uq/internal/platform/authn"
)

// passHandler is a trivial inner handler that returns 200 OK.
var passHandler = http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
	w.WriteHeader(http.StatusOK)
})

// TestRequireCSRF_POST_WithoutToken_Returns403 proves that a state-changing
// (POST) request from an authenticated principal without the X-CSRF-Token
// header is rejected with 403 Forbidden.
func TestRequireCSRF_POST_WithoutToken_Returns403(t *testing.T) {
	handler := authn.RequireCSRF(passHandler)
	p := &authn.Principal{
		SessionID: "sess-1",
		CSRFToken: "secret-csrf-token",
	}
	req := httptest.NewRequest(http.MethodPost, "/api/v1/enrollment-requests", nil)
	req = req.WithContext(authn.WithPrincipal(req.Context(), p))
	// No X-CSRF-Token header.
	rr := httptest.NewRecorder()

	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusForbidden {
		t.Fatalf("want 403, got %d", rr.Code)
	}
}

// TestRequireCSRF_POST_WithCorrectToken_Passes proves that the same request
// WITH the correct X-CSRF-Token header passes through to the inner handler.
func TestRequireCSRF_POST_WithCorrectToken_Passes(t *testing.T) {
	handler := authn.RequireCSRF(passHandler)
	p := &authn.Principal{
		SessionID: "sess-1",
		CSRFToken: "secret-csrf-token",
	}
	req := httptest.NewRequest(http.MethodPost, "/api/v1/enrollment-requests", nil)
	req.Header.Set("X-CSRF-Token", "secret-csrf-token")
	req = req.WithContext(authn.WithPrincipal(req.Context(), p))
	rr := httptest.NewRecorder()

	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("want 200, got %d", rr.Code)
	}
}

// TestRequireCSRF_POST_WithWrongToken_Returns403 proves that a mismatched
// token (e.g. an attacker replaying a stolen token from another session) is
// also rejected with 403.
func TestRequireCSRF_POST_WithWrongToken_Returns403(t *testing.T) {
	handler := authn.RequireCSRF(passHandler)
	p := &authn.Principal{
		SessionID: "sess-1",
		CSRFToken: "secret-csrf-token",
	}
	req := httptest.NewRequest(http.MethodPost, "/api/v1/enrollment-requests", nil)
	req.Header.Set("X-CSRF-Token", "wrong-token")
	req = req.WithContext(authn.WithPrincipal(req.Context(), p))
	rr := httptest.NewRecorder()

	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusForbidden {
		t.Fatalf("want 403, got %d", rr.Code)
	}
}

// TestRequireCSRF_GET_IsExempt ensures GET requests bypass the CSRF check
// entirely so the SPA can call read-only endpoints without the token header.
func TestRequireCSRF_GET_IsExempt(t *testing.T) {
	handler := authn.RequireCSRF(passHandler)
	p := &authn.Principal{
		SessionID: "sess-1",
		CSRFToken: "secret-csrf-token",
	}
	req := httptest.NewRequest(http.MethodGet, "/me", nil)
	req = req.WithContext(authn.WithPrincipal(req.Context(), p))
	// Intentionally no X-CSRF-Token header.
	rr := httptest.NewRecorder()

	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("GET should be exempt from CSRF, want 200, got %d", rr.Code)
	}
}

// TestRequireCSRF_PUT_WithCorrectToken_Passes proves PUT is also guarded and
// passes with the correct token.
func TestRequireCSRF_PUT_WithCorrectToken_Passes(t *testing.T) {
	handler := authn.RequireCSRF(passHandler)
	p := &authn.Principal{CSRFToken: "tok"}
	req := httptest.NewRequest(http.MethodPut, "/api/v1/something", nil)
	req.Header.Set("X-CSRF-Token", "tok")
	req = req.WithContext(authn.WithPrincipal(req.Context(), p))
	rr := httptest.NewRecorder()

	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("want 200, got %d", rr.Code)
	}
}

// TestRequireCSRF_DELETE_WithoutToken_Returns403 proves DELETE is also guarded.
func TestRequireCSRF_DELETE_WithoutToken_Returns403(t *testing.T) {
	handler := authn.RequireCSRF(passHandler)
	p := &authn.Principal{CSRFToken: "tok"}
	req := httptest.NewRequest(http.MethodDelete, "/api/v1/something", nil)
	req = req.WithContext(authn.WithPrincipal(req.Context(), p))
	rr := httptest.NewRecorder()

	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusForbidden {
		t.Fatalf("want 403, got %d", rr.Code)
	}
}
