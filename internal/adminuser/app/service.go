// Package app holds the administrative-user use cases. It depends on the domain
// and on a Repository port, never on a concrete database.
package app

import (
	"context"
	"errors"

	"github.com/uniquindio/profundiza-uq/internal/adminuser/domain"
)

// ErrNotFound is returned when an admin user id does not resolve.
var ErrNotFound = errors.New("adminuser: not found")

// ErrEmailTaken is returned when the institutional email already exists.
var ErrEmailTaken = errors.New("adminuser: institutional email already in use")

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

// ListFilter narrows the admin-user listing and carries pagination.
type ListFilter struct {
	Page     int
	PageSize int
	Role     *domain.Role
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

// Page is a paginated slice of admin users.
type Page struct {
	Items    []domain.AdminUser
	Page     int
	PageSize int
	Total    int
}

// Repository is the output port for admin-user persistence.
type Repository interface {
	List(ctx context.Context, f ListFilter) (items []domain.AdminUser, total int, err error)
	Create(ctx context.Context, in domain.NewAdminUser, actor Actor) (domain.AdminUser, error)
	Get(ctx context.Context, id string) (*domain.AdminUser, error)
	Update(ctx context.Context, id string, patch domain.AdminUserPatch, actor Actor) (*domain.AdminUser, error)
}

// Service implements admin-user use cases.
type Service struct {
	repo Repository
}

// NewService wires a Service with its repository port.
func NewService(repo Repository) *Service { return &Service{repo: repo} }

// List returns a paginated, filtered page of admin users.
func (s *Service) List(ctx context.Context, f ListFilter) (Page, error) {
	f.Normalize()
	items, total, err := s.repo.List(ctx, f)
	if err != nil {
		return Page{}, err
	}
	if items == nil {
		items = []domain.AdminUser{}
	}
	return Page{Items: items, Page: f.Page, PageSize: f.PageSize, Total: total}, nil
}

// Create validates and persists a new admin user, writing an audit event.
func (s *Service) Create(ctx context.Context, in domain.NewAdminUser, actor Actor) (domain.AdminUser, error) {
	if err := in.Validate(); err != nil {
		return domain.AdminUser{}, err
	}
	return s.repo.Create(ctx, in, actor)
}

// Get returns a single admin user or ErrNotFound.
func (s *Service) Get(ctx context.Context, id string) (*domain.AdminUser, error) {
	u, err := s.repo.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	if u == nil {
		return nil, ErrNotFound
	}
	return u, nil
}

// Update validates and applies a partial update, writing an audit event.
func (s *Service) Update(ctx context.Context, id string, patch domain.AdminUserPatch, actor Actor) (*domain.AdminUser, error) {
	if err := patch.Validate(); err != nil {
		return nil, err
	}
	u, err := s.repo.Update(ctx, id, patch, actor)
	if err != nil {
		return nil, err
	}
	if u == nil {
		return nil, ErrNotFound
	}
	return u, nil
}
