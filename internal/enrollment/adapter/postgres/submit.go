// Package postgres implements the enrollment Submitter port. The Submit method
// is the canonical capacity transaction: it locks the target
// offering group row (SELECT ... FOR UPDATE) so concurrent submissions cannot
// classify against a stale seat count, guaranteeing no overbooking of direct
// slots and a fair, server-generated arrival order.
package postgres

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/uniquindio/profundiza-uq/internal/enrollment/app"
	"github.com/uniquindio/profundiza-uq/internal/enrollment/domain"
	"github.com/uniquindio/profundiza-uq/internal/notification"
	"github.com/uniquindio/profundiza-uq/internal/shared/audit"
	shared "github.com/uniquindio/profundiza-uq/internal/shared/domain"
)

// Errors surfaced to the application layer.
var (
	ErrGroupNotFound   = errors.New("enrollment: offering group not found or closed")
	ErrStudentNotFound = errors.New("enrollment: student not found")
	ErrWindowClosed    = errors.New("enrollment: no active enrollment window")
)

// SubmitRepo runs enrollment submissions transactionally.
type SubmitRepo struct {
	pool *pgxpool.Pool
}

// NewSubmitRepo builds a SubmitRepo over a pgx pool.
func NewSubmitRepo(pool *pgxpool.Pool) *SubmitRepo {
	return &SubmitRepo{pool: pool}
}

const uniqueViolation = "23505"

// Submit performs the full submission transaction.
func (r *SubmitRepo) Submit(ctx context.Context, in app.SubmitInput) (app.SubmittedRequest, error) {
	// Idempotency fast-path: a prior request with the same key wins.
	if existing, ok, err := r.findByIdempotencyKey(ctx, in); err != nil {
		return app.SubmittedRequest{}, err
	} else if ok {
		return existing, nil
	}

	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return app.SubmittedRequest{}, err
	}
	defer tx.Rollback(ctx) //nolint:errcheck // no-op after commit

	// 1. Lock the target group row and read its shift, capacity, offering.
	var (
		offeringID  string
		groupShift  shared.AcademicShift
		capacity    int
	)
	err = tx.QueryRow(ctx,
		`SELECT offering_id, shift, capacity
		   FROM offering_groups
		  WHERE id = $1 AND status = 'ACTIVE'
		  FOR UPDATE`, in.OfferingGroupID,
	).Scan(&offeringID, &groupShift, &capacity)
	if errors.Is(err, pgx.ErrNoRows) {
		return app.SubmittedRequest{}, ErrGroupNotFound
	}
	if err != nil {
		return app.SubmittedRequest{}, fmt.Errorf("lock group: %w", err)
	}

	// 2. Student shift and email (email is used for the notification).
	var (
		studentShift shared.AcademicShift
		studentEmail string
	)
	err = tx.QueryRow(ctx,
		`SELECT academic_shift, institutional_email FROM students WHERE id = $1 AND status = 'ACTIVE' FOR UPDATE`, in.StudentID,
	).Scan(&studentShift, &studentEmail)
	if errors.Is(err, pgx.ErrNoRows) {
		return app.SubmittedRequest{}, ErrStudentNotFound
	}
	if err != nil {
		return app.SubmittedRequest{}, fmt.Errorf("read student: %w", err)
	}

	// 3. Active enrollment window for the semester and the student's shift
	//    Submission is blocked outside an active window.
	var windowID string
	err = tx.QueryRow(ctx,
		`SELECT id FROM enrollment_windows
		  WHERE semester_id = $1 AND status = 'ACTIVE'
		    AND now() BETWEEN starts_at AND ends_at
		    AND (target_shift IS NULL OR target_shift = $2)
		  ORDER BY ends_at
		  LIMIT 1`, in.SemesterID, string(studentShift),
	).Scan(&windowID)
	if errors.Is(err, pgx.ErrNoRows) {
		return app.SubmittedRequest{}, ErrWindowClosed
	}
	if err != nil {
		return app.SubmittedRequest{}, fmt.Errorf("check window: %w", err)
	}

	// 4. Per-semester active-request count.
	var activeCount int
	if err := tx.QueryRow(ctx,
		`SELECT count(*) FROM enrollment_requests
		  WHERE student_id = $1 AND semester_id = $2
		    AND status IN ('SUBMITTED','PENDING_REVIEW','WAITLIST_SAME_SHIFT','WAITLIST_OPPOSITE_SHIFT','ACCEPTED')`,
		in.StudentID, in.SemesterID,
	).Scan(&activeCount); err != nil {
		return app.SubmittedRequest{}, fmt.Errorf("count active: %w", err)
	}
	if err := domain.ValidateSubmissionAllowed(activeCount); err != nil {
		return app.SubmittedRequest{}, err
	}

	// 4. Seat occupancy for this group (accepted + same-shift pending direct).
	var occ domain.GroupOccupancy
	occ.Capacity = capacity
	if err := tx.QueryRow(ctx,
		`SELECT
		   count(*) FILTER (WHERE status = 'ACCEPTED'),
		   count(*) FILTER (WHERE status = 'PENDING_REVIEW' AND priority_group = 'DIRECT_SAME_SHIFT')
		 FROM enrollment_requests
		 WHERE offering_group_id = $1`, in.OfferingGroupID,
	).Scan(&occ.AcceptedCount, &occ.PendingDirectCount); err != nil {
		return app.SubmittedRequest{}, fmt.Errorf("count occupancy: %w", err)
	}

	// 5. Classify with the locked, consistent counts (pure domain rule).
	priority, status := domain.Classify(studentShift, groupShift, occ)

	// 6. Insert the request; the DB assigns the official arrival sequence.
	var (
		id       string
		arrival  int64
	)
	err = tx.QueryRow(ctx,
		`INSERT INTO enrollment_requests
		   (semester_id, student_id, offering_id, offering_group_id, enrollment_window_id, student_shift,
		    offering_shift, priority_group, status, idempotency_key)
		 VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)
		 RETURNING id, arrival_sequence`,
		in.SemesterID, in.StudentID, offeringID, in.OfferingGroupID, windowID, studentShift,
		groupShift, priority, status, in.IdempotencyKey,
	).Scan(&id, &arrival)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == uniqueViolation {
			switch pgErr.ConstraintName {
			case "uq_active_request_per_group":
				// The student already holds an active request for this group
				// (e.g. a concurrent batch beat the app-level pre-check).
				return app.SubmittedRequest{}, app.ErrDuplicateActiveRequest
			case "uq_request_idempotency":
				// A concurrent identical idempotency key may have won the race.
				_ = tx.Rollback(ctx)
				if existing, ok, e := r.findByIdempotencyKey(ctx, in); e == nil && ok {
					return existing, nil
				}
			}
		}
		return app.SubmittedRequest{}, fmt.Errorf("insert request: %w", err)
	}

	// 7. Audit + notification, atomic with the insert.
	if err := audit.Write(ctx, tx, audit.Event{
		ActorType:  audit.ActorStudent,
		ActorID:    in.StudentID,
		Action:     "ENROLLMENT_SUBMITTED",
		EntityType: "EnrollmentRequest",
		EntityID:   id,
		NewValue:   map[string]any{"status": status, "priorityGroup": priority, "offeringGroupId": in.OfferingGroupID},
	}); err != nil {
		return app.SubmittedRequest{}, fmt.Errorf("audit submit: %w", err)
	}
	if err := notification.Enqueue(ctx, tx, notification.Message{
		RecipientUserID:   in.StudentID,
		RecipientEmail:    studentEmail,
		Type:              "REQUEST_SUBMITTED",
		Subject:           "We received your enrollment request",
		Body:              "Your enrollment request was received and is now " + humanStatus(status) + ".",
		RelatedEntityType: "EnrollmentRequest",
		RelatedEntityID:   id,
	}); err != nil {
		return app.SubmittedRequest{}, fmt.Errorf("enqueue notification: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return app.SubmittedRequest{}, fmt.Errorf("commit: %w", err)
	}

	return app.SubmittedRequest{
		ID:              id,
		OfferingID:      offeringID,
		Status:          status,
		PriorityGroup:   priority,
		ArrivalSequence: arrival,
	}, nil
}

