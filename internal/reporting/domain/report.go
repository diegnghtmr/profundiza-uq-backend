// Package domain models the reporting bounded context: asynchronous report
// exports (XLSX / PDF) requested by administrators and produced by a background
// worker (TRD §15).
package domain

import (
	"errors"
	"time"
)

// ReportType is the kind of report to generate. Values mirror the OpenAPI
// ReportType enum.
type ReportType string

const (
	ReportGeneralSemester   ReportType = "GENERAL_SEMESTER"
	ReportByElective        ReportType = "BY_ELECTIVE"
	ReportByGroup           ReportType = "BY_GROUP"
	ReportByStudent         ReportType = "BY_STUDENT"
	ReportWaitlist          ReportType = "WAITLIST"
	ReportAcceptedRequests  ReportType = "ACCEPTED_REQUESTS"
	ReportRejectedRequests  ReportType = "REJECTED_REQUESTS"
	ReportCancelledRequests ReportType = "CANCELLED_REQUESTS"
	ReportAudit             ReportType = "AUDIT"
	ReportCapacity          ReportType = "CAPACITY"
	ReportAdminReview       ReportType = "ADMIN_REVIEW"
)

// Format is the output file format. Values mirror the OpenAPI ReportFormat enum.
type Format string

const (
	FormatXLSX Format = "XLSX"
	FormatPDF  Format = "PDF"
)

// Status is the lifecycle of a report export. Values mirror the OpenAPI
// ReportExportStatus enum.
type Status string

const (
	StatusRequested  Status = "REQUESTED"
	StatusProcessing Status = "PROCESSING"
	StatusCompleted  Status = "COMPLETED"
	StatusFailed     Status = "FAILED"
	StatusExpired    Status = "EXPIRED"
)

// Validation errors surfaced by the domain.
var (
	// ErrInvalidReportType is returned when an unknown report type is supplied.
	ErrInvalidReportType = errors.New("reporting: invalid report type")
	// ErrInvalidFormat is returned when an unknown output format is supplied.
	ErrInvalidFormat = errors.New("reporting: invalid report format")
	// ErrSemesterRequired is returned when a report needs a semester scope but
	// none was supplied.
	ErrSemesterRequired = errors.New("reporting: semesterId is required")
)

// ReportExport is the aggregate root of the reporting context: a single
// requested export and its lifecycle metadata. Timestamps are stored in UTC.
type ReportExport struct {
	ID                 string
	RequestedByAdminID string
	SemesterID         *string
	ReportType         ReportType
	Format             Format
	Status             Status
	Filters            map[string]any
	FilePath           *string
	FailureReason      *string
	RequestedAt        time.Time
	StartedAt          *time.Time
	CompletedAt        *time.Time
}

// validReportTypes is the set of report types accepted by the domain.
var validReportTypes = map[ReportType]bool{
	ReportGeneralSemester:   true,
	ReportByElective:        true,
	ReportByGroup:           true,
	ReportByStudent:         true,
	ReportWaitlist:          true,
	ReportAcceptedRequests:  true,
	ReportRejectedRequests:  true,
	ReportCancelledRequests: true,
	ReportAudit:             true,
	ReportCapacity:          true,
	ReportAdminReview:       true,
}

// ValidReportType reports whether t is a recognized report type.
func ValidReportType(t ReportType) bool { return validReportTypes[t] }

// ValidFormat reports whether f is a recognized output format.
func ValidFormat(f Format) bool { return f == FormatXLSX || f == FormatPDF }

// Validate checks that the report request is well-formed: the type and format
// must be recognized and a semester scope must be present. It returns the first
// violation found, or nil when the request is valid.
func (e ReportExport) Validate() error {
	if !ValidReportType(e.ReportType) {
		return ErrInvalidReportType
	}
	if !ValidFormat(e.Format) {
		return ErrInvalidFormat
	}
	if e.SemesterID == nil || *e.SemesterID == "" {
		return ErrSemesterRequired
	}
	return nil
}

// FileExtension returns the lowercase file extension (without the dot) for the
// export's format.
func (e ReportExport) FileExtension() string {
	if e.Format == FormatPDF {
		return "pdf"
	}
	return "xlsx"
}

// ContentType returns the HTTP content type matching the export's format.
func (e ReportExport) ContentType() string {
	if e.Format == FormatPDF {
		return "application/pdf"
	}
	return "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet"
}
