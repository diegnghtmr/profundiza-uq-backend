// Package app holds the enrollment use cases. The capacity-sensitive submission
// is intentionally modeled as a transaction script (TRD §10.2) behind the
// Submitter port; the pure rules it relies on live in the domain package.
package app

import (
	"context"

	"github.com/uniquindio/profundiza-uq/internal/enrollment/domain"
)

// SubmitInput is the command to enroll a student into one offering group.
type SubmitInput struct {
	SemesterID      string
	StudentID       string
	OfferingGroupID string
	IdempotencyKey  string
}

// SubmittedRequest is the outcome of a submission. Existed is true when an
// idempotent replay returned the original request instead of creating a new one.
type SubmittedRequest struct {
	ID              string
	OfferingID      string
	Status          domain.RequestStatus
	PriorityGroup   domain.PriorityGroup
	ArrivalSequence int64
	Existed         bool
}

// Submitter runs the transactional submission against the database. It is the
// real boundary (TRD §7.3) and is implemented by the postgres adapter.
type Submitter interface {
	Submit(ctx context.Context, in SubmitInput) (SubmittedRequest, error)
}
