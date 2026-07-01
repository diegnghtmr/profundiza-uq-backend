package http

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/uniquindio/profundiza-uq/internal/catalog/app"
	"github.com/uniquindio/profundiza-uq/internal/platform/authn"
	"github.com/uniquindio/profundiza-uq/internal/platform/httpx"
	shared "github.com/uniquindio/profundiza-uq/internal/shared/domain"
)

// ElectiveRoutes returns the /electives router. Mount it behind an admin guard.
func (h *Handler) ElectiveRoutes() chi.Router {
	r := chi.NewRouter()
	r.Get("/", h.listElectives)
	r.Post("/", h.createElective)
	r.Get("/{electiveId}", h.getElective)
	r.Patch("/{electiveId}", h.updateElective)
	r.Get("/{electiveId}/prerequisites", h.listElectivePrereqs)
	r.Post("/{electiveId}/prerequisites", h.createElectivePrereq)
	return r
}

// GroupRoutes returns the /offering-groups router. Mount it behind an admin guard.
func (h *Handler) GroupRoutes() chi.Router {
	r := chi.NewRouter()
	r.Patch("/{groupId}", h.updateGroup)
	r.Post("/{groupId}/capacity-adjustments", h.adjustCapacity)
	return r
}

func actorID(r *http.Request) string {
	if p := authn.FromContext(r.Context()); p != nil {
		return p.SubjectID
	}
	return ""
}

func writeAdminErr(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, app.ErrAdminNotFound):
		httpx.WriteError(w, r, http.StatusNotFound, httpx.CodeNotFound, "Not found.", nil)
	case errors.Is(err, app.ErrAdminInvalid):
		httpx.WriteError(w, r, http.StatusBadRequest, httpx.CodeValidation, "Invalid input.", nil)
	default:
		httpx.WriteError(w, r, http.StatusInternalServerError, httpx.CodeInternal, "Could not complete the operation.", nil)
	}
}

// --- electives ---

func (h *Handler) listElectives(w http.ResponseWriter, r *http.Request) {
	electives, err := h.admin.ListElectives(r.Context(), r.URL.Query().Get("q"), r.URL.Query().Get("area"))
	if err != nil {
		writeAdminErr(w, r, err)
		return
	}
	items := make([]electiveDTO, 0, len(electives))
	for _, e := range electives {
		items = append(items, toElectiveDTO(e))
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"items": items})
}

type createElectiveReq struct {
	Name        string `json:"name"`
	Area        string `json:"area"`
	Description string `json:"description"`
}

func (h *Handler) createElective(w http.ResponseWriter, r *http.Request) {
	var req createElectiveReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeAdminErr(w, r, app.ErrAdminInvalid)
		return
	}
	e, err := h.admin.CreateElective(r.Context(), app.CreateElectiveInput{
		Name: req.Name, Area: req.Area, Description: req.Description, ActorID: actorID(r)})
	if err != nil {
		writeAdminErr(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusCreated, toElectiveDTO(e))
}

func (h *Handler) getElective(w http.ResponseWriter, r *http.Request) {
	e, err := h.admin.GetElective(r.Context(), chi.URLParam(r, "electiveId"))
	if err != nil {
		writeAdminErr(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, toElectiveDTO(*e))
}

type updateElectiveReq struct {
	Name        *string `json:"name"`
	Area        *string `json:"area"`
	Description *string `json:"description"`
	Status      *string `json:"status"`
}

func (h *Handler) updateElective(w http.ResponseWriter, r *http.Request) {
	var req updateElectiveReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeAdminErr(w, r, app.ErrAdminInvalid)
		return
	}
	e, err := h.admin.UpdateElective(r.Context(), app.UpdateElectiveInput{
		ID: chi.URLParam(r, "electiveId"), Name: req.Name, Area: req.Area,
		Description: req.Description, Status: req.Status, ActorID: actorID(r)})
	if err != nil {
		writeAdminErr(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, toElectiveDTO(e))
}

func (h *Handler) listElectivePrereqs(w http.ResponseWriter, r *http.Request) {
	prereqs, err := h.admin.ListElectivePrerequisites(r.Context(), chi.URLParam(r, "electiveId"))
	if err != nil {
		writeAdminErr(w, r, err)
		return
	}
	items := make([]prerequisiteDTO, 0, len(prereqs))
	for _, p := range prereqs {
		items = append(items, toPrereqDTO(p))
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"items": items})
}

type createPrereqReq struct {
	Name        string  `json:"name"`
	Description string  `json:"description"`
	PlanType    *string `json:"planType"`
}

