// Package http exposes the in-app notification inbox.
package http

import (
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/uniquindio/profundiza-uq/internal/notification/app"
	"github.com/uniquindio/profundiza-uq/internal/notification/domain"
	"github.com/uniquindio/profundiza-uq/internal/platform/authn"
	"github.com/uniquindio/profundiza-uq/internal/platform/httpx"
)

// Handler adapts HTTP to the notification Service.
type Handler struct{ svc *app.Service }

// NewHandler builds a notification HTTP handler.
func NewHandler(svc *app.Service) *Handler { return &Handler{svc: svc} }

// Routes returns the /notifications router.
func (h *Handler) Routes() chi.Router {
	r := chi.NewRouter()
	r.Get("/", h.list)
	r.Post("/{notificationId}/read", h.markRead)
	return r
}

func (h *Handler) list(w http.ResponseWriter, r *http.Request) {
	p := authn.FromContext(r.Context())
	page := atoiDefault(r.URL.Query().Get("page"), 1)
	pageSize := atoiDefault(r.URL.Query().Get("pageSize"), 20)

	items, total, err := h.svc.ListInApp(r.Context(), p.SubjectID, page, pageSize)
	if err != nil {
		httpx.WriteError(w, r, http.StatusInternalServerError, httpx.CodeInternal, "Could not load notifications.", nil)
		return
	}
	unread, _ := h.svc.UnreadCount(r.Context(), p.SubjectID)

	dtos := make([]notificationDTO, 0, len(items))
	for _, n := range items {
		dtos = append(dtos, toDTO(n))
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{
		"items": dtos, "page": page, "pageSize": pageSize, "total": total, "unread": unread,
	})
}

func (h *Handler) markRead(w http.ResponseWriter, r *http.Request) {
	p := authn.FromContext(r.Context())
	ok, err := h.svc.MarkRead(r.Context(), chi.URLParam(r, "notificationId"), p.SubjectID)
	if err != nil {
		httpx.WriteError(w, r, http.StatusInternalServerError, httpx.CodeInternal, "Could not update notification.", nil)
		return
	}
	if !ok {
		httpx.WriteError(w, r, http.StatusNotFound, httpx.CodeNotFound, "Notification not found.", nil)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

type notificationDTO struct {
	ID                string  `json:"id"`
	Type              string  `json:"type"`
	Channel           string  `json:"channel"`
	DeliveryStatus    string  `json:"deliveryStatus"`
	Subject           string  `json:"subject"`
	Body              string  `json:"body"`
	RecipientEmail    string  `json:"recipientEmail"`
	RelatedEntityType *string `json:"relatedEntityType"`
	RelatedEntityID   *string `json:"relatedEntityId"`
	ReadAt            *string `json:"readAt"`
	CreatedAt         string  `json:"createdAt"`
}

func toDTO(n domain.Notification) notificationDTO {
	d := notificationDTO{
		ID: n.ID, Type: n.Type, Channel: n.Channel, DeliveryStatus: n.DeliveryStatus,
		Subject: n.Subject, Body: n.Body, RecipientEmail: n.RecipientEmail,
		RelatedEntityType: n.RelatedEntityType, RelatedEntityID: n.RelatedEntityID,
		CreatedAt: n.CreatedAt.UTC().Format(time.RFC3339),
	}
	if n.ReadAt != nil {
		s := n.ReadAt.UTC().Format(time.RFC3339)
		d.ReadAt = &s
	}
	return d
}

func atoiDefault(s string, def int) int {
	if s == "" {
		return def
	}
	if n, err := strconv.Atoi(s); err == nil && n > 0 {
		return n
	}
	return def
}
