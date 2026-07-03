// Package http is the driving adapter for administrative review: the queue and
// the decision command. Admin/superadmin only.
package http

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/uniquindio/profundiza-uq/internal/enrollment/domain"
	"github.com/uniquindio/profundiza-uq/internal/platform/authn"
	"github.com/uniquindio/profundiza-uq/internal/platform/httpx"
	reviewpg "github.com/uniquindio/profundiza-uq/internal/review/adapter/postgres"
	"github.com/uniquindio/profundiza-uq/internal/review/app"
)

// Handler adapts HTTP to the review Service.
type Handler struct{ svc *app.Service }

// NewHandler builds a review HTTP handler.
func NewHandler(svc *app.Service) *Handler { return &Handler{svc: svc} }

// Queue handles GET /admin/review-queues.
func (h *Handler) Queue(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	semesterID := q.Get("semesterId")
	if semesterID == "" {
		httpx.WriteError(w, r, http.StatusBadRequest, httpx.CodeValidation, "semesterId is required.", nil)
		return
	}
	f := app.QueueFilter{
		SemesterID:    semesterID,
		OfferingID:    q.Get("offeringId"),
		GroupID:       q.Get("groupId"),
		Status:        q.Get("status"),
		PriorityGroup: q.Get("priorityGroup"),
		Page:          atoiDefault(q.Get("page"), 1),
		PageSize:      atoiDefault(q.Get("pageSize"), 20),
	}
	items, total, err := h.svc.Queue(r.Context(), f)
	if err != nil {
		httpx.WriteError(w, r, http.StatusInternalServerError, httpx.CodeInternal, "Could not load the review queue.", nil)
		return
	}
	dtos := make([]queueItemDTO, 0, len(items))
	for _, it := range items {
		dtos = append(dtos, toQueueItemDTO(it))
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{
		"items": dtos, "page": f.Page, "pageSize": f.PageSize, "total": total,
	})
}

type decisionReq struct {
	DecisionType  string `json:"decisionType"`
	Reason        string `json:"reason"`
	TargetGroupID string `json:"targetGroupId"`
}

// Decide handles POST /admin/enrollment-requests/{requestId}/decisions.
func (h *Handler) Decide(w http.ResponseWriter, r *http.Request) {
	p := authn.FromContext(r.Context())
	adminID, ok := p.AdminUserID()
	if !ok {
		httpx.WriteError(w, r, http.StatusForbidden, httpx.CodeForbidden, "Only administrators can decide requests.", nil)
		return
	}
	var req decisionReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.DecisionType == "" {
		httpx.WriteError(w, r, http.StatusBadRequest, httpx.CodeValidation, "decisionType and reason are required.", nil)
		return
	}
	res, err := h.svc.Decide(r.Context(), app.DecisionInput{
		RequestID:     chi.URLParam(r, "requestId"),
		AdminUserID:   adminID,
		DecisionType:  domain.DecisionType(req.DecisionType),
		Reason:        req.Reason,
		TargetGroupID: req.TargetGroupID,
	})
	if err != nil {
		writeDecisionError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{
		"request":  toRequestDTO(res.Request),
		"decision": toDecisionDTO(res.Decision),
	})
}

func writeDecisionError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, domain.ErrReasonRequired):
		httpx.WriteError(w, r, http.StatusBadRequest, httpx.CodeReasonRequired, "A reason of at least 3 characters is required.", nil)
	case errors.Is(err, domain.ErrTargetGroupRequired):
		httpx.WriteError(w, r, http.StatusBadRequest, httpx.CodeValidation, "A target group is required for a create-group acceptance.", nil)
	case errors.Is(err, domain.ErrCapacityExceeded):
		httpx.WriteError(w, r, http.StatusConflict, httpx.CodeCapacityExceeded, "Accepting this request would exceed the group capacity. Adjust capacity or create a group.", nil)
	case errors.Is(err, domain.ErrMaxElectivesReached):
		httpx.WriteError(w, r, http.StatusConflict, httpx.CodeMaxElectives, "The student already holds the maximum of four accepted professional electives.", nil)
	case errors.Is(err, domain.ErrAlreadyTerminal):
		httpx.WriteError(w, r, http.StatusConflict, httpx.CodeConflict, "This request is already in a final state.", nil)
	case errors.Is(err, domain.ErrUnknownDecision):
		httpx.WriteError(w, r, http.StatusBadRequest, httpx.CodeValidation, "Unknown decision type.", nil)
	case errors.Is(err, reviewpg.ErrRequestNotFound):
		httpx.WriteError(w, r, http.StatusNotFound, httpx.CodeNotFound, "Enrollment request not found.", nil)
	default:
		httpx.WriteError(w, r, http.StatusInternalServerError, httpx.CodeInternal, "Could not record the decision.", nil)
	}
}

// --- DTOs ---

type queueItemDTO struct {
	Request  requestDTO   `json:"request"`
	Student  studentDTO   `json:"student"`
	Offering offeringDTO  `json:"offering"`
	Group    groupDTO     `json:"group"`
	Warnings []string     `json:"warnings"`
}

