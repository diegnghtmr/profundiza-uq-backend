// Package http is the driving adapter exposing the audit read use case over REST.
package http

import (
	"encoding/json"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/uniquindio/profundiza-uq/internal/audit/app"
	"github.com/uniquindio/profundiza-uq/internal/audit/domain"
	"github.com/uniquindio/profundiza-uq/internal/platform/httpx"
)

// Handler adapts HTTP requests to the audit Service.
type Handler struct {
	svc *app.Service
}

// NewHandler builds an audit HTTP handler.
func NewHandler(svc *app.Service) *Handler { return &Handler{svc: svc} }

// Routes returns the chi router for the audit-events resource.
func (h *Handler) Routes() chi.Router {
	r := chi.NewRouter()
	r.Get("/", h.list)
	return r
}

// --- DTOs (aligned with the OpenAPI schemas) ---

type auditEventDTO struct {
	ID            string          `json:"id"`
	ActorType     string          `json:"actorType"`
	ActorID       *string         `json:"actorId"`
	Action        string          `json:"action"`
	EntityType    string          `json:"entityType"`
	EntityID      string          `json:"entityId"`
	PreviousValue json.RawMessage `json:"previousValue,omitempty"`
	NewValue      json.RawMessage `json:"newValue,omitempty"`
	Reason        *string         `json:"reason"`
	CreatedAt     string          `json:"createdAt"`
}

func toDTO(e domain.AuditEvent) auditEventDTO {
	entityID := ""
	if e.EntityID != nil {
		entityID = *e.EntityID
	}
	return auditEventDTO{
		ID:            strconv.FormatInt(e.ID, 10),
		ActorType:     e.ActorType,
		ActorID:       e.ActorID,
		Action:        e.Action,
		EntityType:    e.EntityType,
		EntityID:      entityID,
		PreviousValue: e.PreviousValue,
		NewValue:      e.NewValue,
		Reason:        e.Reason,
		CreatedAt:     e.CreatedAt.UTC().Format(time.RFC3339),
	}
}

type pageDTO struct {
	Items    []auditEventDTO `json:"items"`
	Page     int             `json:"page"`
	PageSize int             `json:"pageSize"`
	Total    int             `json:"total"`
}

// --- handlers ---

func (h *Handler) list(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	f := app.ListFilter{
		Page:     atoiDefault(q.Get("page"), 0),
		PageSize: atoiDefault(q.Get("pageSize"), 0),
	}
	if s := q.Get("entityType"); s != "" {
		f.EntityType = &s
	}
	if s := q.Get("entityId"); s != "" {
		f.EntityID = &s
	}
	if s := q.Get("actorId"); s != "" {
		f.ActorID = &s
	}

	page, err := h.svc.List(r.Context(), f)
	if err != nil {
		httpx.WriteError(w, r, http.StatusInternalServerError, httpx.CodeInternal, "Could not load audit events.", nil)
		return
	}
	items := make([]auditEventDTO, 0, len(page.Items))
	for _, e := range page.Items {
		items = append(items, toDTO(e))
	}
	httpx.WriteJSON(w, http.StatusOK, pageDTO{Items: items, Page: page.Page, PageSize: page.PageSize, Total: page.Total})
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
