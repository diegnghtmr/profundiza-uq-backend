package domain

import (
	"errors"
	"strings"
)

// MaxElectivesPerSemester is the hard cap on professional electives a student
// may hold or be accepted into within a single semester.
const MaxElectivesPerSemester = 4

// minReasonLength mirrors the OpenAPI contract (reason minLength: 3).
const minReasonLength = 3

// Domain errors. Adapters map these to the API error envelope codes.
var (
	ErrMaxElectivesReached = errors.New("enrollment: student already holds the maximum professional electives for the semester")
	ErrReasonRequired      = errors.New("enrollment: a non-empty reason is required for this decision")
	ErrCapacityExceeded    = errors.New("enrollment: accepting this request would exceed the group capacity")
	ErrUnknownDecision     = errors.New("enrollment: unknown decision type")
	ErrAlreadyTerminal     = errors.New("enrollment: request is already in a terminal state")
	ErrTargetGroupRequired = errors.New("enrollment: a target group is required for a create-group acceptance")
)

// DecisionType is an administrative action on a request.
type DecisionType string

const (
	DecisionAccept                    DecisionType = "ACCEPT"
	DecisionReject                    DecisionType = "REJECT"
	DecisionAdminCancel               DecisionType = "ADMIN_CANCEL"
	DecisionMoveToReview              DecisionType = "MOVE_TO_REVIEW"
	DecisionCreateGroupAcceptance     DecisionType = "CREATE_GROUP_ACCEPTANCE"
	DecisionCapacityAdjustmentAcceptance DecisionType = "CAPACITY_ADJUSTMENT_ACCEPTANCE"
)

// ValidateSubmissionAllowed enforces the per-semester cap: a student may not exceed the
// per-semester limit of active requests. activeCount is the number of the
// student's currently active requests in the semester (cancelled/rejected
// excluded).
func ValidateSubmissionAllowed(activeCount int) error {
	if activeCount >= MaxElectivesPerSemester {
		return ErrMaxElectivesReached
	}
	return nil
}

// reasonIsValid trims whitespace and enforces the minimum length.
func reasonIsValid(reason string) bool {
	return len(strings.TrimSpace(reason)) >= minReasonLength
}

// DecisionContext carries the facts a decision needs to be validated against,
// gathered inside the decision transaction.
type DecisionContext struct {
	CurrentStatus             RequestStatus
	Reason                    string
	GroupCapacity             int
	GroupAcceptedCount        int
	StudentAcceptedInSemester int
	// TargetGroupID is the group the student is accepted INTO. It is only
	// meaningful for CREATE_GROUP_ACCEPTANCE, where the admin moves a
	// waitlisted student into a different (newly created) group; for that
	// decision type GroupCapacity/GroupAcceptedCount describe the target group.
	// It is required for CREATE_GROUP_ACCEPTANCE and ignored otherwise.
	TargetGroupID string
}

// ApplyDecision validates an administrative decision against the domain rules
// and returns the resulting request status. It enforces the mandatory
// reason, the no-overbooking rule and the maximum-accepted rule.
//
// Fix #6 — ADMIN_CANCEL on ACCEPTED: DecisionAdminCancel is evaluated before
// the general IsTerminal guard so that an admin can free a seat from an
// already-accepted request. Only settled (rejected/cancelled) requests are
// blocked; see IsCancellableByAdmin for the exact set.
//
// Fix #5 — capacity exemption removed: All acceptance paths now enforce
// GroupAcceptedCount < GroupCapacity. For CAPACITY_ADJUSTMENT_ACCEPTANCE the
// capacity-adjustment endpoint (POST /offering-groups/{id}/capacity-adjustments)
// must raise the group's capacity BEFORE this decision is recorded; if the count
// is still at the old capacity the adjustment was not made and the decision
// fails with ErrCapacityExceeded. The same invariant applies to
// CREATE_GROUP_ACCEPTANCE: the newly created group has its own capacity and the
// target group must have room.
func ApplyDecision(dt DecisionType, ctx DecisionContext) (RequestStatus, error) {
	if !reasonIsValid(ctx.Reason) {
		return "", ErrReasonRequired
	}

	// Fix #6: handle ADMIN_CANCEL before the IsTerminal guard so admins can
	// cancel ACCEPTED requests and free the associated seat. Already-cancelled
	// or rejected requests remain immutable (IsCancellableByAdmin returns false).
	if dt == DecisionAdminCancel {
		if !ctx.CurrentStatus.IsCancellableByAdmin() {
			return "", ErrAlreadyTerminal
		}
		return StatusCancelledByAdmin, nil
	}

	// All other decisions are blocked on any terminal state.
	if ctx.CurrentStatus.IsTerminal() {
		return "", ErrAlreadyTerminal
	}

	switch dt {
	case DecisionAccept, DecisionCreateGroupAcceptance, DecisionCapacityAdjustmentAcceptance:
		// CREATE_GROUP_ACCEPTANCE moves the student INTO a distinct target group,
		// so a target must be named. GroupCapacity/GroupAcceptedCount are then
		// supplied from that target group by the persistence adapter.
		if dt == DecisionCreateGroupAcceptance && strings.TrimSpace(ctx.TargetGroupID) == "" {
			return "", ErrTargetGroupRequired
		}
		if ctx.StudentAcceptedInSemester >= MaxElectivesPerSemester {
			return "", ErrMaxElectivesReached
		}
		// Fix #5: capacity check applies to every acceptance path. For
		// CAPACITY_ADJUSTMENT_ACCEPTANCE and CREATE_GROUP_ACCEPTANCE the admin
		// must have already made room (by adjusting or creating the group) before
		// recording this decision. If GroupAcceptedCount is still >= GroupCapacity
		// the prerequisite action was not completed and we reject.
		if ctx.GroupAcceptedCount >= ctx.GroupCapacity {
			return "", ErrCapacityExceeded
		}
		return StatusAccepted, nil
	case DecisionReject:
		return StatusRejected, nil
	case DecisionMoveToReview:
		return StatusPendingReview, nil
	default:
		return "", ErrUnknownDecision
	}
}
