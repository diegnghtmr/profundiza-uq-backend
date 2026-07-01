// Package app holds the audit read use case. It depends on the domain and on a
// Repository port, never on a concrete database.
package app

import (
	"context"

	"github.com/uniquindio/profundiza-uq/internal/audit/domain"
)

// Pagination defaults shared by every paginated listing.
const (
	defaultPage     = 1
	defaultPageSize = 20
	maxPageSize     = 100
)

// ListFilter narrows the audit-event listing and carries pagination. A nil
// pointer means "do not filter on this field".
type ListFilter struct {
	Page       int
	PageSize   int
	EntityType *string
	EntityID   *string
	ActorID    *string
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

// Page is a paginated slice of audit events.
type Page struct {
	Items    []domain.AuditEvent
	Page     int
	PageSize int
	Total    int
}

// Repository is the read port for audit events.
type Repository interface {
	List(ctx context.Context, f ListFilter) (items []domain.AuditEvent, total int, err error)
}

// Service exposes the audit read use case.
type Service struct {
	repo Repository
}

// NewService wires a Service with its repository port.
func NewService(repo Repository) *Service { return &Service{repo: repo} }

// List returns a paginated, filtered page of audit events, newest first.
func (s *Service) List(ctx context.Context, f ListFilter) (Page, error) {
	f.Normalize()
	items, total, err := s.repo.List(ctx, f)
	if err != nil {
		return Page{}, err
	}
	if items == nil {
		items = []domain.AuditEvent{}
	}
	return Page{Items: items, Page: f.Page, PageSize: f.PageSize, Total: total}, nil
}