type requestDTO struct {
	ID              string  `json:"id"`
	SemesterID      string  `json:"semesterId"`
	StudentID       string  `json:"studentId"`
	OfferingID      string  `json:"offeringId"`
	OfferingGroupID string  `json:"offeringGroupId"`
	StudentShift    string  `json:"studentShift"`
	OfferingShift   string  `json:"offeringShift"`
	PriorityGroup   string  `json:"priorityGroup"`
	Status          string  `json:"status"`
	ArrivalSequence int64   `json:"arrivalSequence"`
	SubmittedAt     string  `json:"submittedAt"`
	CancelledAt     *string `json:"cancelledAt"`
	LatestReason    *string `json:"latestReason"`
}

type studentDTO struct {
	ID                                  string `json:"id"`
	InstitutionalEmail                  string `json:"institutionalEmail"`
	DocumentNumber                      string `json:"documentNumber"`
	FullName                            string `json:"fullName"`
	AcademicShift                       string `json:"academicShift"`
	Status                              string `json:"status"`
	CompletedProfessionalElectivesCount int    `json:"completedProfessionalElectivesCount"`
	CreatedAt                           string `json:"createdAt"`
	UpdatedAt                           string `json:"updatedAt"`
}

type electiveDTO struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Area        string `json:"area"`
	Description string `json:"description"`
	Status      string `json:"status"`
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

type offeringDTO struct {
	ID         string            `json:"id"`
	SemesterID string            `json:"semesterId"`
	Elective   electiveDTO       `json:"elective"`
	Groups     []groupSummaryDTO `json:"groups"`
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

type decisionDTO struct {
	ID                  string `json:"id"`
	EnrollmentRequestID string `json:"enrollmentRequestId"`
	AdminUserID         string `json:"adminUserId"`
	DecisionType        string `json:"decisionType"`
	PreviousStatus      string `json:"previousStatus"`
	NewStatus           string `json:"newStatus"`
	Reason              string `json:"reason"`
	CreatedAt           string `json:"createdAt"`
}

func toQueueItemDTO(it app.QueueItem) queueItemDTO {
	return queueItemDTO{
		Request: toRequestDTO(it.Request),
		Student: studentDTO{
			ID: it.Student.ID, InstitutionalEmail: it.Student.Email, DocumentNumber: it.Student.DocumentNumber,
			FullName: it.Student.FullName, AcademicShift: it.Student.Shift, Status: it.Student.Status,
			CompletedProfessionalElectivesCount: it.Student.CompletedCount,
			CreatedAt: rfc(it.Student.CreatedAt), UpdatedAt: rfc(it.Student.UpdatedAt),
		},
		Offering: offeringDTO{
			ID: it.Request.OfferingID, SemesterID: it.Request.SemesterID,
			Elective: electiveDTO{ID: it.Elective.ID, Name: it.Elective.Name, Area: it.Elective.Area, Description: it.Elective.Description, Status: it.Elective.Status},
			Groups: []groupSummaryDTO{{ID: it.Group.ID, GroupCode: it.Group.GroupCode, Shift: it.Group.Shift, ScheduleText: it.Group.ScheduleText, Capacity: it.Group.Capacity, AcceptedCount: it.Group.AcceptedCount, Status: it.Group.Status}},
		},
		Group:    toGroupDTO(it.Group),
		Warnings: it.Warnings,
	}
}

func toGroupDTO(g app.Group) groupDTO {
	return groupDTO{
		ID: g.ID, OfferingID: g.OfferingID, GroupCode: g.GroupCode, Shift: g.Shift, TeacherName: g.TeacherName,
		ScheduleText: g.ScheduleText, Capacity: g.Capacity, AcceptedCount: g.AcceptedCount,
		PendingDirectCount: g.PendingDirectCount, WaitlistSameShiftCount: g.WaitlistSameShiftCount,
		WaitlistOppositeShiftCount: g.WaitlistOppositeShiftCount, Status: g.Status,
		CreatedAt: rfc(g.CreatedAt), UpdatedAt: rfc(g.UpdatedAt),
	}
}

func toRequestDTO(r app.RequestRow) requestDTO {
	d := requestDTO{
		ID: r.ID, SemesterID: r.SemesterID, StudentID: r.StudentID, OfferingID: r.OfferingID,
		OfferingGroupID: r.OfferingGroupID, StudentShift: r.StudentShift, OfferingShift: r.OfferingShift,
		PriorityGroup: r.PriorityGroup, Status: r.Status, ArrivalSequence: r.ArrivalSequence,
		SubmittedAt: rfc(r.SubmittedAt), LatestReason: r.LatestReason,
	}
	if r.CancelledAt != nil {
		s := rfc(*r.CancelledAt)
		d.CancelledAt = &s
	}
	return d
}

func toDecisionDTO(d app.Decision) decisionDTO {
	return decisionDTO{
		ID: d.ID, EnrollmentRequestID: d.EnrollmentRequestID, AdminUserID: d.AdminUserID,
		DecisionType: d.DecisionType, PreviousStatus: d.PreviousStatus, NewStatus: d.NewStatus,
		Reason: d.Reason, CreatedAt: rfc(d.CreatedAt),
	}
}

func rfc(t time.Time) string { return t.UTC().Format(time.RFC3339) }

func atoiDefault(s string, def int) int {
	if n, err := strconv.Atoi(s); err == nil && n > 0 {
		return n
	}
	return def
}
