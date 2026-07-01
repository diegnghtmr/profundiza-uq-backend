// Package http is the driving adapter for identity: login, logout, and the
// current-user endpoint, including secure session-cookie management.
package http

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/uniquindio/profundiza-uq/internal/identity/app"
	"github.com/uniquindio/profundiza-uq/internal/platform/authn"
	"github.com/uniquindio/profundiza-uq/internal/platform/httpx"
	"github.com/uniquindio/profundiza-uq/internal/platform/ratelimit"
)

// Handler adapts HTTP to the identity AuthService.
type Handler struct {
	svc          *app.AuthService
	cookieSecure bool
	sessionTTL   time.Duration
	logger       *slog.Logger
	// loginLimiter gates /login/start and /login/verify per client IP
	// Nil means no rate limiting (useful in tests).
	loginLimiter *ratelimit.Limiter
}

// NewHandler builds an identity HTTP handler. loginLimiter may be nil (no
// rate limiting); in production a non-nil limiter should always be passed to
// guard against OTP brute-force.
func NewHandler(svc *app.AuthService, cookieSecure bool, sessionTTL time.Duration, logger *slog.Logger, loginLimiter *ratelimit.Limiter) *Handler {
	return &Handler{svc: svc, cookieSecure: cookieSecure, sessionTTL: sessionTTL, logger: logger, loginLimiter: loginLimiter}
}

// AuthRoutes returns the router mounted at /auth.
func (h *Handler) AuthRoutes() chi.Router {
	r := chi.NewRouter()
	// Rate-limit /login/start and /login/verify per IP.
	// /logout is intentionally excluded — it carries no brute-force risk.
	if h.loginLimiter != nil {
		limiterMiddleware := ratelimit.IPMiddleware(h.loginLimiter)
		r.With(limiterMiddleware).Post("/login/start", h.startLogin)
		r.With(limiterMiddleware).Post("/login/verify", h.verifyLogin)
	} else {
		r.Post("/login/start", h.startLogin)
		r.Post("/login/verify", h.verifyLogin)
	}
	r.Post("/logout", h.logout)
	return r
}

// Me handles GET /me (requires an authenticated principal in context).
func (h *Handler) Me(w http.ResponseWriter, r *http.Request) {
	p := authn.FromContext(r.Context())
	if p == nil {
		httpx.WriteError(w, r, http.StatusUnauthorized, httpx.CodeUnauthorized, "Authentication required.", nil)
		return
	}
	user, err := h.svc.CurrentUser(r.Context(), p)
	if err != nil {
		httpx.WriteError(w, r, http.StatusUnauthorized, httpx.CodeUnauthorized, "Session is no longer valid.", nil)
		return
	}
	// Include the CSRF token so the SPA can read it on page
	// load (GET /me is the typical bootstrap call) and send it back as the
	// X-CSRF-Token header on subsequent state-changing requests.
	httpx.WriteJSON(w, http.StatusOK, map[string]any{
		"user":      currentUserDTO(user),
		"csrfToken": p.CSRFToken,
	})
}

type startLoginReq struct {
	Email string `json:"email"`
}

func (h *Handler) startLogin(w http.ResponseWriter, r *http.Request) {
	var req startLoginReq
	if !decode(w, r, &req) || req.Email == "" {
		httpx.WriteError(w, r, http.StatusBadRequest, httpx.CodeValidation, "A valid email is required.", nil)
		return
	}
	res, err := h.svc.StartLogin(r.Context(), req.Email)
	if err != nil {
		h.logger.ErrorContext(r.Context(), "start_login_failed", slog.String("traceId", httpx.TraceID(r.Context())), slog.Any("error", err))
		httpx.WriteError(w, r, http.StatusInternalServerError, httpx.CodeInternal, "Could not start login.", nil)
		return
	}
	httpx.WriteJSON(w, http.StatusAccepted, map[string]any{
		"delivery":         "EMAIL_SENT",
		"expiresInSeconds": res.ExpiresInSeconds,
	})
}

type verifyLoginReq struct {
	Email string `json:"email"`
	Code  string `json:"code"`
}

func (h *Handler) verifyLogin(w http.ResponseWriter, r *http.Request) {
	var req verifyLoginReq
	if !decode(w, r, &req) || req.Email == "" || len(req.Code) < 6 {
		httpx.WriteError(w, r, http.StatusBadRequest, httpx.CodeValidation, "Email and a 6-digit code are required.", nil)
		return
	}
	principal, sessionID, err := h.svc.VerifyLogin(r.Context(), req.Email, req.Code)
	if err != nil {
		httpx.WriteError(w, r, http.StatusUnauthorized, httpx.CodeUnauthorized, "Invalid or expired code.", nil)
		return
	}
	h.setSessionCookie(w, sessionID)
	// Return the CSRF token on login so the SPA can store it
	// in memory and send it as X-CSRF-Token on subsequent state-changing calls.
	httpx.WriteJSON(w, http.StatusOK, map[string]any{
		"user":      principalDTO(principal),
		"csrfToken": principal.CSRFToken,
	})
}

func (h *Handler) logout(w http.ResponseWriter, r *http.Request) {
	if c, err := r.Cookie(authn.CookieName); err == nil && c.Value != "" {
		_ = h.svc.Logout(r.Context(), c.Value)
	}
	h.clearSessionCookie(w)
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) setSessionCookie(w http.ResponseWriter, sessionID string) {
	http.SetCookie(w, &http.Cookie{
		Name:     authn.CookieName,
		Value:    sessionID,
		Path:     "/",
		HttpOnly: true,
		Secure:   h.cookieSecure,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   int(h.sessionTTL.Seconds()),
	})
}

func (h *Handler) clearSessionCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name:     authn.CookieName,
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		Secure:   h.cookieSecure,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   -1,
	})
}

// --- DTOs ---

type currentUser struct {
	ID          string  `json:"id"`
	Email       string  `json:"email"`
	FullName    string  `json:"fullName"`
	Role        string  `json:"role"`
	StudentID   *string `json:"studentId"`
	AdminUserID *string `json:"adminUserId"`
}

func currentUserDTO(u *app.DirectoryUser) currentUser {
	out := currentUser{ID: u.SubjectID, Email: u.Email, FullName: u.FullName, Role: string(u.Role)}
	id := u.SubjectID
	if u.SubjectType == authn.SubjectStudent {
		out.StudentID = &id
	} else {
		out.AdminUserID = &id
	}
	return out
}

func principalDTO(p *authn.Principal) currentUser {
	out := currentUser{ID: p.SubjectID, Email: p.Email, FullName: p.FullName, Role: string(p.Role)}
	id := p.SubjectID
	if p.SubjectType == authn.SubjectStudent {
		out.StudentID = &id
	} else {
		out.AdminUserID = &id
	}
	return out
}

func decode(w http.ResponseWriter, r *http.Request, dst any) bool {
	defer r.Body.Close()
	if err := json.NewDecoder(r.Body).Decode(dst); err != nil {
		return false
	}
	return true
}
