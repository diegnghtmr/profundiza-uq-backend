// Package http is the driving adapter exposing the reporting use cases over
// REST. All routes are admin/superadmin only (enforced by the router guard).
package http

import (
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/uniquindio/profundiza-uq/internal/platform/authn"
	"github.com/uniquindio/profundiza-uq/internal/platform/httpx"
	"github.com/uniquindio/profundiza-uq/internal/reporting/app"
	"github.com/uniquindio/profundiza-uq/internal/reporting/domain"
)

// Handler adapts HTTP requests to the reporting Service.
type Handler struct {
	svc *app.Service
}

// NewHandler builds a reporting HTTP handler.
func NewHandler(svc *app.Service) *Handler {
	return &Handler{svc: svc}
}

// Routes returns the chi router for the reports resource.
func (h *Handler) Routes() chi.Router {
	r := chi.NewRouter()
	r.Get("/", h.list)
	r.Post("/", h.create)
	r.Get("/{reportId}", h.get)
	r.Get("/{reportId}/download", h.download)
	return r
}

// createReportRequest mirrors the OpenAPI CreateReportExportRequest schema.
type createReportRequest struct {
	SemesterID string         `json:"semesterId"`
	ReportType string         `json:"reportType"`
	Format     string         `json:"format"`
	Filters    map[string]any `json:"filters"`
}

// reportExportDTO mirrors the OpenAPI ReportExport schema.
type reportExportDTO struct {
	ID                     string         `json:"id"`
	SemesterID             *string        `json:"semesterId"`
	RequestedByAdminUserID string         `json:"requestedByAdminUserId"`
	ReportType             string         `json:"reportType"`
	Format                 string         `json:"format"`
	Status                 string         `json:"status"`
	Filters                map[string]any `json:"filters"`
	FilePath               *string        `json:"filePath"`
	DownloadURL            *string        `json:"downloadUrl"`
	FailureReason          *string        `json:"failureReason"`
	RequestedAt            string         `json:"requestedAt"`
	StartedAt              *string        `json:"startedAt"`
	CompletedAt            *string        `json:"completedAt"`
}

func toDTO(e domain.ReportExport) reportExportDTO {
	dto := reportExportDTO{
		ID:                     e.ID,
		SemesterID:             e.SemesterID,
		RequestedByAdminUserID: e.RequestedByAdminID,
		ReportType:             string(e.ReportType),
		Format:                 string(e.Format),
		Status:                 string(e.Status),
		Filters:                e.Filters,
		FilePath:               e.FilePath,
		FailureReason:          e.FailureReason,
		RequestedAt:            e.RequestedAt.UTC().Format(time.RFC3339),
		StartedAt:              formatTimePtr(e.StartedAt),
		CompletedAt:            formatTimePtr(e.CompletedAt),
	}
	// Only completed exports are downloadable.
	if e.Status == domain.StatusCompleted {
		url := "/api/v1/reports/" + e.ID + "/download"
		dto.DownloadURL = &url
	}
	return dto
}

func formatTimePtr(t *time.Time) *string {
	if t == nil {
		return nil
	}
	s := t.UTC().Format(time.RFC3339)
	return &s
}

func (h *Handler) list(w http.ResponseWriter, r *http.Request) {
	semesterID := r.URL.Query().Get("semesterId")
	if semesterID == "" {
		httpx.WriteError(w, r, http.StatusBadRequest, httpx.CodeValidation, "semesterId is required.", nil)
		return
	}
	exports, err := h.svc.ListBySemester(r.Context(), semesterID)
	if err != nil {
		httpx.WriteError(w, r, http.StatusInternalServerError, httpx.CodeInternal, "Could not load report exports.", nil)
		return
	}
	items := make([]reportExportDTO, 0, len(exports))
	for _, e := range exports {
		items = append(items, toDTO(e))
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (h *Handler) create(w http.ResponseWriter, r *http.Request) {
	principal := authn.FromContext(r.Context())
	adminID, ok := principal.AdminUserID()
	if !ok {
		httpx.WriteError(w, r, http.StatusForbidden, httpx.CodeForbidden, "Only administrators can request reports.", nil)
		return
	}

	var body createReportRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		httpx.WriteError(w, r, http.StatusBadRequest, httpx.CodeBadRequest, "Invalid request body.", nil)
		return
	}

	var semesterID *string
	if body.SemesterID != "" {
		semesterID = &body.SemesterID
	}

	export, err := h.svc.Create(r.Context(), app.CreateRequest{
		RequestedByAdminID: adminID,
		SemesterID:         semesterID,
		ReportType:         domain.ReportType(body.ReportType),
		Format:             domain.Format(body.Format),
		Filters:            body.Filters,
	})
	if err != nil {
		switch {
		case errors.Is(err, domain.ErrInvalidReportType),
			errors.Is(err, domain.ErrInvalidFormat),
			errors.Is(err, domain.ErrSemesterRequired):
			httpx.WriteError(w, r, http.StatusBadRequest, httpx.CodeValidation, err.Error(), nil)
		default:
			httpx.WriteError(w, r, http.StatusInternalServerError, httpx.CodeInternal, "Could not request report.", nil)
		}
		return
	}
	// Asynchronous: the worker generates the file later (TRD §15).
	httpx.WriteJSON(w, http.StatusAccepted, toDTO(export))
}

func (h *Handler) get(w http.ResponseWriter, r *http.Request) {
	export, err := h.svc.Get(r.Context(), chi.URLParam(r, "reportId"))
	if errors.Is(err, app.ErrNotFound) {
		httpx.WriteError(w, r, http.StatusNotFound, httpx.CodeNotFound, "Report export not found.", nil)
		return
	}
	if err != nil {
		httpx.WriteError(w, r, http.StatusInternalServerError, httpx.CodeInternal, "Could not load report export.", nil)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, toDTO(export))
}

func (h *Handler) download(w http.ResponseWriter, r *http.Request) {
	export, err := h.svc.Get(r.Context(), chi.URLParam(r, "reportId"))
	if errors.Is(err, app.ErrNotFound) {
		httpx.WriteError(w, r, http.StatusNotFound, httpx.CodeNotFound, "Report export not found.", nil)
		return
	}
	if err != nil {
		httpx.WriteError(w, r, http.StatusInternalServerError, httpx.CodeInternal, "Could not load report export.", nil)
		return
	}
	if export.Status != domain.StatusCompleted || export.FilePath == nil {
		httpx.WriteError(w, r, http.StatusConflict, httpx.CodeConflict, "Report is not ready for download.", nil)
		return
	}

	f, err := os.Open(*export.FilePath)
	if err != nil {
		httpx.WriteError(w, r, http.StatusNotFound, httpx.CodeNotFound, "Report file is no longer available.", nil)
		return
	}
	defer func() { _ = f.Close() }()

	info, err := f.Stat()
	if err != nil {
		httpx.WriteError(w, r, http.StatusInternalServerError, httpx.CodeInternal, "Could not read report file.", nil)
		return
	}

	filename := export.ID + "." + export.FileExtension()
	w.Header().Set("Content-Type", export.ContentType())
	w.Header().Set("Content-Disposition", `attachment; filename="`+filename+`"`)
	http.ServeContent(w, r, filename, info.ModTime(), f)
}
