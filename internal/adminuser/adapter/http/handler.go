// Package http is the driving adapter exposing admin-user use cases over REST.
package http

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/uniquindio/profundiza-uq/internal/adminuser/app"
	"github.com/uniquindio/profundiza-uq/internal/adminuser/domain"
	"github.com/uniquindio/profundiza-uq/internal/platform/authn"
	"github.com/uniquindio/profundiza-uq/internal/platform/httpx"
)

// Handler adapts HTTP requests to the admin-user Service.
type Handler struct {
	svc *app.Service
}

// NewHandler builds an admin-user HTTP handler.
func NewHandler(svc *app.Service) *Handler { return &Handler{svc: svc} }

// Routes returns the chi router for the admin-user resource.
func (h *Handler) Routes() chi.Router {
	r := chi.NewRouter()
	r.Get("/", h.list)
	r.Post("/", h.create)
	r.Get("/{adminUserId}", h.get)
	r.Patch("/{adminUserId}", h.update)
	return r
}

// --- DTOs (aligned with the OpenAPI schemas) ---

type adminUserDTO struct {
	ID                 string `json:"id"`
	InstitutionalEmail string `json:"institutionalEmail"`
	FullName           string `json:"fullName"`
	Role               string `json:"role"`
	Status             string `json:"status"`
	CreatedAt          string `json:"createdAt"`
	UpdatedAt          string `json:"updatedAt"`
}

func toDTO(u domain.AdminUser) adminUserDTO {
	return adminUserDTO{
		ID:                 u.ID,
		InstitutionalEmail: u.InstitutionalEmail,
		FullName:           u.FullName,
		Role:               string(u.Role),
		Status:             string(u.Status),
		CreatedAt:          u.CreatedAt.UTC().Format(time.RFC3339),
		UpdatedAt:          u.UpdatedAt.UTC().Format(time.RFC3339),
	}
}

type pageDTO struct {
	Items    []adminUserDTO `json:"items"`
	Page     int            `json:"page"`
	PageSize int            `json:"pageSize"`
	Total    int            `json:"total"`
}

type createRequest struct {
	InstitutionalEmail string `json:"institutionalEmail"`
	FullName           string `json:"fullName"`
	Role               string `json:"role"`
}

type updateRequest struct {
	FullName *string `json:"fullName"`
	Role     *string `json:"role"`
	Status   *string `json:"status"`
}

func (u updateRequest) toDomain() domain.AdminUserPatch {
	var p domain.AdminUserPatch
	p.FullName = u.FullName
	if u.Role != nil {
		role := domain.Role(*u.Role)
		p.Role = &role
	}
	if u.Status != nil {
		st := domain.Status(*u.Status)
		p.Status = &st
	}
	return p
}

// --- handlers ---

func (h *Handler) list(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	f := app.ListFilter{
		Page:     atoiDefault(q.Get("page"), 0),
		PageSize: atoiDefault(q.Get("pageSize"), 0),
	}
	if s := q.Get("role"); s == string(domain.RoleAdmin) || s == string(domain.RoleSuperAdmin) {
		role := domain.Role(s)
		f.Role = &role
	}
	if s := q.Get("status"); s == string(domain.StatusActive) || s == string(domain.StatusInactive) {
		st := domain.Status(s)
		f.Status = &st
	}

	page, err := h.svc.List(r.Context(), f)
	if err != nil {
		httpx.WriteError(w, r, http.StatusInternalServerError, httpx.CodeInternal, "Could not load administrative users.", nil)
		return
	}
	items := make([]adminUserDTO, 0, len(page.Items))
	for _, u := range page.Items {
		items = append(items, toDTO(u))
	}
	httpx.WriteJSON(w, http.StatusOK, pageDTO{Items: items, Page: page.Page, PageSize: page.PageSize, Total: page.Total})
}

func (h *Handler) create(w http.ResponseWriter, r *http.Request) {
	var req createRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	u, err := h.svc.Create(r.Context(), domain.NewAdminUser{
		InstitutionalEmail: strings.TrimSpace(req.InstitutionalEmail),
		FullName:           strings.TrimSpace(req.FullName),
		Role:               domain.Role(req.Role),
	}, actorOf(r))
	if err != nil {
		writeDomainError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusCreated, toDTO(u))
}

func (h *Handler) get(w http.ResponseWriter, r *http.Request) {
	u, err := h.svc.Get(r.Context(), chi.URLParam(r, "adminUserId"))
	if err != nil {
		writeDomainError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, toDTO(*u))
}

func (h *Handler) update(w http.ResponseWriter, r *http.Request) {
	var req updateRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	u, err := h.svc.Update(r.Context(), chi.URLParam(r, "adminUserId"), req.toDomain(), actorOf(r))
	if err != nil {
		writeDomainError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, toDTO(*u))
}

// --- shared helpers ---

func actorOf(r *http.Request) app.Actor {
	p := authn.FromContext(r.Context())
	if p == nil {
		return app.Actor{}
	}
	return app.Actor{Type: string(p.SubjectType), ID: p.SubjectID}
}

func decodeJSON(w http.ResponseWriter, r *http.Request, dst any) bool {
	defer r.Body.Close()
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		httpx.WriteError(w, r, http.StatusBadRequest, httpx.CodeBadRequest, "Invalid JSON body.", nil)
		return false
	}
	return true
}

func writeDomainError(w http.ResponseWriter, r *http.Request, err error) {
	var ve domain.ValidationError
	switch {
	case errors.As(err, &ve):
		httpx.WriteError(w, r, http.StatusBadRequest, httpx.CodeValidation, ve.Message,
			map[string]any{"field": ve.Field})
	case errors.Is(err, app.ErrNotFound):
		httpx.WriteError(w, r, http.StatusNotFound, httpx.CodeNotFound, "Administrative user not found.", nil)
	case errors.Is(err, app.ErrEmailTaken):
		httpx.WriteError(w, r, http.StatusConflict, httpx.CodeConflict, "Institutional email already in use.", nil)
	default:
		httpx.WriteError(w, r, http.StatusInternalServerError, httpx.CodeInternal, "Unexpected error.", nil)
	}
}

func atoiDefault(s string, def int) int {
	if s == "" {
		return def
	}
	n, err := strconv.Atoi(s)
	if err != nil {
		return def
	}
	return n
}
