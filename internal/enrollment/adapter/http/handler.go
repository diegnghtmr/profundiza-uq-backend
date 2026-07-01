// Package http is the driving adapter for student enrollment: submit, batch,
// cancel, list and read of the student's own requests.
package http

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"

	enrollpg "github.com/uniquindio/profundiza-uq/internal/enrollment/adapter/postgres"
	"github.com/uniquindio/profundiza-uq/internal/enrollment/app"
	"github.com/uniquindio/profundiza-uq/internal/enrollment/domain"
	"github.com/uniquindio/profundiza-uq/internal/platform/authn"
	"github.com/uniquindio/profundiza-uq/internal/platform/httpx"
)

// Handler adapts HTTP to the EnrollmentService.
type Handler struct{ svc *app.EnrollmentService }

// NewHandler builds an enrollment HTTP handler.
func NewHandler(svc *app.EnrollmentService) *Handler { return &Handler{svc: svc} }

// Routes returns the /enrollment-requests router (student-facing).
func (h *Handler) Routes() chi.Router {
	r := chi.NewRouter()
	r.Get("/", h.listMine)
	r.Post("/", h.submit)
	r.Post("/batch", h.submitBatch)
	r.Get("/{requestId}", h.get)
	r.Post("/{requestId}/cancel", h.cancel)
	return r
}

func studentID(r *http.Request) (string, bool) {
	p := authn.FromContext(r.Context())
	if p == nil {
		return "", false
	}
	return p.StudentID()
}

func (h *Handler) listMine(w http.ResponseWriter, r *http.Request) {
	sid, ok := studentID(r)
	if !ok {
		httpx.WriteError(w, r, http.StatusForbidden, httpx.CodeForbidden, "Only students can list their requests.", nil)
		return
	}
	semesterID := r.URL.Query().Get("semesterId")
	if semesterID == "" {
		httpx.WriteError(w, r, http.StatusBadRequest, httpx.CodeValidation, "semesterId is required.", nil)
		return
	}
	views, err := h.svc.ListMine(r.Context(), sid, semesterID)
	if err != nil {
		httpx.WriteError(w, r, http.StatusInternalServerError, httpx.CodeInternal, "Could not load requests.", nil)
		return
	}
	items := make([]requestDTO, 0, len(views))
	for _, v := range views {
		items = append(items, toDTO(v))
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"items": items})
}

type submitReq struct {
	SemesterID      string `json:"semesterId"`
	OfferingGroupID string `json:"offeringGroupId"`
}

func (h *Handler) submit(w http.ResponseWriter, r *http.Request) {
	sid, ok := studentID(r)
	if !ok {
		httpx.WriteError(w, r, http.StatusForbidden, httpx.CodeForbidden, "Only students can submit requests.", nil)
		return
	}
	idem := r.Header.Get("Idempotency-Key")
	if len(idem) < 16 {
		httpx.WriteError(w, r, http.StatusBadRequest, httpx.CodeValidation, "A valid Idempotency-Key header is required.", nil)
		return
	}
	var req submitReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.SemesterID == "" || req.OfferingGroupID == "" {
		httpx.WriteError(w, r, http.StatusBadRequest, httpx.CodeValidation, "semesterId and offeringGroupId are required.", nil)
		return
	}
	res, err := h.svc.Submit(r.Context(), app.SubmitInput{
		SemesterID: req.SemesterID, StudentID: sid, OfferingGroupID: req.OfferingGroupID, IdempotencyKey: idem,
	})
	if err != nil {
		writeEnrollmentError(w, r, err)
		return
	}
	view, err := h.svc.Get(r.Context(), res.ID, sid, false)
	if err != nil {
		httpx.WriteError(w, r, http.StatusInternalServerError, httpx.CodeInternal, "Request stored but could not be read back.", nil)
		return
	}
	status := http.StatusCreated
	if res.Existed {
		status = http.StatusOK
	}
	httpx.WriteJSON(w, status, toDTO(*view))
}

type batchReq struct {
	SemesterID string `json:"semesterId"`
	Items      []struct {
		OfferingGroupID string `json:"offeringGroupId"`
	} `json:"items"`
}

func (h *Handler) submitBatch(w http.ResponseWriter, r *http.Request) {
	sid, ok := studentID(r)
	if !ok {
		httpx.WriteError(w, r, http.StatusForbidden, httpx.CodeForbidden, "Only students can submit requests.", nil)
		return
	}
	idem := r.Header.Get("Idempotency-Key")
	if len(idem) < 16 {
		httpx.WriteError(w, r, http.StatusBadRequest, httpx.CodeValidation, "A valid Idempotency-Key header is required.", nil)
		return
	}
	var req batchReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.SemesterID == "" || len(req.Items) == 0 || len(req.Items) > domain.MaxElectivesPerSemester {
		httpx.WriteError(w, r, http.StatusBadRequest, httpx.CodeValidation, "semesterId and 1..4 items are required.", nil)
		return
	}
	gids := make([]string, len(req.Items))
	for i, it := range req.Items {
		gids[i] = it.OfferingGroupID
	}
	results, err := h.svc.SubmitBatch(r.Context(), req.SemesterID, sid, idem, gids)
	if err != nil {
		writeEnrollmentError(w, r, err)
		return
	}
	items := make([]requestDTO, 0, len(results))
	for _, res := range results {
		if v, err := h.svc.Get(r.Context(), res.ID, sid, false); err == nil {
			items = append(items, toDTO(*v))
		}
	}
	httpx.WriteJSON(w, http.StatusCreated, map[string]any{"items": items})
}

