// Package http is the driving adapter exposing student use cases over REST.
package http

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/uniquindio/profundiza-uq/internal/platform/authn"
	"github.com/uniquindio/profundiza-uq/internal/platform/httpx"
	shared "github.com/uniquindio/profundiza-uq/internal/shared/domain"
	"github.com/uniquindio/profundiza-uq/internal/student/app"
	"github.com/uniquindio/profundiza-uq/internal/student/domain"
)

// Handler adapts HTTP requests to the student Service.
type Handler struct {
	svc *app.Service
}

// NewHandler builds a student HTTP handler.
func NewHandler(svc *app.Service) *Handler { return &Handler{svc: svc} }

// Routes returns the chi router for the student resource.
func (h *Handler) Routes() chi.Router {
	r := chi.NewRouter()
	r.Get("/", h.list)
	r.Post("/", h.create)
	r.Post("/import", h.importStudents)
	r.Get("/{studentId}", h.get)
	r.Patch("/{studentId}", h.update)
	r.Get("/{studentId}/academic-records", h.listRecords)
	r.Post("/{studentId}/academic-records", h.createRecord)
	return r
}

// --- DTOs (aligned with the OpenAPI schemas) ---

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

func toStudentDTO(s domain.Student) studentDTO {
	return studentDTO{
		ID:                                  s.ID,
		InstitutionalEmail:                  s.InstitutionalEmail,
		DocumentNumber:                      s.DocumentNumber,
		FullName:                            s.FullName,
		AcademicShift:                       string(s.AcademicShift),
		Status:                              string(s.Status),
		CompletedProfessionalElectivesCount: s.CompletedProfessionalElectivesCount,
		CreatedAt:                           s.CreatedAt.UTC().Format(time.RFC3339),
		UpdatedAt:                           s.UpdatedAt.UTC().Format(time.RFC3339),
	}
}

type studentPageDTO struct {
	Items    []studentDTO `json:"items"`
	Page     int          `json:"page"`
	PageSize int          `json:"pageSize"`
	Total    int          `json:"total"`
}

type recordDTO struct {
	ID         string `json:"id"`
	StudentID  string `json:"studentId"`
	SemesterID string `json:"semesterId"`
	Notes      string `json:"notes"`
	Source     string `json:"source"`
	CreatedAt  string `json:"createdAt"`
}

func toRecordDTO(rec domain.AcademicRecord) recordDTO {
	semesterID := ""
	if rec.SemesterID != nil {
		semesterID = *rec.SemesterID
	}
	return recordDTO{
		ID:         rec.ID,
		StudentID:  rec.StudentID,
		SemesterID: semesterID,
		Notes:      rec.Notes,
		Source:     rec.Source,
		CreatedAt:  rec.CreatedAt.UTC().Format(time.RFC3339),
	}
}

type createStudentRequest struct {
	InstitutionalEmail                  string `json:"institutionalEmail"`
	DocumentNumber                      string `json:"documentNumber"`
	FullName                            string `json:"fullName"`
	AcademicShift                       string `json:"academicShift"`
	CompletedProfessionalElectivesCount int    `json:"completedProfessionalElectivesCount"`
}

func (c createStudentRequest) toDomain() domain.NewStudent {
	return domain.NewStudent{
		InstitutionalEmail:                  strings.TrimSpace(c.InstitutionalEmail),
		DocumentNumber:                      strings.TrimSpace(c.DocumentNumber),
		FullName:                            strings.TrimSpace(c.FullName),
		AcademicShift:                       shared.AcademicShift(c.AcademicShift),
		CompletedProfessionalElectivesCount: c.CompletedProfessionalElectivesCount,
	}
}

type updateStudentRequest struct {
	FullName                            *string `json:"fullName"`
	AcademicShift                       *string `json:"academicShift"`
	Status                              *string `json:"status"`
	CompletedProfessionalElectivesCount *int    `json:"completedProfessionalElectivesCount"`
}

func (u updateStudentRequest) toDomain() domain.StudentPatch {
	var p domain.StudentPatch
	p.FullName = u.FullName
	if u.AcademicShift != nil {
		s := shared.AcademicShift(*u.AcademicShift)
		p.AcademicShift = &s
	}
	if u.Status != nil {
		s := domain.Status(*u.Status)
		p.Status = &s
	}
	p.CompletedProfessionalElectivesCount = u.CompletedProfessionalElectivesCount
	return p
}

type createRecordRequest struct {
	SemesterID string `json:"semesterId"`
	Notes      string `json:"notes"`
	Source     string `json:"source"`
}

type importResultDTO struct {
	AcceptedRows int      `json:"acceptedRows"`
	RejectedRows int      `json:"rejectedRows"`
	Errors       []string `json:"errors"`
}

// --- handlers ---

