// Package app holds enrollment-window use cases.
package app

import (
	"context"
	"errors"
	"time"

	"github.com/uniquindio/profundiza-uq/internal/window/domain"
)

// Errors.
var (
	ErrNotFound = errors.New("window: not found")
	ErrInvalid  = errors.New("window: invalid input")
)

// CreateInput creates a window.
type CreateInput struct {
	SemesterID  string
	Name        string
	StartsAt    time.Time
	EndsAt      time.Time
	TargetShift *string
	ActorID     string
}

// UpdateInput updates a window's editable fields.
type UpdateInput struct {
	ID          string
	Name        *string
	StartsAt    *time.Time
	EndsAt      *time.Time
	TargetShift *string
	Status      *string
	ActorID     string
}

// Repository is the window persistence port.
type Repository interface {
	ListBySemester(ctx context.Context, semesterID string) ([]domain.Window, error)
	Get(ctx context.Context, id string) (*domain.Window, error)
	Create(ctx context.Context, in CreateInput) (domain.Window, error)
	Update(ctx context.Context, in UpdateInput) (domain.Window, error)
}

// Service exposes window use cases.
type Service struct{ repo Repository }

// NewService wires the window service.
func NewService(repo Repository) *Service { return &Service{repo: repo} }

// ListBySemester lists a semester's windows.
func (s *Service) ListBySemester(ctx context.Context, semesterID string) ([]domain.Window, error) {
	return s.repo.ListBySemester(ctx, semesterID)
}

// Get returns one window.
func (s *Service) Get(ctx context.Context, id string) (*domain.Window, error) {
	w, err := s.repo.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	if w == nil {
		return nil, ErrNotFound
	}
	return w, nil
}

// Create creates a window.
func (s *Service) Create(ctx context.Context, in CreateInput) (domain.Window, error) {
	if in.SemesterID == "" || in.Name == "" || !in.EndsAt.After(in.StartsAt) {
		return domain.Window{}, ErrInvalid
	}
	return s.repo.Create(ctx, in)
}

// Update updates a window.
func (s *Service) Update(ctx context.Context, in UpdateInput) (domain.Window, error) {
	return s.repo.Update(ctx, in)
}