func (h *Handler) createElectivePrereq(w http.ResponseWriter, r *http.Request) {
	var req createPrereqReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeAdminErr(w, r, app.ErrAdminInvalid)
		return
	}
	p, err := h.admin.CreatePrerequisite(r.Context(), app.CreatePrerequisiteInput{
		ElectiveID: chi.URLParam(r, "electiveId"), Name: req.Name, Description: req.Description,
		PlanType: req.PlanType, ActorID: actorID(r)})
	if err != nil {
		writeAdminErr(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusCreated, toPrereqDTO(p))
}

// --- offerings (writes; reads live in handler.go) ---

type createOfferingReq struct {
	SemesterID string `json:"semesterId"`
	ElectiveID string `json:"electiveId"`
}

func (h *Handler) createOffering(w http.ResponseWriter, r *http.Request) {
	var req createOfferingReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeAdminErr(w, r, app.ErrAdminInvalid)
		return
	}
	id, err := h.admin.CreateOffering(r.Context(), app.CreateOfferingInput{
		SemesterID: req.SemesterID, ElectiveID: req.ElectiveID, ActorID: actorID(r)})
	if err != nil {
		writeAdminErr(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusCreated, map[string]any{
		"id": id, "semesterId": req.SemesterID, "electiveId": req.ElectiveID, "status": "ACTIVE"})
}

type createOfferingPrereqReq struct {
	Name        string  `json:"name"`
	Description string  `json:"description"`
	PlanType    *string `json:"planType"`
	Source      string  `json:"source"`
}

func (h *Handler) createOfferingPrereq(w http.ResponseWriter, r *http.Request) {
	var req createOfferingPrereqReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeAdminErr(w, r, app.ErrAdminInvalid)
		return
	}
	p, err := h.admin.CreateOfferingPrereq(r.Context(), app.CreateOfferingPrereqInput{
		OfferingID: chi.URLParam(r, "offeringId"), Name: req.Name, Description: req.Description,
		PlanType: req.PlanType, Source: req.Source, ActorID: actorID(r)})
	if err != nil {
		writeAdminErr(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusCreated, toPrereqDTO(p))
}

type createGroupReq struct {
	GroupCode    string  `json:"groupCode"`
	Shift        string  `json:"shift"`
	TeacherName  *string `json:"teacherName"`
	ScheduleText string  `json:"scheduleText"`
	Capacity     int     `json:"capacity"`
	Reason       string  `json:"reason"`
}

func (h *Handler) createGroup(w http.ResponseWriter, r *http.Request) {
	var req createGroupReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeAdminErr(w, r, app.ErrAdminInvalid)
		return
	}
	g, err := h.admin.CreateGroup(r.Context(), app.CreateGroupInput{
		OfferingID: chi.URLParam(r, "offeringId"), GroupCode: req.GroupCode, Shift: shared.AcademicShift(req.Shift),
		TeacherName: req.TeacherName, ScheduleText: req.ScheduleText, Capacity: req.Capacity,
		Reason: req.Reason, ActorID: actorID(r)})
	if err != nil {
		writeAdminErr(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusCreated, toGroupDTO(g))
}

// --- offering groups ---

type updateGroupReq struct {
	GroupCode    *string `json:"groupCode"`
	TeacherName  *string `json:"teacherName"`
	ScheduleText *string `json:"scheduleText"`
	Status       *string `json:"status"`
	Reason       string  `json:"reason"`
}

func (h *Handler) updateGroup(w http.ResponseWriter, r *http.Request) {
	var req updateGroupReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeAdminErr(w, r, app.ErrAdminInvalid)
		return
	}
	g, err := h.admin.UpdateGroup(r.Context(), app.UpdateGroupInput{
		ID: chi.URLParam(r, "groupId"), GroupCode: req.GroupCode, TeacherName: req.TeacherName,
		ScheduleText: req.ScheduleText, Status: req.Status, Reason: req.Reason, ActorID: actorID(r)})
	if err != nil {
		writeAdminErr(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, toGroupDTO(g))
}

type adjustCapacityReq struct {
	NewCapacity int    `json:"newCapacity"`
	Reason      string `json:"reason"`
}

func (h *Handler) adjustCapacity(w http.ResponseWriter, r *http.Request) {
	var req adjustCapacityReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeAdminErr(w, r, app.ErrAdminInvalid)
		return
	}
	g, err := h.admin.AdjustCapacity(r.Context(), app.AdjustCapacityInput{
		GroupID: chi.URLParam(r, "groupId"), NewCapacity: req.NewCapacity, Reason: req.Reason, ActorID: actorID(r)})
	if err != nil {
		writeAdminErr(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, toGroupDTO(g))
}
