// Package http is the driving adapter exposing the catalog read use cases.
package http

import (
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/uniquindio/profundiza-uq/internal/catalog/app"
	"github.com/uniquindio/profundiza-uq/internal/catalog/domain"
	"github.com/uniquindio/profundiza-uq/internal/platform/authn"
	"github.com/uniquindio/profundiza-uq/internal/platform/httpx"
	shared "github.com/uniquindio/profundiza-uq/internal/shared/domain"
)

// Handler adapts HTTP to the catalog read Service and the admin write service.
type Handler struct {
	svc   *app.Service
	admin *app.AdminService
}

// NewHandler builds a catalog HTTP handler.
func NewHandler(svc *app.Service, admin *app.AdminService) *Handler {
	return &Handler{svc: svc, admin: admin}
}

// Routes returns the offerings router (reads for any user; writes for admins).
func (h *Handler) Routes() chi.Router {
	r := chi.NewRouter()
	r.Get("/", h.listOfferings)
	r.Get("/{offeringId}", h.getOffering)
	r.Get("/{offeringId}/prerequisites", h.listPrerequisites)

	r.Group(func(admin chi.Router) {
		admin.Use(authn.RequireRole(authn.RoleAdmin, authn.RoleSuperAdmin))
		admin.Post("/", h.createOffering)
		admin.Post("/{offeringId}/prerequisites", h.createOfferingPrereq)
		admin.Post("/{offeringId}/groups", h.createGroup)
	})
	return r
}

func (h *Handler) listOfferings(w http.ResponseWriter, r *http.Request) {
	semesterID := r.URL.Query().Get("semesterId")
	if semesterID == "" {
		httpx.WriteError(w, r, http.StatusBadRequest, httpx.CodeValidation, "semesterId is required.", nil)
		return
	}
	f := app.OfferingFilter{SemesterID: semesterID, OnlyOpen: r.URL.Query().Get("onlyOpen") == "true"}
	if s := r.URL.Query().Get("shift"); s == string(shared.ShiftDay) || s == string(shared.ShiftNight) {
		shift := shared.AcademicShift(s)
		f.Shift = &shift
	}

	offerings, err := h.svc.ListOfferings(r.Context(), f)
	if err != nil {
		httpx.WriteError(w, r, http.StatusInternalServerError, httpx.CodeInternal, "Could not load offerings.", nil)
		return
	}
	items := make([]offeringSummaryDTO, 0, len(offerings))
	for _, o := range offerings {
		items = append(items, toSummaryDTO(o))
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (h *Handler) getOffering(w http.ResponseWriter, r *http.Request) {
	detail, err := h.svc.GetOfferingDetail(r.Context(), chi.URLParam(r, "offeringId"))
	if err != nil {
		httpx.WriteError(w, r, http.StatusInternalServerError, httpx.CodeInternal, "Could not load offering.", nil)
		return
	}
	if detail == nil {
		httpx.WriteError(w, r, http.StatusNotFound, httpx.CodeNotFound, "Offering not found.", nil)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, toDetailDTO(*detail))
}

func (h *Handler) listPrerequisites(w http.ResponseWriter, r *http.Request) {
	prereqs, err := h.svc.ListEffectivePrerequisites(r.Context(), chi.URLParam(r, "offeringId"))
	if err != nil {
		httpx.WriteError(w, r, http.StatusInternalServerError, httpx.CodeInternal, "Could not load prerequisites.", nil)
		return
	}
	items := make([]prerequisiteDTO, 0, len(prereqs))
	for _, p := range prereqs {
		items = append(items, toPrereqDTO(p))
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"items": items})
}

// --- DTOs (aligned with the OpenAPI schemas) ---

type electiveDTO struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Area        string `json:"area"`
	Description string `json:"description"`
	Status      string `json:"status"`
	CreatedAt   string `json:"createdAt"`
	UpdatedAt   string `json:"updatedAt"`
}

type groupSummaryDTO struct {
	ID            string `json:"id"`
	GroupCode     string `json:"groupCode"`
	Shift         string `json:"shift"`
	ScheduleText  string `json:"scheduleText"`
	Capacity      int    `json:"capacity"`
	AcceptedCount int    `json:"acceptedCount"`
	Status        string `json:"status"`
}

