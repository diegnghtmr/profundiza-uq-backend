// Package app holds the semester use cases. It depends on the domain and on a
// Repository port, never on a concrete database.
package app

import (
	"context"
	"errors"
	"time"

	"github.com/uniquindio/profundiza-uq/internal/semester/domain"
)

// Errors surfaced to the HTTP adapter.
var (
	ErrNotFound = errors.New("semester: not found")
	ErrInvalid  = errors.New("semester: invalid input")
)

// CreateInput is the command to create a semester.
type CreateInput struct {
	Code     string
	Name     string
	StartsAt time.Time
	EndsAt   time.Time
	ActorID  string
}

// Repository is the output port for semester persistence.
type Repository interface {
	List(ctx context.Context) ([]domain.Semester, error)
	GetActive(ctx context.Context) (*domain.Semester, error)
	Get(ctx context.Context, id string) (*domain.Semester, error)
	Create(ctx context.Context, in CreateInput) (domain.Semester, error)
	Activate(ctx context.Context, id, actorID string) (*domain.Semester, error)
	Close(ctx context.Context, id, actorID, reason string) (*domain.Semester, error)
}

// Service implements semester use cases.
type Service struct {
	repo Repository
}

// NewService wires a Service with its repository port.
func NewService(repo Repository) *Service {
	return &Service{repo: repo}
}

// List returns all semesters, newest first.
func (s *Service) List(ctx context.Context) ([]domain.Semester, error) {
	return s.repo.List(ctx)
}

// Active returns the currently active semester, or nil when none is active.
func (s *Service) Active(ctx context.Context) (*domain.Semester, error) {
	return s.repo.GetActive(ctx)
}

// Get returns one semester or ErrNotFound.
func (s *Service) Get(ctx context.Context, id string) (*domain.Semester, error) {
	sem, err := s.repo.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	if sem == nil {
		return nil, ErrNotFound
	}
	return sem, nil
}

// Create creates a semester in DRAFT state.
func (s *Service) Create(ctx context.Context, in CreateInput) (domain.Semester, error) {
	if in.Code == "" || in.Name == "" || !in.EndsAt.After(in.StartsAt) {
		return domain.Semester{}, ErrInvalid
	}
	return s.repo.Create(ctx, in)
}

// Activate makes a semester the single active one (the previously active one is
// closed) inside a transaction.
func (s *Service) Activate(ctx context.Context, id, actorID string) (*domain.Semester, error) {
	sem, err := s.repo.Activate(ctx, id, actorID)
	if err != nil {
		return nil, err
	}
	if sem == nil {
		return nil, ErrNotFound
	}
	return sem, nil
}

// Close closes a semester (a reason is required).
func (s *Service) Close(ctx context.Context, id, actorID, reason string) (*domain.Semester, error) {
	if len(reason) < 3 {
		return nil, ErrInvalid
	}
	sem, err := s.repo.Close(ctx, id, actorID, reason)
	if err != nil {
		return nil, err
	}
	if sem == nil {
		return nil, ErrNotFound
	}
	return sem, nil
}
