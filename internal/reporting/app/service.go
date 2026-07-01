// Package app holds the reporting use cases. It depends on the domain and on
// the Repository and Generator ports, never on a concrete database or file
// format library.
package app

import (
	"context"
	"errors"

	"github.com/uniquindio/profundiza-uq/internal/reporting/domain"
)

// ErrNotFound is returned by the Service when a report export does not exist.
var ErrNotFound = errors.New("reporting: report export not found")

// CreateRequest is the input for requesting a new report export.
type CreateRequest struct {
	RequestedByAdminID string
	SemesterID         *string
	ReportType         domain.ReportType
	Format             domain.Format
	Filters            map[string]any
}

// Repository is the output port for report-export persistence.
type Repository interface {
	// Create inserts a new export in REQUESTED state and returns the stored row.
	Create(ctx context.Context, e domain.ReportExport) (domain.ReportExport, error)
	// Get returns the export by id, or ErrNotFound when it does not exist.
	Get(ctx context.Context, id string) (domain.ReportExport, error)
	// ListBySemester returns exports for a semester, newest first.
	ListBySemester(ctx context.Context, semesterID string) ([]domain.ReportExport, error)
	// ClaimNext atomically claims the oldest REQUESTED export, marking it
	// PROCESSING, and returns it. It returns ErrNoJob when the queue is empty.
	// Implementations MUST use row-level locking (SELECT ... FOR UPDATE SKIP
	// LOCKED) so concurrent workers never claim the same job.
	ClaimNext(ctx context.Context) (domain.ReportExport, error)
	// MarkCompleted records the generated file path and COMPLETED status.
	MarkCompleted(ctx context.Context, id, filePath string) error
	// MarkFailed records the failure reason and FAILED status.
	MarkFailed(ctx context.Context, id, reason string) error
}

// ErrNoJob is returned by Repository.ClaimNext when no REQUESTED export is
// available to claim.
var ErrNoJob = errors.New("reporting: no report job available")

// Generator is the output port that turns a claimed export into a file on disk,
// returning the absolute or relative path of the written file.
type Generator interface {
	Generate(ctx context.Context, e domain.ReportExport) (filePath string, err error)
}

// Service implements the reporting use cases.
type Service struct {
	repo Repository
}

// NewService wires a Service with its repository port.
func NewService(repo Repository) *Service {
	return &Service{repo: repo}
}

// Create validates and persists a new report export in REQUESTED state. The
// background worker picks it up asynchronously; this call returns immediately.
func (s *Service) Create(ctx context.Context, req CreateRequest) (domain.ReportExport, error) {
	filters := req.Filters
	if filters == nil {
		filters = map[string]any{}
	}
	e := domain.ReportExport{
		RequestedByAdminID: req.RequestedByAdminID,
		SemesterID:         req.SemesterID,
		ReportType:         req.ReportType,
		Format:             req.Format,
		Status:             domain.StatusRequested,
		Filters:            filters,
	}
	if err := e.Validate(); err != nil {
		return domain.ReportExport{}, err
	}
	return s.repo.Create(ctx, e)
}

// Get returns a single report export, or ErrNotFound.
func (s *Service) Get(ctx context.Context, id string) (domain.ReportExport, error) {
	return s.repo.Get(ctx, id)
}

// ListBySemester returns the exports requested for a semester, newest first.
func (s *Service) ListBySemester(ctx context.Context, semesterID string) ([]domain.ReportExport, error) {
	return s.repo.ListBySemester(ctx, semesterID)
}
