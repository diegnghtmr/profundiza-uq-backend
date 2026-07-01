// Package app holds the student use cases. It depends on the domain and on a
// Repository port, never on a concrete database.
package app

import (
	"context"
	"errors"
	"strconv"

	shared "github.com/uniquindio/profundiza-uq/internal/shared/domain"
	"github.com/uniquindio/profundiza-uq/internal/student/domain"
)

// ErrStudentNotFound is returned when a student id does not resolve.
var ErrStudentNotFound = errors.New("student: not found")

// ErrEmailTaken is returned when the institutional email already exists.
var ErrEmailTaken = errors.New("student: institutional email already in use")

// Pagination defaults shared by every paginated listing.
const (
	defaultPage     = 1
	defaultPageSize = 20
	maxPageSize     = 100
)

// Actor is the principal performing a state-changing operation (for audit).
type Actor struct {
	Type string // STUDENT | ADMIN | SYSTEM
	ID   string // empty => NULL actor
}

// ListFilter narrows the student listing and carries pagination.
type ListFilter struct {
	Page     int
	PageSize int
	Q        string
	Shift    *shared.AcademicShift
	Status   *domain.Status
}

// Normalize clamps pagination to its valid bounds and returns the LIMIT/OFFSET.
func (f *ListFilter) Normalize() (limit, offset int) {
	if f.Page < 1 {
		f.Page = defaultPage
	}
	if f.PageSize < 1 {
		f.PageSize = defaultPageSize
	}
	if f.PageSize > maxPageSize {
		f.PageSize = maxPageSize
	}
	return f.PageSize, (f.Page - 1) * f.PageSize
}

// Page is a paginated slice of students.
type Page struct {
	Items    []domain.Student
	Page     int
	PageSize int
	Total    int
}

// ImportResult summarizes a bulk import (OpenAPI ImportResult).
type ImportResult struct {
	AcceptedRows int
	RejectedRows int
	Errors       []string
}

// Repository is the output port for student persistence.
type Repository interface {
	List(ctx context.Context, f ListFilter) (items []domain.Student, total int, err error)
	Create(ctx context.Context, in domain.NewStudent, actor Actor) (domain.Student, error)
	Get(ctx context.Context, id string) (*domain.Student, error)
	Update(ctx context.Context, id string, patch domain.StudentPatch, actor Actor) (*domain.Student, error)
	ListAcademicRecords(ctx context.Context, studentID string, semesterID *string) ([]domain.AcademicRecord, error)
	CreateAcademicRecord(ctx context.Context, studentID string, in domain.NewAcademicRecord, actor Actor) (*domain.AcademicRecord, error)
}

// Service implements student use cases.
type Service struct {
	repo Repository
}

// NewService wires a Service with its repository port.
func NewService(repo Repository) *Service { return &Service{repo: repo} }

// List returns a paginated, filtered page of students.
func (s *Service) List(ctx context.Context, f ListFilter) (Page, error) {
	f.Normalize()
	items, total, err := s.repo.List(ctx, f)
	if err != nil {
		return Page{}, err
	}
	if items == nil {
		items = []domain.Student{}
	}
	return Page{Items: items, Page: f.Page, PageSize: f.PageSize, Total: total}, nil
}

// Create validates and persists a new student, writing an audit event.
func (s *Service) Create(ctx context.Context, in domain.NewStudent, actor Actor) (domain.Student, error) {
	if err := in.Validate(); err != nil {
		return domain.Student{}, err
	}
	return s.repo.Create(ctx, in, actor)
}

// Get returns a single student or ErrStudentNotFound.
func (s *Service) Get(ctx context.Context, id string) (*domain.Student, error) {
	st, err := s.repo.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	if st == nil {
		return nil, ErrStudentNotFound
	}
	return st, nil
}

// Update validates and applies a partial update, writing an audit event.
func (s *Service) Update(ctx context.Context, id string, patch domain.StudentPatch, actor Actor) (*domain.Student, error) {
	if err := patch.Validate(); err != nil {
		return nil, err
	}
	st, err := s.repo.Update(ctx, id, patch, actor)
	if err != nil {
		return nil, err
	}
	if st == nil {
		return nil, ErrStudentNotFound
	}
	return st, nil
}

// Import validates each row and persists the valid ones, returning a summary.
// Invalid or conflicting rows are reported in Errors without aborting the batch.
func (s *Service) Import(ctx context.Context, rows []domain.NewStudent, actor Actor) (ImportResult, error) {
	res := ImportResult{Errors: []string{}}
	for i, row := range rows {
		if err := row.Validate(); err != nil {
			res.RejectedRows++
			res.Errors = append(res.Errors, rowError(i, err))
			continue
		}
		if _, err := s.repo.Create(ctx, row, actor); err != nil {
			res.RejectedRows++
			res.Errors = append(res.Errors, rowError(i, err))
			continue
		}
		res.AcceptedRows++
	}
	return res, nil
}

// ListAcademicRecords returns the manual records for a student, optionally
// filtered by semester.
func (s *Service) ListAcademicRecords(ctx context.Context, studentID string, semesterID *string) ([]domain.AcademicRecord, error) {
	if _, err := s.Get(ctx, studentID); err != nil {
		return nil, err
	}
	recs, err := s.repo.ListAcademicRecords(ctx, studentID, semesterID)
	if err != nil {
		return nil, err
	}
	if recs == nil {
		recs = []domain.AcademicRecord{}
	}
	return recs, nil
}

// CreateAcademicRecord validates and persists a manual record, writing an audit event.
func (s *Service) CreateAcademicRecord(ctx context.Context, studentID string, in domain.NewAcademicRecord, actor Actor) (*domain.AcademicRecord, error) {
	if err := in.Validate(); err != nil {
		return nil, err
	}
	rec, err := s.repo.CreateAcademicRecord(ctx, studentID, in, actor)
	if err != nil {
		return nil, err
	}
	if rec == nil {
		return nil, ErrStudentNotFound
	}
	return rec, nil
}

func rowError(index int, err error) string {
	return "row " + strconv.Itoa(index+1) + ": " + err.Error()
}
