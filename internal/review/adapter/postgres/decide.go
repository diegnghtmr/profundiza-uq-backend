package postgres

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/uniquindio/profundiza-uq/internal/enrollment/domain"
	"github.com/uniquindio/profundiza-uq/internal/notification"
	"github.com/uniquindio/profundiza-uq/internal/review/app"
	"github.com/uniquindio/profundiza-uq/internal/shared/audit"
)

// uniqueViolation is the PostgreSQL SQLSTATE for a unique-constraint violation.
const uniqueViolation = "23505"

// Errors surfaced by the decision command. Adapters map these to the API error
// envelope; they must never leak as raw pg errors.
var (
	// ErrRequestNotFound is returned when the target request does not exist.
	ErrRequestNotFound = errors.New("review: enrollment request not found")
	// ErrTargetGroupNotFound is returned when a CREATE_GROUP_ACCEPTANCE names a
	// target group that does not exist.
	ErrTargetGroupNotFound = errors.New("review: target offering group not found")
	// ErrTargetGroupOfferingMismatch is returned when the target group belongs to
	// a different offering than the request; a student may not be moved across
	// offerings.
	ErrTargetGroupOfferingMismatch = errors.New("review: target offering group belongs to a different offering")
	// ErrDuplicateActiveInTargetGroup is returned when the student already holds
	// an active request in the target group (uq_active_request_per_group).
	ErrDuplicateActiveInTargetGroup = errors.New("review: student already has an active request in the target group")
)