// humanStatus renders a submission status as user-facing prose for the
// confirmation notification, so the email/alert never leaks a raw enum name
// (e.g. "WAITLIST_SAME_SHIFT") to the student.
func humanStatus(s domain.RequestStatus) string {
	switch s {
	case domain.StatusPendingReview:
		return "pending review"
	case domain.StatusWaitlistSameShift, domain.StatusWaitlistOppositeShift:
		return "on the waitlist"
	default:
		return strings.ToLower(strings.ReplaceAll(string(s), "_", " "))
	}
}

func (r *SubmitRepo) findByIdempotencyKey(ctx context.Context, in app.SubmitInput) (app.SubmittedRequest, bool, error) {
	var out app.SubmittedRequest
	err := r.pool.QueryRow(ctx,
		`SELECT id, offering_id, status, priority_group, arrival_sequence
		   FROM enrollment_requests
		  WHERE student_id = $1 AND semester_id = $2 AND idempotency_key = $3`,
		in.StudentID, in.SemesterID, in.IdempotencyKey,
	).Scan(&out.ID, &out.OfferingID, &out.Status, &out.PriorityGroup, &out.ArrivalSequence)
	if errors.Is(err, pgx.ErrNoRows) {
		return app.SubmittedRequest{}, false, nil
	}
	if err != nil {
		return app.SubmittedRequest{}, false, err
	}
	out.Existed = true
	return out, true, nil
}
