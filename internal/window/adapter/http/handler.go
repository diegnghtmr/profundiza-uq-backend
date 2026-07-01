// Package http is the driving adapter for enrollment windows.
package http

import (
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/uniquindio/profundiza-uq/internal/platform/authn"
	"github.com/uniquindio/profundiza-uq/internal/platform/httpx"
	"github.com/uniquindio/profundiza-uq/internal/window/app"
	"github.com/uniquindio/profundiza-uq/internal/window/domain"
)

// Handler adapts HTTP to the window Service.
type Handler struct{ svc *app.Service }

// NewHandler builds a window HTTP handler.
func NewHandler(svc *app.Service) *Handler { return &Handler{svc: svc} }

// Routes returns the /enrollment-windows router. Reads for any authenticated
// user; writes for admins.
func (h *Handler) Routes() chi.Router {
	r := chi.NewRouter()
	r.Get("/", h.list)
	r.Get("/{windowId}", h.get)
	r.Group(func(admin chi.Router) {
		admin.Use(authn.RequireRole(authn.RoleAdmin, authn.RoleSuperAdmin))
		admin.Post("/", h.create)
		admin.Patch("/{windowId}", h.update)
	})
	return r
}

type windowDTO struct {
	ID          string  `json:"id"`
	SemesterID  string  `json:"semesterId"`
	Name        string  `json:"name"`
	StartsAt    string  `json:"startsAt"`
	EndsAt      string  `json:"endsAt"`
	TargetShift *string `json:"targetShift"`
	Status      string  `json:"status"`
	CreatedAt   string  `json:"createdAt"`
	UpdatedAt   string  `json:"updatedAt"`
}

func toDTO(w domain.Window) windowDTO {
	return windowDTO{
		ID: w.ID, SemesterID: w.SemesterID, Name: w.Name,
		StartsAt: w.StartsAt.UTC().Format(time.RFC3339), EndsAt: w.EndsAt.UTC().Format(time.RFC3339),
		TargetShift: w.TargetShift, Status: w.Status,
		CreatedAt: w.CreatedAt.UTC().Format(time.RFC3339), UpdatedAt: w.UpdatedAt.UTC().Format(time.RFC3339),
	}
}

func (h *Handler) list(w http.ResponseWriter, r *http.Request) {
	semesterID := r.URL.Query().Get("semesterId")
	if semesterID == "" {
		httpx.WriteError(w, r, http.StatusBadRequest, httpx.CodeValidation, "semesterId is required.", nil)
		return
	}
	windows, err := h.svc.ListBySemester(r.Context(), semesterID)
	if err != nil {
		httpx.WriteError(w, r, http.StatusInternalServerError, httpx.CodeInternal, "Could not load windows.", nil)
		return
	}
	items := make([]windowDTO, 0, len(windows))
	for _, win := range windows {
		items = append(items, toDTO(win))
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (h *Handler) get(w http.ResponseWriter, r *http.Request) {
	win, err := h.svc.Get(r.Context(), chi.URLParam(r, "windowId"))
	if err != nil {
		writeErr(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, toDTO(*win))
}

type createReq struct {
	SemesterID  string  `json:"semesterId"`
	Name        string  `json:"name"`
	StartsAt    string  `json:"startsAt"`
	EndsAt      string  `json:"endsAt"`
	TargetShift *string `json:"targetShift"`
}

func (h *Handler) create(w http.ResponseWriter, r *http.Request) {
	var req createReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpx.WriteError(w, r, http.StatusBadRequest, httpx.CodeValidation, "Invalid body.", nil)
		return
	}
	startsAt, e1 := time.Parse(time.RFC3339, req.StartsAt)
	endsAt, e2 := time.Parse(time.RFC3339, req.EndsAt)
	if e1 != nil || e2 != nil {
		httpx.WriteError(w, r, http.StatusBadRequest, httpx.CodeValidation, "startsAt and endsAt must be RFC3339.", nil)
		return
	}
	win, err := h.svc.Create(r.Context(), app.CreateInput{
		SemesterID: req.SemesterID, Name: req.Name, StartsAt: startsAt, EndsAt: endsAt,
		TargetShift: req.TargetShift, ActorID: actorID(r)})
	if err != nil {
		writeErr(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusCreated, toDTO(win))
}

type updateReq struct {
	Name        *string `json:"name"`
	StartsAt    *string `json:"startsAt"`
	EndsAt      *string `json:"endsAt"`
	TargetShift *string `json:"targetShift"`
	Status      *string `json:"status"`
}

func (h *Handler) update(w http.ResponseWriter, r *http.Request) {
	var req updateReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpx.WriteError(w, r, http.StatusBadRequest, httpx.CodeValidation, "Invalid body.", nil)
		return
	}
	in := app.UpdateInput{ID: chi.URLParam(r, "windowId"), Name: req.Name, TargetShift: req.TargetShift, Status: req.Status, ActorID: actorID(r)}
	if req.StartsAt != nil {
		if t, err := time.Parse(time.RFC3339, *req.StartsAt); err == nil {
			in.StartsAt = &t
		}
	}
	if req.EndsAt != nil {
		if t, err := time.Parse(time.RFC3339, *req.EndsAt); err == nil {
			in.EndsAt = &t
		}
	}
	win, err := h.svc.Update(r.Context(), in)
	if err != nil {
		writeErr(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, toDTO(win))
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
		httpx.WriteError(w, r, http.StatusNotFound, httpx.CodeNotFound, "Window not found.", nil)
	case errors.Is(err, app.ErrInvalid):
		httpx.WriteError(w, r, http.StatusBadRequest, httpx.CodeValidation, "Invalid window input.", nil)
	default:
		httpx.WriteError(w, r, http.StatusInternalServerError, httpx.CodeInternal, "Could not complete the operation.", nil)
	}
}