// Decide applies an administrative decision atomically: it locks the request
// and its group, validates the decision with the enrollment domain rules
// (mandatory reason, capacity, max-accepted), updates the status, appends the
// decision and the audit event, and enqueues the student notification.
func (r *Repo) Decide(ctx context.Context, in app.DecisionInput) (app.DecisionResult, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return app.DecisionResult{}, err
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	var (
		studentID, email, groupID, offeringID string
		current                               domain.RequestStatus
	)
	err = tx.QueryRow(ctx,
		`SELECT er.student_id, s.institutional_email, er.offering_group_id, er.offering_id, er.status
		   FROM enrollment_requests er
		   JOIN students s ON s.id = er.student_id
		  WHERE er.id = $1
		  FOR UPDATE OF er`, in.RequestID,
	).Scan(&studentID, &email, &groupID, &offeringID, &current)
	if errors.Is(err, pgx.ErrNoRows) {
		return app.DecisionResult{}, ErrRequestNotFound
	}
	if err != nil {
		return app.DecisionResult{}, err
	}

	// For CREATE_GROUP_ACCEPTANCE the decision moves the student INTO a different
	// (admin-created) target group, so the capacity/accepted counts must come
	// from that target — and the target must belong to the same offering. Every
	// other decision type keeps evaluating the request's original group.
	capacityGroupID := groupID
	if in.DecisionType == domain.DecisionCreateGroupAcceptance {
		if in.TargetGroupID == "" {
			// Mirror the domain rule so the caller gets the same typed error
			// without needing the group lookup below.
			return app.DecisionResult{}, domain.ErrTargetGroupRequired
		}
		var targetOffering string
		err := tx.QueryRow(ctx,
			`SELECT offering_id FROM offering_groups WHERE id = $1 FOR UPDATE`, in.TargetGroupID,
		).Scan(&targetOffering)
		if errors.Is(err, pgx.ErrNoRows) {
			return app.DecisionResult{}, ErrTargetGroupNotFound
		}
		if err != nil {
			return app.DecisionResult{}, fmt.Errorf("lock target group: %w", err)
		}
		if targetOffering != offeringID {
			return app.DecisionResult{}, ErrTargetGroupOfferingMismatch
		}
		capacityGroupID = in.TargetGroupID
	}

	// Lock the capacity-relevant group and read its capacity.
	var capacity int
	if err := tx.QueryRow(ctx,
		`SELECT capacity FROM offering_groups WHERE id = $1 FOR UPDATE`, capacityGroupID,
	).Scan(&capacity); err != nil {
		return app.DecisionResult{}, fmt.Errorf("lock group: %w", err)
	}

	var groupAccepted, studentAccepted int
	if err := tx.QueryRow(ctx,
		`SELECT count(*) FROM enrollment_requests WHERE offering_group_id=$1 AND status='ACCEPTED'`, capacityGroupID,
	).Scan(&groupAccepted); err != nil {
		return app.DecisionResult{}, err
	}
	if err := tx.QueryRow(ctx,
		`SELECT count(*) FROM enrollment_requests er
		   JOIN (SELECT semester_id FROM enrollment_requests WHERE id=$1) t ON true
		  WHERE er.student_id=$2 AND er.semester_id=t.semester_id AND er.status='ACCEPTED'`,
		in.RequestID, studentID,
	).Scan(&studentAccepted); err != nil {
		return app.DecisionResult{}, err
	}

	newStatus, err := domain.ApplyDecision(in.DecisionType, domain.DecisionContext{
		CurrentStatus:             current,
		Reason:                    in.Reason,
		GroupCapacity:             capacity,
		GroupAcceptedCount:        groupAccepted,
		StudentAcceptedInSemester: studentAccepted,
		TargetGroupID:             in.TargetGroupID,
	})
	if err != nil {
		return app.DecisionResult{}, err
	}

	// CREATE_GROUP_ACCEPTANCE reassigns the request to the target group in the
	// same UPDATE; every other decision only changes the status. The target
	// reassignment can collide with uq_active_request_per_group if the student
	// already holds an active request in the target group — map that to a typed
	// error instead of leaking the raw unique violation.
	if in.DecisionType == domain.DecisionCreateGroupAcceptance {
		if _, err := tx.Exec(ctx,
			`UPDATE enrollment_requests SET status=$2, offering_group_id=$3, updated_at=now() WHERE id=$1`,
			in.RequestID, string(newStatus), in.TargetGroupID,
		); err != nil {
			var pgErr *pgconn.PgError
			if errors.As(err, &pgErr) && pgErr.Code == uniqueViolation && pgErr.ConstraintName == "uq_active_request_per_group" {
				return app.DecisionResult{}, ErrDuplicateActiveInTargetGroup
			}
			return app.DecisionResult{}, fmt.Errorf("reassign and update status: %w", err)
		}
	} else if _, err := tx.Exec(ctx,
		`UPDATE enrollment_requests SET status=$2, updated_at=now() WHERE id=$1`, in.RequestID, string(newStatus),
	); err != nil {
		return app.DecisionResult{}, fmt.Errorf("update status: %w", err)
	}

	var dec app.Decision
	if err := tx.QueryRow(ctx,
		`INSERT INTO enrollment_decisions
		   (enrollment_request_id, admin_user_id, decision_type, previous_status, new_status, reason)
		 VALUES ($1,$2,$3,$4,$5,$6)
		 RETURNING id, enrollment_request_id, admin_user_id, decision_type, previous_status, new_status, reason, created_at`,
		in.RequestID, in.AdminUserID, string(in.DecisionType), string(current), string(newStatus), in.Reason,
	).Scan(&dec.ID, &dec.EnrollmentRequestID, &dec.AdminUserID, &dec.DecisionType, &dec.PreviousStatus,
		&dec.NewStatus, &dec.Reason, &dec.CreatedAt); err != nil {
		return app.DecisionResult{}, fmt.Errorf("insert decision: %w", err)
	}

	if err := audit.Write(ctx, tx, audit.Event{
		ActorType:     audit.ActorAdmin,
		ActorID:       in.AdminUserID,
		Action:        "ENROLLMENT_DECISION_" + string(in.DecisionType),
		EntityType:    "EnrollmentRequest",
		EntityID:      in.RequestID,
		PreviousValue: map[string]any{"status": current},
		NewValue:      map[string]any{"status": newStatus},
		Reason:        in.Reason,
	}); err != nil {
		return app.DecisionResult{}, err
	}

	if nType, subject, body := notifyFor(in.DecisionType, in.Reason); nType != "" {
		if err := notification.Enqueue(ctx, tx, notification.Message{
			RecipientUserID:   studentID,
			RecipientEmail:    email,
			Type:              nType,
			Subject:           subject,
			Body:              body,
			RelatedEntityType: "EnrollmentRequest",
			RelatedEntityID:   in.RequestID,
		}); err != nil {
			return app.DecisionResult{}, err
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return app.DecisionResult{}, err
	}

	req, err := r.getRequestRow(ctx, in.RequestID)
	if err != nil {
		return app.DecisionResult{}, err
	}
	return app.DecisionResult{Request: req, Decision: dec}, nil
}

func notifyFor(dt domain.DecisionType, reason string) (nType, subject, body string) {
	switch dt {
	case domain.DecisionAccept, domain.DecisionCreateGroupAcceptance, domain.DecisionCapacityAdjustmentAcceptance:
		return "REQUEST_ACCEPTED", "Your enrollment request was accepted", "Your enrollment request has been accepted. Reason: " + reason
	case domain.DecisionReject:
		return "REQUEST_REJECTED", "Your enrollment request was rejected", "Your enrollment request was rejected. Reason: " + reason
	case domain.DecisionAdminCancel:
		return "REQUEST_CANCELLED_BY_ADMIN", "Your enrollment request was cancelled", "Your enrollment request was cancelled by an administrator. Reason: " + reason
	default:
		return "", "", "" // MOVE_TO_REVIEW does not notify the student
	}
}

// getRequestRow re-reads a request as an app.RequestRow after the decision.
func (r *Repo) getRequestRow(ctx context.Context, id string) (app.RequestRow, error) {
	var rr app.RequestRow
	err := r.pool.QueryRow(ctx,
		`SELECT r.id, r.semester_id, r.student_id, r.offering_id, r.offering_group_id, r.enrollment_window_id,
		        r.student_shift, r.offering_shift, r.priority_group, r.status, r.arrival_sequence, r.submitted_at, r.cancelled_at,
		        (SELECT d.reason FROM enrollment_decisions d WHERE d.enrollment_request_id=r.id ORDER BY d.created_at DESC LIMIT 1)
		   FROM enrollment_requests r WHERE r.id=$1`, id,
	).Scan(&rr.ID, &rr.SemesterID, &rr.StudentID, &rr.OfferingID, &rr.OfferingGroupID, &rr.EnrollmentWindowID,
		&rr.StudentShift, &rr.OfferingShift, &rr.PriorityGroup, &rr.Status, &rr.ArrivalSequence,
		&rr.SubmittedAt, &rr.CancelledAt, &rr.LatestReason)
	return rr, err
}
