// Package domain implements the pure business rules of the enrollment context:
// priority classification, the maximum-electives rule, and administrative
// decision validation. It depends only on the standard library and the shared
// value objects — never on HTTP, PostgreSQL, or generated code.
package domain

// PriorityGroup is the bucket that determines the administrative attention order
// of a request. DirectSameShift is served first, then WaitlistSameShift, then
// WaitlistOppositeShift. Within a bucket, lower arrivalSequence wins.
type PriorityGroup string

const (
	PriorityDirectSameShift      PriorityGroup = "DIRECT_SAME_SHIFT"
	PriorityWaitlistSameShift    PriorityGroup = "WAITLIST_SAME_SHIFT"
	PriorityWaitlistOppositeShift PriorityGroup = "WAITLIST_OPPOSITE_SHIFT"
)

// RequestStatus is the lifecycle state of an enrollment request.
type RequestStatus string

const (
	StatusSubmitted             RequestStatus = "SUBMITTED"
	StatusPendingReview         RequestStatus = "PENDING_REVIEW"
	StatusWaitlistSameShift     RequestStatus = "WAITLIST_SAME_SHIFT"
	StatusWaitlistOppositeShift RequestStatus = "WAITLIST_OPPOSITE_SHIFT"
	StatusAccepted              RequestStatus = "ACCEPTED"
	StatusRejected              RequestStatus = "REJECTED"
	StatusCancelledByStudent    RequestStatus = "CANCELLED_BY_STUDENT"
	StatusCancelledByAdmin      RequestStatus = "CANCELLED_BY_ADMIN"
)

// IsActive reports whether a request still occupies a position (counts toward
// the per-semester limit and the partial-unique active constraint). Cancelled
// and rejected requests are historical and no longer active.
func (s RequestStatus) IsActive() bool {
	switch s {
	case StatusSubmitted, StatusPendingReview, StatusWaitlistSameShift,
		StatusWaitlistOppositeShift, StatusAccepted:
		return true
	default:
		return false
	}
}

// IsTerminal reports whether the request reached a final state.
func (s RequestStatus) IsTerminal() bool {
	switch s {
	case StatusAccepted, StatusRejected, StatusCancelledByStudent, StatusCancelledByAdmin:
		return true
	default:
		return false
	}
}

// IsCancellableByAdmin reports whether an admin-cancel decision is permitted on
// this request. Active states and ACCEPTED are all cancellable: an admin may
// need to free a seat from an already-accepted request (e.g. the student drops
// the semester) without manual DB edits.
//
// Only already-cancelled or rejected requests are immutable — they have no seat
// to release and reverting them would silently lose audit history.
//
// Seat occupancy is computed as count(*) FILTER (WHERE status='ACCEPTED'), so
// flipping an ACCEPTED request to CANCELLED_BY_ADMIN automatically decrements
// the group's accepted count. There is no materialized counter that requires a
// separate update.
func (s RequestStatus) IsCancellableByAdmin() bool {
	switch s {
	case StatusRejected, StatusCancelledByStudent, StatusCancelledByAdmin:
		return false
	default:
		return true
	}
}
