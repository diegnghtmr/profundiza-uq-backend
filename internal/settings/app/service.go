// Package app holds the global-settings use cases. It depends on the domain and
// on a Repository port, never on a concrete database.
package app

import (
	"context"

	"github.com/uniquindio/profundiza-uq/internal/settings/domain"
)

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

// ListFilter carries pagination for the settings listing.
type ListFilter struct {
	Page     int
	PageSize int
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

// Page is a paginated slice of global settings.
type Page struct {
	Items    []domain.GlobalSetting
	Page     int
	PageSize int
	Total    int
}

// Repository is the output port for global-settings persistence.
type Repository interface {
	List(ctx context.Context, f ListFilter) (items []domain.GlobalSetting, total int, err error)
	GetByKey(ctx context.Context, key string) (domain.GlobalSetting, bool, error)
	Upsert(ctx context.Context, in domain.UpsertSetting, actor Actor) (domain.GlobalSetting, error)
}

// Service implements global-settings use cases.
type Service struct {
	repo Repository
}

// NewService wires a Service with its repository port.
func NewService(repo Repository) *Service { return &Service{repo: repo} }

// List returns a paginated page of settings.
func (s *Service) List(ctx context.Context, f ListFilter) (Page, error) {
	f.Normalize()
	items, total, err := s.repo.List(ctx, f)
	if err != nil {
		return Page{}, err
	}
	if items == nil {
		items = []domain.GlobalSetting{}
	}
	return Page{Items: items, Page: f.Page, PageSize: f.PageSize, Total: total}, nil
}

// Get returns a single setting by key. ok is false when no setting exists for
// the key — not an error — so callers can apply their own default.
func (s *Service) Get(ctx context.Context, key string) (domain.GlobalSetting, bool, error) {
	return s.repo.GetByKey(ctx, key)
}

// Upsert validates and persists a setting value, writing an audit event.
func (s *Service) Upsert(ctx context.Context, in domain.UpsertSetting, actor Actor) (domain.GlobalSetting, error) {
	if err := in.Validate(); err != nil {
		return domain.GlobalSetting{}, err
	}
	return s.repo.Upsert(ctx, in, actor)
}
