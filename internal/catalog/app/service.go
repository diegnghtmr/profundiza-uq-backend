// Package app holds catalog read use cases for the student offering browser.
package app

import (
	"context"

	"github.com/uniquindio/profundiza-uq/internal/catalog/domain"
	shared "github.com/uniquindio/profundiza-uq/internal/shared/domain"
)

// OfferingFilter narrows the offering list.
type OfferingFilter struct {
	SemesterID string
	Shift      *shared.AcademicShift // optional group-shift filter
	OnlyOpen   bool                  // keep only offerings with at least one open group
}

// Repository is the catalog read port.
type Repository interface {
	ListOfferings(ctx context.Context, f OfferingFilter) ([]domain.OfferingSummary, error)
	GetOfferingDetail(ctx context.Context, offeringID string) (*domain.OfferingDetail, error)
	ListEffectivePrerequisites(ctx context.Context, offeringID string) ([]domain.Prerequisite, error)
}

// Service exposes catalog read use cases.
type Service struct {
	repo Repository
}

// NewService wires the catalog service.
func NewService(repo Repository) *Service { return &Service{repo: repo} }

// ListOfferings returns offerings for a semester, applying optional filters.
func (s *Service) ListOfferings(ctx context.Context, f OfferingFilter) ([]domain.OfferingSummary, error) {
	return s.repo.ListOfferings(ctx, f)
}

// GetOfferingDetail returns one offering with effective prerequisites and groups.
func (s *Service) GetOfferingDetail(ctx context.Context, offeringID string) (*domain.OfferingDetail, error) {
	return s.repo.GetOfferingDetail(ctx, offeringID)
}

// ListEffectivePrerequisites returns the effective prerequisites of an offering.
func (s *Service) ListEffectivePrerequisites(ctx context.Context, offeringID string) ([]domain.Prerequisite, error) {
	return s.repo.ListEffectivePrerequisites(ctx, offeringID)
}
