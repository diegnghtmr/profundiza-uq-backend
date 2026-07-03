// Package postgres implements the review Repository: the prioritized queue and
// the transactional decision command.
package postgres

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/uniquindio/profundiza-uq/internal/enrollment/domain"
	"github.com/uniquindio/profundiza-uq/internal/review/app"
)

// Repo is the pgx-backed review repository.
type Repo struct{ pool *pgxpool.Pool }

// NewRepo builds a review Repo.
func NewRepo(pool *pgxpool.Pool) *Repo { return &Repo{pool: pool} }

// reviewableStatuses are shown by default (the pending workload): anything not
// yet finalized into accepted/rejected/cancelled.
const reviewableStatuses = `('SUBMITTED','PENDING_REVIEW','WAITLIST_SAME_SHIFT','WAITLIST_OPPOSITE_SHIFT')`

// priorityRank orders DIRECT_SAME_SHIFT < WAITLIST_SAME_SHIFT < WAITLIST_OPPOSITE_SHIFT.
const priorityRank = `CASE r.priority_group
	WHEN 'DIRECT_SAME_SHIFT' THEN 0
	WHEN 'WAITLIST_SAME_SHIFT' THEN 1
	ELSE 2 END`

// groupCounts is a correlated set of per-group occupancy counts.
const groupCounts = `(
	SELECT
	  count(*) FILTER (WHERE status='ACCEPTED'),
	  count(*) FILTER (WHERE status='PENDING_REVIEW' AND priority_group='DIRECT_SAME_SHIFT'),
	  count(*) FILTER (WHERE status='WAITLIST_SAME_SHIFT'),
	  count(*) FILTER (WHERE status='WAITLIST_OPPOSITE_SHIFT')
	FROM enrollment_requests er WHERE er.offering_group_id = g.id)`

// ListQueue returns the prioritized review queue with a total count.
func (r *Repo) ListQueue(ctx context.Context, f app.QueueFilter) ([]app.QueueItem, int, error) {
	// Build the dynamic WHERE clause and arg list.
	args := []any{f.SemesterID}
	where := "r.semester_id = $1"
	add := func(cond string, val any) {
		args = append(args, val)
		where += fmt.Sprintf(" AND %s$%d", cond, len(args))
	}
	if f.OfferingID != "" {
		add("r.offering_id = ", f.OfferingID)
	}
	if f.GroupID != "" {
		add("r.offering_group_id = ", f.GroupID)
	}
	if f.PriorityGroup != "" {
		add("r.priority_group = ", f.PriorityGroup)
	}
	if f.Status != "" {
		add("r.status = ", f.Status)
	} else {
		where += " AND r.status IN " + reviewableStatuses
	}

	var total int
	if err := r.pool.QueryRow(ctx,
		`SELECT count(*) FROM enrollment_requests r WHERE `+where, args...,
	).Scan(&total); err != nil {
		return nil, 0, err
	}

	limitArg := len(args) + 1
	offsetArg := len(args) + 2
	args = append(args, f.PageSize, (f.Page-1)*f.PageSize)

	query := `
SELECT
  r.id, r.semester_id, r.student_id, r.offering_id, r.offering_group_id, r.enrollment_window_id,
  r.student_shift, r.offering_shift, r.priority_group, r.status, r.arrival_sequence, r.submitted_at, r.cancelled_at,
  (SELECT d.reason FROM enrollment_decisions d WHERE d.enrollment_request_id=r.id ORDER BY d.created_at DESC LIMIT 1),
  s.id, s.institutional_email, s.document_number, s.full_name, s.academic_shift, s.status,
  s.completed_professional_electives_count, s.created_at, s.updated_at,
  e.id, e.name, e.area, e.description, e.status,
  g.id, g.group_code, g.shift, g.teacher_name, g.schedule_text, g.capacity, g.status, g.created_at, g.updated_at,
  c.accepted, c.pending_direct, c.wl_same, c.wl_opp,
  (SELECT count(*) FROM enrollment_requests r2
     WHERE r2.student_id=r.student_id AND r2.semester_id=r.semester_id AND r2.status='ACCEPTED') AS student_accepted
FROM enrollment_requests r
JOIN students s ON s.id = r.student_id
JOIN elective_offerings o ON o.id = r.offering_id
JOIN electives e ON e.id = o.elective_id
JOIN offering_groups g ON g.id = r.offering_group_id
LEFT JOIN LATERAL ` + groupCounts + ` AS c(accepted, pending_direct, wl_same, wl_opp) ON true
WHERE ` + where + `
ORDER BY ` + priorityRank + `, r.arrival_sequence
LIMIT $` + itoa(limitArg) + ` OFFSET $` + itoa(offsetArg)

	rows, err := r.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	items := []app.QueueItem{}
	for rows.Next() {
		var (
			it             app.QueueItem
			studentAccepted int
		)
		if err := rows.Scan(
			&it.Request.ID, &it.Request.SemesterID, &it.Request.StudentID, &it.Request.OfferingID,
			&it.Request.OfferingGroupID, &it.Request.EnrollmentWindowID, &it.Request.StudentShift,
			&it.Request.OfferingShift, &it.Request.PriorityGroup, &it.Request.Status, &it.Request.ArrivalSequence,
			&it.Request.SubmittedAt, &it.Request.CancelledAt, &it.Request.LatestReason,
			&it.Student.ID, &it.Student.Email, &it.Student.DocumentNumber, &it.Student.FullName, &it.Student.Shift,
			&it.Student.Status, &it.Student.CompletedCount, &it.Student.CreatedAt, &it.Student.UpdatedAt,
			&it.Elective.ID, &it.Elective.Name, &it.Elective.Area, &it.Elective.Description, &it.Elective.Status,
			&it.Group.ID, &it.Group.GroupCode, &it.Group.Shift, &it.Group.TeacherName, &it.Group.ScheduleText,
			&it.Group.Capacity, &it.Group.Status, &it.Group.CreatedAt, &it.Group.UpdatedAt,
			&it.Group.AcceptedCount, &it.Group.PendingDirectCount, &it.Group.WaitlistSameShiftCount,
			&it.Group.WaitlistOppositeShiftCount, &studentAccepted,
		); err != nil {
			return nil, 0, err
		}
		it.Group.OfferingID = it.Request.OfferingID
		it.Warnings = warningsFor(it, studentAccepted)
		items = append(items, it)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, err
	}

	// Attach every item's offering group list. The queue join above only knows
	// each request's OWN group; the admin UI also needs the offering's SIBLING
	// groups (e.g. to target a different group in CREATE_GROUP_ACCEPTANCE). Load
	// them in ONE batched query keyed by the distinct offering IDs on this page
	// (never one query per request — that would be N+1).
	if err := r.attachOfferingGroups(ctx, items); err != nil {
		return nil, 0, err
	}
	return items, total, nil
}