func (h *Handler) get(w http.ResponseWriter, r *http.Request) {
	p := authn.FromContext(r.Context())
	sid, _ := p.StudentID()
	isAdmin := p.Role == authn.RoleAdmin || p.Role == authn.RoleSuperAdmin
	view, err := h.svc.Get(r.Context(), chi.URLParam(r, "requestId"), sid, isAdmin)
	if err != nil {
		writeEnrollmentError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, toDTO(*view))
}

func (h *Handler) cancel(w http.ResponseWriter, r *http.Request) {
	sid, ok := studentID(r)
	if !ok {
		httpx.WriteError(w, r, http.StatusForbidden, httpx.CodeForbidden, "Only students can cancel their requests.", nil)
		return
	}
	view, err := h.svc.Cancel(r.Context(), app.CancelInput{RequestID: chi.URLParam(r, "requestId"), StudentID: sid})
	if err != nil {
		writeEnrollmentError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, toDTO(view))
}

// writeEnrollmentError maps domain/app/adapter errors to the API envelope.
func writeEnrollmentError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, domain.ErrMaxElectivesReached):
		httpx.WriteError(w, r, http.StatusConflict, httpx.CodeMaxElectives, "You have reached the maximum of four professional electives for the semester.", nil)
	case errors.Is(err, enrollpg.ErrWindowClosed):
		httpx.WriteError(w, r, http.StatusConflict, httpx.CodeWindowClosed, "Enrollment is not currently open. There is no active enrollment window.", nil)
	case errors.Is(err, enrollpg.ErrGroupNotFound):
		httpx.WriteError(w, r, http.StatusNotFound, httpx.CodeNotFound, "Offering group not found or closed.", nil)
	case errors.Is(err, enrollpg.ErrStudentNotFound):
		httpx.WriteError(w, r, http.StatusNotFound, httpx.CodeNotFound, "Student not found.", nil)
	case errors.Is(err, app.ErrNotFound):
		httpx.WriteError(w, r, http.StatusNotFound, httpx.CodeNotFound, "Request not found.", nil)
	case errors.Is(err, app.ErrForbidden):
		httpx.WriteError(w, r, http.StatusForbidden, httpx.CodeForbidden, "This request does not belong to you.", nil)
	case errors.Is(err, app.ErrNotCancelable):
		httpx.WriteError(w, r, http.StatusConflict, httpx.CodeConflict, "This request can no longer be cancelled.", nil)
	case errors.Is(err, app.ErrDuplicateActiveRequest):
		httpx.WriteError(w, r, http.StatusConflict, httpx.CodeConflict, "You already have an active request for this elective group.", nil)
	case errors.Is(err, app.ErrDuplicateBatchItem):
		httpx.WriteError(w, r, http.StatusBadRequest, httpx.CodeValidation, "Each elective group may appear only once per submission.", nil)
	default:
		httpx.WriteError(w, r, http.StatusInternalServerError, httpx.CodeInternal, "Could not complete the operation.", nil)
	}
}

// requestDTO matches the OpenAPI EnrollmentRequest schema.
type requestDTO struct {
	ID                 string  `json:"id"`
	SemesterID         string  `json:"semesterId"`
	StudentID          string  `json:"studentId"`
	OfferingID         string  `json:"offeringId"`
	OfferingGroupID    string  `json:"offeringGroupId"`
	EnrollmentWindowID *string `json:"enrollmentWindowId"`
	StudentShift       string  `json:"studentShift"`
	OfferingShift      string  `json:"offeringShift"`
	PriorityGroup      string  `json:"priorityGroup"`
	Status             string  `json:"status"`
	ArrivalSequence    int64   `json:"arrivalSequence"`
	SubmittedAt        string  `json:"submittedAt"`
	CancelledAt        *string `json:"cancelledAt"`
	LatestReason       *string `json:"latestReason"`
}

func toDTO(v app.RequestView) requestDTO {
	return requestDTO{
		ID: v.ID, SemesterID: v.SemesterID, StudentID: v.StudentID, OfferingID: v.OfferingID,
		OfferingGroupID: v.OfferingGroupID, EnrollmentWindowID: v.EnrollmentWindowID,
		StudentShift: string(v.StudentShift), OfferingShift: string(v.OfferingShift),
		PriorityGroup: string(v.PriorityGroup), Status: string(v.Status), ArrivalSequence: v.ArrivalSequence,
		SubmittedAt: v.SubmittedAt, CancelledAt: v.CancelledAt, LatestReason: v.LatestReason,
	}
}
