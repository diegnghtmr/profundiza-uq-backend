// Package http is the driving adapter exposing global-settings use cases over REST.
package http

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/uniquindio/profundiza-uq/internal/platform/authn"
	"github.com/uniquindio/profundiza-uq/internal/platform/httpx"
	"github.com/uniquindio/profundiza-uq/internal/settings/app"
	"github.com/uniquindio/profundiza-uq/internal/settings/domain"
)

// Handler adapts HTTP requests to the global-settings Service.
type Handler struct {
	svc *app.Service
}

// NewHandler builds a global-settings HTTP handler.
func NewHandler(svc *app.Service) *Handler { return &Handler{svc: svc} }

// Routes returns the chi router for the global-settings resource. Both PATCH
// and PUT are registered for the upsert so either verb is accepted.
func (h *Handler) Routes() chi.Router {
	r := chi.NewRouter()
	r.Get("/", h.list)
	r.Patch("/{settingKey}", h.upsert)
	r.Put("/{settingKey}", h.upsert)
	return r
}

// --- DTOs (aligned with the OpenAPI schemas) ---

type settingDTO struct {
	Key                  string          `json:"key"`
	Value                json.RawMessage `json:"value"`
	Description          string          `json:"description"`
	UpdatedByAdminUserID *string         `json:"updatedByAdminUserId"`
	UpdatedAt            string          `json:"updatedAt"`
}

func toDTO(s domain.GlobalSetting) settingDTO {
	value := s.Value
	if len(value) == 0 {
		value = json.RawMessage("null")
	}
	return settingDTO{
		Key:                  s.Key,
		Value:                value,
		Description:          s.Description,
		UpdatedByAdminUserID: s.UpdatedByAdminUserID,
		UpdatedAt:            s.UpdatedAt.UTC().Format(time.RFC3339),
	}
}

type pageDTO struct {
	Items    []settingDTO `json:"items"`
	Page     int          `json:"page"`
	PageSize int          `json:"pageSize"`
	Total    int          `json:"total"`
}

type updateRequest struct {
	Value  json.RawMessage `json:"value"`
	Reason string          `json:"reason"`
}

// --- handlers ---

func (h *Handler) list(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	f := app.ListFilter{
		Page:     atoiDefault(q.Get("page"), 0),
		PageSize: atoiDefault(q.Get("pageSize"), 0),
	}
	page, err := h.svc.List(r.Context(), f)
	if err != nil {
		httpx.WriteError(w, r, http.StatusInternalServerError, httpx.CodeInternal, "Could not load global settings.", nil)
		return
	}
	items := make([]settingDTO, 0, len(page.Items))
	for _, s := range page.Items {
		items = append(items, toDTO(s))
	}
	httpx.WriteJSON(w, http.StatusOK, pageDTO{Items: items, Page: page.Page, PageSize: page.PageSize, Total: page.Total})
}

func (h *Handler) upsert(w http.ResponseWriter, r *http.Request) {
	var req updateRequest
	defer r.Body.Close()
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&req); err != nil {
		httpx.WriteError(w, r, http.StatusBadRequest, httpx.CodeBadRequest, "Invalid JSON body.", nil)
		return
	}

	s, err := h.svc.Upsert(r.Context(), domain.UpsertSetting{
		Key:    chi.URLParam(r, "settingKey"),
		Value:  req.Value,
		Reason: req.Reason,
	}, actorOf(r))
	if err != nil {
		var ve domain.ValidationError
		if errors.As(err, &ve) {
			httpx.WriteError(w, r, http.StatusBadRequest, httpx.CodeValidation, ve.Message,
				map[string]any{"field": ve.Field})
			return
		}
		httpx.WriteError(w, r, http.StatusInternalServerError, httpx.CodeInternal, "Could not update setting.", nil)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, toDTO(s))
}

// --- helpers ---

func actorOf(r *http.Request) app.Actor {
	p := authn.FromContext(r.Context())
	if p == nil {
		return app.Actor{}
	}
	return app.Actor{Type: string(p.SubjectType), ID: p.SubjectID}
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