func (h *Handler) list(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	f := app.ListFilter{
		Page:     atoiDefault(q.Get("page"), 0),
		PageSize: atoiDefault(q.Get("pageSize"), 0),
		Q:        q.Get("q"),
	}
	if s := q.Get("shift"); s == string(shared.ShiftDay) || s == string(shared.ShiftNight) {
		shift := shared.AcademicShift(s)
		f.Shift = &shift
	}
	if s := q.Get("status"); s == string(domain.StatusActive) || s == string(domain.StatusInactive) {
		st := domain.Status(s)
		f.Status = &st
	}

	page, err := h.svc.List(r.Context(), f)
	if err != nil {
		httpx.WriteError(w, r, http.StatusInternalServerError, httpx.CodeInternal, "Could not load students.", nil)
		return
	}
	items := make([]studentDTO, 0, len(page.Items))
	for _, s := range page.Items {
		items = append(items, toStudentDTO(s))
	}
	httpx.WriteJSON(w, http.StatusOK, studentPageDTO{Items: items, Page: page.Page, PageSize: page.PageSize, Total: page.Total})
}

func (h *Handler) create(w http.ResponseWriter, r *http.Request) {
	var req createStudentRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	st, err := h.svc.Create(r.Context(), req.toDomain(), actorOf(r))
	if err != nil {
		writeDomainError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusCreated, toStudentDTO(st))
}

func (h *Handler) get(w http.ResponseWriter, r *http.Request) {
	st, err := h.svc.Get(r.Context(), chi.URLParam(r, "studentId"))
	if err != nil {
		writeDomainError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, toStudentDTO(*st))
}

func (h *Handler) update(w http.ResponseWriter, r *http.Request) {
	var req updateStudentRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	st, err := h.svc.Update(r.Context(), chi.URLParam(r, "studentId"), req.toDomain(), actorOf(r))
	if err != nil {
		writeDomainError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, toStudentDTO(*st))
}

func (h *Handler) listRecords(w http.ResponseWriter, r *http.Request) {
	var semesterID *string
	if s := r.URL.Query().Get("semesterId"); s != "" {
		semesterID = &s
	}
	recs, err := h.svc.ListAcademicRecords(r.Context(), chi.URLParam(r, "studentId"), semesterID)
	if err != nil {
		writeDomainError(w, r, err)
		return
	}
	items := make([]recordDTO, 0, len(recs))
	for _, rec := range recs {
		items = append(items, toRecordDTO(rec))
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (h *Handler) createRecord(w http.ResponseWriter, r *http.Request) {
	var req createRecordRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	rec, err := h.svc.CreateAcademicRecord(r.Context(), chi.URLParam(r, "studentId"), domain.NewAcademicRecord{
		SemesterID: strings.TrimSpace(req.SemesterID),
		Notes:      strings.TrimSpace(req.Notes),
		Source:     strings.TrimSpace(req.Source),
	}, actorOf(r))
	if err != nil {
		writeDomainError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusCreated, toRecordDTO(*rec))
}

// importStudents accepts a JSON array body {"students": [CreateStudentRequest...]}.
// It also accepts a bare top-level array for convenience.
func (h *Handler) importStudents(w http.ResponseWriter, r *http.Request) {
	rows, ok := decodeImportBody(w, r)
	if !ok {
		return
	}
	domainRows := make([]domain.NewStudent, 0, len(rows))
	for _, row := range rows {
		domainRows = append(domainRows, row.toDomain())
	}
	res, err := h.svc.Import(r.Context(), domainRows, actorOf(r))
	if err != nil {
		httpx.WriteError(w, r, http.StatusInternalServerError, httpx.CodeInternal, "Could not import students.", nil)
		return
	}
	httpx.WriteJSON(w, http.StatusAccepted, importResultDTO{
		AcceptedRows: res.AcceptedRows,
		RejectedRows: res.RejectedRows,
		Errors:       res.Errors,
	})
}

// --- shared helpers ---

func decodeImportBody(w http.ResponseWriter, r *http.Request) ([]createStudentRequest, bool) {
	defer r.Body.Close()
	var wrapper struct {
		Students []createStudentRequest `json:"students"`
	}
	raw, err := io.ReadAll(r.Body)
	if err != nil {
		httpx.WriteError(w, r, http.StatusBadRequest, httpx.CodeBadRequest, "Could not read request body.", nil)
		return nil, false
	}
	if err := json.Unmarshal(raw, &wrapper); err == nil && wrapper.Students != nil {
		return wrapper.Students, true
	}
	var bare []createStudentRequest
	if err := json.Unmarshal(raw, &bare); err == nil {
		return bare, true
	}
	httpx.WriteError(w, r, http.StatusBadRequest, httpx.CodeValidation,
		"Expected a JSON body of the form {\"students\":[...]}.", nil)
	return nil, false
}

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
	case errors.Is(err, app.ErrStudentNotFound):
		httpx.WriteError(w, r, http.StatusNotFound, httpx.CodeNotFound, "Student not found.", nil)
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
