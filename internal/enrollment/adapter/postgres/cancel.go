package postgres

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"

	"github.com/uniquindio/profundiza-uq/internal/enrollment/app"
	"github.com/uniquindio/profundiza-uq/internal/enrollment/domain"
	"github.com/uniquindio/profundiza-uq/internal/notification"
	"github.com/uniquindio/profundiza-uq/internal/shared/audit"
)

// Cancel cancels a student's own request. The request must belong to
// the student and be in an active, non-terminal state. The cancellation, audit
// event and notification commit atomically.
func (r *SubmitRepo) Cancel(ctx context.Context, in app.CancelInput) (app.RequestView, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return app.RequestView{}, err
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	var (
		studentID string
		status    domain.RequestStatus
		email     string
	)
	err = tx.QueryRow(ctx,
		`SELECT er.student_id, er.status, s.institutional_email
		   FROM enrollment_requests er
		   JOIN students s ON s.id = er.student_id
		  WHERE er.id = $1
		  FOR UPDATE OF er`, in.RequestID,
	).Scan(&studentID, &status, &email)
	if errors.Is(err, pgx.ErrNoRows) {
		return app.RequestView{}, app.ErrNotFound
	}
	if err != nil {
		return app.RequestView{}, err
	}
	if studentID != in.StudentID {
		return app.RequestView{}, app.ErrForbidden
	}
	if status.IsTerminal() {
		return app.RequestView{}, app.ErrNotCancelable
	}

	if _, err := tx.Exec(ctx,
		`UPDATE enrollment_requests
		    SET status = 'CANCELLED_BY_STUDENT', cancelled_at = now(), updated_at = now()
		  WHERE id = $1`, in.RequestID); err != nil {
		return app.RequestView{}, fmt.Errorf("cancel update: %w", err)
	}

	if err := audit.Write(ctx, tx, audit.Event{
		ActorType:     audit.ActorStudent,
		ActorID:       in.StudentID,
		Action:        "ENROLLMENT_CANCELLED_BY_STUDENT",
		EntityType:    "EnrollmentRequest",
		EntityID:      in.RequestID,
		PreviousValue: map[string]any{"status": status},
		NewValue:      map[string]any{"status": domain.StatusCancelledByStudent},
	}); err != nil {
		return app.RequestView{}, err
	}
	if err := notification.Enqueue(ctx, tx, notification.Message{
		RecipientUserID:   in.StudentID,
		RecipientEmail:    email,
		Type:              "REQUEST_CANCELLED_BY_STUDENT",
		Subject:           "Your enrollment request was cancelled",
		Body:              "You cancelled your enrollment request. Your previous position is no longer reserved.",
		RelatedEntityType: "EnrollmentRequest",
		RelatedEntityID:   in.RequestID,
	}); err != nil {
		return app.RequestView{}, err
	}

	if err := tx.Commit(ctx); err != nil {
		return app.RequestView{}, err
	}

	rv, err := r.Get(ctx, in.RequestID)
	if err != nil {
		return app.RequestView{}, err
	}
	return *rv, nil
}