type groupDTO struct {
	ID                         string  `json:"id"`
	OfferingID                 string  `json:"offeringId"`
	GroupCode                  string  `json:"groupCode"`
	Shift                      string  `json:"shift"`
	TeacherName                *string `json:"teacherName"`
	ScheduleText               string  `json:"scheduleText"`
	Capacity                   int     `json:"capacity"`
	AcceptedCount              int     `json:"acceptedCount"`
	PendingDirectCount         int     `json:"pendingDirectCount"`
	WaitlistSameShiftCount     int     `json:"waitlistSameShiftCount"`
	WaitlistOppositeShiftCount int     `json:"waitlistOppositeShiftCount"`
	Status                     string  `json:"status"`
	CreatedAt                  string  `json:"createdAt"`
	UpdatedAt                  string  `json:"updatedAt"`
}

type prerequisiteDTO struct {
	ID          string  `json:"id"`
	OfferingID  string  `json:"offeringId"`
	Name        string  `json:"name"`
	Description string  `json:"description"`
	PlanType    *string `json:"planType"`
	Source      string  `json:"source"`
	Status      string  `json:"status"`
}

type offeringSummaryDTO struct {
	ID         string            `json:"id"`
	SemesterID string            `json:"semesterId"`
	Elective   electiveDTO       `json:"elective"`
	Groups     []groupSummaryDTO `json:"groups"`
}

type offeringDetailDTO struct {
	ID            string            `json:"id"`
	SemesterID    string            `json:"semesterId"`
	Elective      electiveDTO       `json:"elective"`
	Prerequisites []prerequisiteDTO `json:"prerequisites"`
	Groups        []groupDTO        `json:"groups"`
}

func toElectiveDTO(e domain.Elective) electiveDTO {
	return electiveDTO{
		ID: e.ID, Name: e.Name, Area: e.Area, Description: e.Description, Status: e.Status,
		CreatedAt: e.CreatedAt.UTC().Format(time.RFC3339), UpdatedAt: e.UpdatedAt.UTC().Format(time.RFC3339),
	}
}

func toSummaryDTO(o domain.OfferingSummary) offeringSummaryDTO {
	groups := make([]groupSummaryDTO, 0, len(o.Groups))
	for _, g := range o.Groups {
		groups = append(groups, groupSummaryDTO{
			ID: g.ID, GroupCode: g.GroupCode, Shift: string(g.Shift), ScheduleText: g.ScheduleText,
			Capacity: g.Capacity, AcceptedCount: g.AcceptedCount, Status: g.Status,
		})
	}
	return offeringSummaryDTO{ID: o.ID, SemesterID: o.SemesterID, Elective: toElectiveDTO(o.Elective), Groups: groups}
}

func toDetailDTO(d domain.OfferingDetail) offeringDetailDTO {
	groups := make([]groupDTO, 0, len(d.Groups))
	for _, g := range d.Groups {
		groups = append(groups, toGroupDTO(g))
	}
	prereqs := make([]prerequisiteDTO, 0, len(d.Prerequisites))
	for _, p := range d.Prerequisites {
		prereqs = append(prereqs, toPrereqDTO(p))
	}
	return offeringDetailDTO{ID: d.ID, SemesterID: d.SemesterID, Elective: toElectiveDTO(d.Elective), Prerequisites: prereqs, Groups: groups}
}

func toGroupDTO(g domain.Group) groupDTO {
	return groupDTO{
		ID: g.ID, OfferingID: g.OfferingID, GroupCode: g.GroupCode, Shift: string(g.Shift),
		TeacherName: g.TeacherName, ScheduleText: g.ScheduleText, Capacity: g.Capacity,
		AcceptedCount: g.AcceptedCount, PendingDirectCount: g.PendingDirectCount,
		WaitlistSameShiftCount: g.WaitlistSameShiftCount, WaitlistOppositeShiftCount: g.WaitlistOppositeShiftCount,
		Status: g.Status, CreatedAt: g.CreatedAt.UTC().Format(time.RFC3339), UpdatedAt: g.UpdatedAt.UTC().Format(time.RFC3339),
	}
}

func toPrereqDTO(p domain.Prerequisite) prerequisiteDTO {
	return prerequisiteDTO{
		ID: p.ID, OfferingID: p.OfferingID, Name: p.Name, Description: p.Description,
		PlanType: p.PlanType, Source: p.Source, Status: p.Status,
	}
}
