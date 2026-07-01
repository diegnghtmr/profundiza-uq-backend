// Package http is the driving adapter exposing semester use cases over REST.
package http

import (
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/uniquindio/profundiza-uq/internal/platform/authn"
	"github.com/uniquindio/profundiza-uq/internal/platform/httpx"
	"github.com/uniquindio/profundiza-uq/internal/semester/app"
	"github.com/uniquindio/profundiza-uq/internal/semester/domain"
)

// Handler adapts HTTP requests to the semester Service.
type Handler struct {
	svc *app.Service
}

// NewHandler builds a semester HTTP handler.
func NewHandler(svc *app.Service) *Handler {
	return &Handler{svc: svc}
}

// Routes returns the chi router for the semester resource. Reads are available
// to any authenticated user; writes require an administrator.
func (h *Handler) Routes() chi.Router {
	r := chi.NewRouter()
	r.Get("/", h.list)
	r.Get("/{semesterId}", h.get)

	r.Group(func(admin chi.Router) {
		admin.Use(authn.RequireRole(authn.RoleAdmin, authn.RoleSuperAdmin))
		admin.Post("/", h.create)
		admin.Post("/{semesterId}/activate", h.activate)
		admin.Post("/{semesterId}/close", h.close)
	})
	return r
}

type semesterDTO struct {
	ID        string `json:"id"`
	Code      string `json:"code"`
	Name      string `json:"name"`
	StartsAt  string `json:"startsAt"`
	EndsAt    string `json:"endsAt"`
	Status    string `json:"status"`
	CreatedAt string `json:"createdAt"`
	UpdatedAt string `json:"updatedAt"`
}

func toDTO(s domain.Semester) semesterDTO {
	return semesterDTO{
		ID: s.ID, Code: s.Code, Name: s.Name,
		StartsAt: s.StartsAt.UTC().Format(time.RFC3339), EndsAt: s.EndsAt.UTC().Format(time.RFC3339),
		Status: string(s.Status), CreatedAt: s.CreatedAt.UTC().Format(time.RFC3339), UpdatedAt: s.UpdatedAt.UTC().Format(time.RFC3339),
	}
}

func (h *Handler) list(w http.ResponseWriter, r *http.Request) {
	semesters, err := h.svc.List(r.Context())
	if err != nil {
		httpx.WriteError(w, r, http.StatusInternalServerError, httpx.CodeInternal, "Could not load semesters.", nil)
		return
	}
	out := make([]semesterDTO, 0, len(semesters))
	for _, s := range semesters {
		out = append(out, toDTO(s))
	}
	httpx.WriteJSON(w, http.StatusOK, out)
}

func (h *Handler) get(w http.ResponseWriter, r *http.Request) {
	s, err := h.svc.Get(r.Context(), chi.URLParam(r, "semesterId"))
	if err != nil {
		writeErr(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, toDTO(*s))
}

type createReq struct {
	Code     string `json:"code"`
	Name     string `json:"name"`
	StartsAt string `json:"startsAt"`
	EndsAt   string `json:"endsAt"`
}

func (h *Handler) create(w http.ResponseWriter, r *http.Request) {
	var req createReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpx.WriteError(w, r, http.StatusBadRequest, httpx.CodeValidation, "Invalid body.", nil)
		return
	}
	startsAt, err1 := time.Parse(time.RFC3339, req.StartsAt)
	endsAt, err2 := time.Parse(time.RFC3339, req.EndsAt)
	if err1 != nil || err2 != nil {
		httpx.WriteError(w, r, http.StatusBadRequest, httpx.CodeValidation, "startsAt and endsAt must be RFC3339 timestamps.", nil)
		return
	}
	s, err := h.svc.Create(r.Context(), app.CreateInput{
		Code: req.Code, Name: req.Name, StartsAt: startsAt, EndsAt: endsAt, ActorID: actorID(r),
	})
	if err != nil {
		writeErr(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusCreated, toDTO(s))
}

func (h *Handler) activate(w http.ResponseWriter, r *http.Request) {
	s, err := h.svc.Activate(r.Context(), chi.URLParam(r, "semesterId"), actorID(r))
	if err != nil {
		writeErr(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, toDTO(*s))
}

type reasonReq struct {
	Reason string `json:"reason"`
}

func (h *Handler) close(w http.ResponseWriter, r *http.Request) {
	var req reasonReq
	_ = json.NewDecoder(r.Body).Decode(&req)
	s, err := h.svc.Close(r.Context(), chi.URLParam(r, "semesterId"), actorID(r), req.Reason)
	if err != nil {
		writeErr(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, toDTO(*s))
}

func actorID(r *http.Request) string {
	if p := authn.FromContext(r.Context()); p != nil {
		return p.SubjectID
	}
	return ""
}

func writeErr(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, app.ErrNotFound):
		httpx.WriteError(w, r, http.StatusNotFound, httpx.CodeNotFound, "Semester not found.", nil)
	case errors.Is(err, app.ErrInvalid):
		httpx.WriteError(w, r, http.StatusBadRequest, httpx.CodeValidation, "Invalid semester input (check dates and reason).", nil)
	default:
		httpx.WriteError(w, r, http.StatusInternalServerError, httpx.CodeInternal, "Could not complete the operation.", nil)
	}
}