// attachOfferingGroups batch-loads the ACTIVE groups of every offering present in
// the queue page and assigns each item's full offering group list, scoped per
// offering. A single query over the distinct offering IDs avoids the N+1 that a
// per-request lookup would introduce.
func (r *Repo) attachOfferingGroups(ctx context.Context, items []app.QueueItem) error {
	if len(items) == 0 {
		return nil
	}

	// Collect the distinct offering IDs on this page.
	seen := map[string]struct{}{}
	offeringIDs := make([]string, 0, len(items))
	for _, it := range items {
		id := it.Request.OfferingID
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		offeringIDs = append(offeringIDs, id)
	}

	// ONE query: all active groups for every offering on the page, with each
	// group's live accepted count. `ANY($1)` keeps it a single round-trip.
	rows, err := r.pool.Query(ctx, `
SELECT
  g.id, g.offering_id, g.group_code, g.shift, g.schedule_text, g.capacity, g.status,
  (SELECT count(*) FROM enrollment_requests er
     WHERE er.offering_group_id = g.id AND er.status = 'ACCEPTED') AS accepted
FROM offering_groups g
WHERE g.offering_id = ANY($1) AND g.status = 'ACTIVE'
ORDER BY g.offering_id, g.group_code`, offeringIDs)
	if err != nil {
		return err
	}
	defer rows.Close()

	byOffering := map[string][]app.Group{}
	for rows.Next() {
		var g app.Group
		if err := rows.Scan(
			&g.ID, &g.OfferingID, &g.GroupCode, &g.Shift, &g.ScheduleText,
			&g.Capacity, &g.Status, &g.AcceptedCount,
		); err != nil {
			return err
		}
		byOffering[g.OfferingID] = append(byOffering[g.OfferingID], g)
	}
	if err := rows.Err(); err != nil {
		return err
	}

	for i := range items {
		items[i].OfferingGroups = byOffering[items[i].Request.OfferingID]
	}
	return nil
}

// warningsFor surfaces administrative warnings for a queue item.
func warningsFor(it app.QueueItem, studentAccepted int) []string {
	var w []string
	if it.Student.CompletedCount+studentAccepted >= domain.MaxElectivesPerSemester {
		w = append(w, "Student is at the maximum of accepted professional electives.")
	}
	if it.Group.AcceptedCount >= it.Group.Capacity {
		w = append(w, "Group is at capacity — accepting requires a capacity adjustment or a new group.")
	}
	if it.Request.PriorityGroup == string(domain.PriorityWaitlistOppositeShift) {
		w = append(w, "Opposite-shift request — lowest priority.")
	}
	return w
}

// itoa is a tiny base-10 formatter to keep the query builder dependency-free.
func itoa(i int) string {
	if i == 0 {
		return "0"
	}
	var b []byte
	for i > 0 {
		b = append([]byte{byte('0' + i%10)}, b...)
		i /= 10
	}
	return string(b)
}
