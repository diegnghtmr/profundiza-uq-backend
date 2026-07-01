// Package authn carries the authenticated principal across the request pipeline
// and provides the session/role guard middleware. It depends on nothing
// framework-specific beyond net/http.
package authn

import (
	"context"
	"net"
	"net/http"

	"github.com/uniquindio/profundiza-uq/internal/platform/httpx"
	"github.com/uniquindio/profundiza-uq/internal/shared/reqmeta"
)

// CookieName is the session cookie name (matches the OpenAPI securityScheme).
const CookieName = "session_id"

// Role is the authorization role of the principal.
type Role string

const (
	RoleStudent    Role = "STUDENT"
	RoleAdmin      Role = "ADMIN"
	RoleSuperAdmin Role = "SUPER_ADMIN"
)

// SubjectType distinguishes the backing record of a principal.
type SubjectType string

const (
	SubjectStudent SubjectType = "STUDENT"
	SubjectAdmin   SubjectType = "ADMIN"
)

// Principal is the authenticated identity attached to a request.
type Principal struct {
	SessionID   string
	Role        Role
	SubjectType SubjectType
	SubjectID   string
	Email       string
	FullName    string
	CSRFToken   string
}

// StudentID returns the student id when the principal is a student.
func (p *Principal) StudentID() (string, bool) {
	if p.SubjectType == SubjectStudent {
		return p.SubjectID, true
	}
	return "", false
}

// AdminUserID returns the admin id when the principal is an admin/superadmin.
func (p *Principal) AdminUserID() (string, bool) {
	if p.SubjectType == SubjectAdmin {
		return p.SubjectID, true
	}
	return "", false
}

// Authenticator resolves a session id into a principal.
type Authenticator interface {
	Authenticate(ctx context.Context, sessionID string) (*Principal, error)
}

type ctxKey string

const principalKey ctxKey = "principal"

// WithPrincipal returns a context carrying the principal.
func WithPrincipal(ctx context.Context, p *Principal) context.Context {
	return context.WithValue(ctx, principalKey, p)
}

// FromContext returns the principal on the context, or nil.
func FromContext(ctx context.Context) *Principal {
	if p, ok := ctx.Value(principalKey).(*Principal); ok {
		return p
	}
	return nil
}

// Middleware reads the session cookie and, when valid, attaches the principal to
// the request context. It never rejects on its own — guards do that — so it can
// front both public and protected routes.
//
// It also stores the client IP address (port stripped) and the User-Agent
// header in the context via reqmeta so that downstream audit writes can
// automatically record them without any call-site changes.
func Middleware(a Authenticator) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Capture client IP and User-Agent for audit context.
			ip := r.RemoteAddr
			if host, _, err := net.SplitHostPort(r.RemoteAddr); err == nil {
				ip = host
			}
			r = r.WithContext(reqmeta.WithRequestMeta(r.Context(), ip, r.Header.Get("User-Agent")))

			cookie, err := r.Cookie(CookieName)
			if err == nil && cookie.Value != "" {
				if p, err := a.Authenticate(r.Context(), cookie.Value); err == nil && p != nil {
					r = r.WithContext(WithPrincipal(r.Context(), p))
				}
			}
			next.ServeHTTP(w, r)
		})
	}
}

// RequireAuth rejects requests without an authenticated principal.
func RequireAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if FromContext(r.Context()) == nil {
			httpx.WriteError(w, r, http.StatusUnauthorized, httpx.CodeUnauthorized, "Authentication required.", nil)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// RequireCSRF validates the X-CSRF-Token request header for state-changing
// HTTP methods (POST, PUT, PATCH, DELETE) on authenticated routes.
//
// The CSRF token is generated at login time, stored in the session, and
// returned to the SPA in the login-verify and /me responses.
// The SPA must send the token back in the X-CSRF-Token header on every
// state-changing request.
//
// Safe methods (GET, HEAD, OPTIONS) and requests without an authenticated
// principal (which RequireAuth already rejects before this middleware runs on
// protected groups) are always exempt.
func RequireCSRF(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet, http.MethodHead, http.MethodOptions:
			// Safe methods carry no state-changing side-effects per RFC 9110;
			// CSRF tokens are not required for them.
			next.ServeHTTP(w, r)
			return
		}
		p := FromContext(r.Context())
		if p == nil {
			// No authenticated principal: the route is public or RequireAuth
			// already handled this. Pass through — do not block.
			next.ServeHTTP(w, r)
			return
		}
		token := r.Header.Get("X-CSRF-Token")
		if token == "" || token != p.CSRFToken {
			httpx.WriteError(w, r, http.StatusForbidden, httpx.CodeForbidden,
				"CSRF token missing or invalid.", nil)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// RequireRole rejects principals whose role is not in the allowed set. It
// implies RequireAuth.
func RequireRole(roles ...Role) func(http.Handler) http.Handler {
	allowed := make(map[Role]bool, len(roles))
	for _, role := range roles {
		allowed[role] = true
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			p := FromContext(r.Context())
			if p == nil {
				httpx.WriteError(w, r, http.StatusUnauthorized, httpx.CodeUnauthorized, "Authentication required.", nil)
				return
			}
			if !allowed[p.Role] {
				httpx.WriteError(w, r, http.StatusForbidden, httpx.CodeForbidden, "You do not have access to this resource.", nil)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}
