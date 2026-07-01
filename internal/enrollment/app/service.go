package app

import (
	"context"
	"errors"

	"github.com/uniquindio/profundiza-uq/internal/enrollment/domain"
	shared "github.com/uniquindio/profundiza-uq/internal/shared/domain"
)

// Errors surfaced by the enrollment use cases.
var (
	ErrNotFound          = errors.New("enrollment: request not found")
	ErrForbidden         = errors.New("enrollment: request does not belong to the student")
	ErrNotCancelable     = errors.New("enrollment: request is not in a cancelable state")
	ErrDuplicateBatchItem = errors.New("enrollment: batch contains duplicate offering group IDs")
	// ErrDuplicateActiveRequest is returned when a student submits an offering
	// group they already hold an active request for. It is enforced by the
	// uq_active_request_per_group partial index and pre-checked in SubmitBatch.
	ErrDuplicateActiveRequest = errors.New("enrollment: student already has an active request for this offering group")
)

// CancelInput cancels one of the student's own requests.
type CancelInput struct {
	RequestID string
	StudentID string
}

// RequestView is a wire-ready projection of an enrollment request.
type RequestView struct {
	ID                 string
	SemesterID         string
	StudentID          string
	OfferingID         string
	OfferingGroupID    string
	EnrollmentWindowID *string
	StudentShift       shared.AcademicShift
	OfferingShift      shared.AcademicShift
	PriorityGroup      domain.PriorityGroup
	Status             domain.RequestStatus
	ArrivalSequence    int64
	SubmittedAt        string
	CancelledAt        *string
	LatestReason       *string
}

// Store is the enrollment persistence boundary.
type Store interface {
	Submit(ctx context.Context, in SubmitInput) (SubmittedRequest, error)
	Cancel(ctx context.Context, in CancelInput) (RequestView, error)
	ListMine(ctx context.Context, studentID, semesterID string) ([]RequestView, error)
	Get(ctx context.Context, requestID string) (*RequestView, error)
}

// EnrollmentService exposes enrollment use cases to driving adapters.
type EnrollmentService struct {
	store Store
}

// NewEnrollmentService wires the service with its store.
func NewEnrollmentService(store Store) *EnrollmentService {
	return &EnrollmentService{store: store}
}

// Submit enrolls a student into a group. Locking, counting, classifying and
// inserting happen atomically inside the store.
func (s *EnrollmentService) Submit(ctx context.Context, in SubmitInput) (SubmittedRequest, error) {
	return s.store.Submit(ctx, in)
}

// SubmitBatch submits up to four requests, returning the outcome of each.
//
// Fix #1 — two pre-validations run before any store.Submit call to prevent
// partial commits that leave the student in an unknown state:
//
// (a) Duplicate group IDs are rejected immediately. Sending the same group
// twice is always a client error and could never partially succeed.
//
// (b) The student's current active count is read once via ListMine; if adding
// all batch items would exceed MaxElectivesPerSemester the whole batch is
// rejected with ErrMaxElectivesReached before any row is inserted. This makes
// the common failure case deterministic and all-or-nothing.
//
// Individual items are still committed in their own transactions,
// so uncommon per-item failures (ErrWindowClosed, etc.) may still produce
// partial commits. The pre-check eliminates partial commits for the cap-breach
// case, which is the most confusing one for students.
func (s *EnrollmentService) SubmitBatch(ctx context.Context, semesterID, studentID, baseIdemKey string, groupIDs []string) ([]SubmittedRequest, error) {
	// (a) Reject duplicate group IDs.
	seen := make(map[string]struct{}, len(groupIDs))
	for _, gid := range groupIDs {
		if _, dup := seen[gid]; dup {
			return nil, ErrDuplicateBatchItem
		}
		seen[gid] = struct{}{}
	}

	// (b) Pre-validate against the per-semester cap. Read the student's
	// existing active requests and fail the whole batch now — before committing
	// anything — when adding all items would exceed the limit.
	views, err := s.store.ListMine(ctx, studentID, semesterID)
	if err != nil {
		return nil, err
	}
	activeCount := 0
	activeGroups := make(map[string]struct{}, len(views))
	for _, v := range views {
		if v.Status.IsActive() {
			activeCount++
			activeGroups[v.OfferingGroupID] = struct{}{}
		}
	}
	// Reject re-requests of an already-active group up front, so the batch is
	// all-or-nothing (no partial commit) and the DB unique index never surfaces
	// as an opaque error.
	for _, gid := range groupIDs {
		if _, dup := activeGroups[gid]; dup {
			return nil, ErrDuplicateActiveRequest
		}
	}
	if activeCount+len(groupIDs) > domain.MaxElectivesPerSemester {
		return nil, domain.ErrMaxElectivesReached
	}

	out := make([]SubmittedRequest, 0, len(groupIDs))
	for i, gid := range groupIDs {
		res, err := s.store.Submit(ctx, SubmitInput{
			SemesterID:      semesterID,
			StudentID:       studentID,
			OfferingGroupID: gid,
			IdempotencyKey:  baseIdemKey + ":" + itoa(i),
		})
		if err != nil {
			return out, err
		}
		out = append(out, res)
	}
	return out, nil
}

// Cancel cancels the student's own request (the position is lost).
func (s *EnrollmentService) Cancel(ctx context.Context, in CancelInput) (RequestView, error) {
	return s.store.Cancel(ctx, in)
}

// ListMine returns the student's requests for a semester.
func (s *EnrollmentService) ListMine(ctx context.Context, studentID, semesterID string) ([]RequestView, error) {
	return s.store.ListMine(ctx, studentID, semesterID)
}

// Get returns a single request, enforcing object-level authorization: a student
// may only read their own request; admins may read any.
func (s *EnrollmentService) Get(ctx context.Context, requestID, studentID string, isAdmin bool) (*RequestView, error) {
	rv, err := s.store.Get(ctx, requestID)
	if err != nil {
		return nil, err
	}
	if rv == nil {
		return nil, ErrNotFound
	}
	if !isAdmin && rv.StudentID != studentID {
		return nil, ErrForbidden
	}
	return rv, nil
}

func itoa(i int) string {
	const digits = "0123456789"
	if i == 0 {
		return "0"
	}
	var b []byte
	for i > 0 {
		b = append([]byte{digits[i%10]}, b...)
		i /= 10
	}
	return string(b)
}
