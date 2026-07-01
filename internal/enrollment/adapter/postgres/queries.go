package postgres

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/uniquindio/profundiza-uq/internal/enrollment/app"
)

// requestColumns is the projection shared by list/get, including the latest
// decision reason for the request.
const requestColumns = `
	r.id, r.semester_id, r.student_id, r.offering_id, r.offering_group_id, r.enrollment_window_id,
	r.student_shift, r.offering_shift, r.priority_group, r.status, r.arrival_sequence,
	r.submitted_at, r.cancelled_at,
	(SELECT d.reason FROM enrollment_decisions d WHERE d.enrollment_request_id = r.id ORDER BY d.created_at DESC LIMIT 1)`

type scannable interface{ Scan(dest ...any) error }

func scanRequest(row scannable) (app.RequestView, error) {
	var (
		rv          app.RequestView
		submittedAt time.Time
		cancelledAt *time.Time
	)
	if err := row.Scan(
		&rv.ID, &rv.SemesterID, &rv.StudentID, &rv.OfferingID, &rv.OfferingGroupID, &rv.EnrollmentWindowID,
		&rv.StudentShift, &rv.OfferingShift, &rv.PriorityGroup, &rv.Status, &rv.ArrivalSequence,
		&submittedAt, &cancelledAt, &rv.LatestReason,
	); err != nil {
		return app.RequestView{}, err
	}
	rv.SubmittedAt = submittedAt.UTC().Format(time.RFC3339)
	if cancelledAt != nil {
		s := cancelledAt.UTC().Format(time.RFC3339)
		rv.CancelledAt = &s
	}
	return rv, nil
}

// ListMine returns the student's requests for a semester, newest arrival first.
func (r *SubmitRepo) ListMine(ctx context.Context, studentID, semesterID string) ([]app.RequestView, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT `+requestColumns+`
		   FROM enrollment_requests r
		  WHERE r.student_id = $1 AND r.semester_id = $2
		  ORDER BY r.arrival_sequence DESC`, studentID, semesterID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []app.RequestView{}
	for rows.Next() {
		rv, err := scanRequest(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, rv)
	}
	return out, rows.Err()
}

// Get returns a single request by id, or nil when absent.
func (r *SubmitRepo) Get(ctx context.Context, requestID string) (*app.RequestView, error) {
	rv, err := scanRequest(r.pool.QueryRow(ctx,
		`SELECT `+requestColumns+` FROM enrollment_requests r WHERE r.id = $1`, requestID))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &rv, nil
}
