// Package postgres implements the catalog read Repository with pgx, computing
// live seat occupancy from enrollment_requests.
package postgres

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/uniquindio/profundiza-uq/internal/catalog/app"
	"github.com/uniquindio/profundiza-uq/internal/catalog/domain"
	shared "github.com/uniquindio/profundiza-uq/internal/shared/domain"
)

// Repo is the pgx-backed catalog repository.
type Repo struct{ pool *pgxpool.Pool }

// NewRepo builds a catalog Repo.
func NewRepo(pool *pgxpool.Pool) *Repo { return &Repo{pool: pool} }

// groupsQuery computes occupancy counts per group for a set of offerings,
// optionally filtered by shift.
const groupsQuery = `
SELECT g.id, g.offering_id, g.group_code, g.shift, g.teacher_name, g.schedule_text, g.capacity,
       COALESCE(c.accepted,0), COALESCE(c.pending_direct,0), COALESCE(c.wl_same,0), COALESCE(c.wl_opp,0),
       g.status, g.created_at, g.updated_at
FROM offering_groups g
LEFT JOIN (
  SELECT offering_group_id,
    count(*) FILTER (WHERE status='ACCEPTED')                                              AS accepted,
    count(*) FILTER (WHERE status='PENDING_REVIEW' AND priority_group='DIRECT_SAME_SHIFT') AS pending_direct,
    count(*) FILTER (WHERE status='WAITLIST_SAME_SHIFT')                                    AS wl_same,
    count(*) FILTER (WHERE status='WAITLIST_OPPOSITE_SHIFT')                                AS wl_opp
  FROM enrollment_requests
  GROUP BY offering_group_id
) c ON c.offering_group_id = g.id
WHERE g.offering_id = ANY($1)
  AND ($2::text IS NULL OR g.shift = $2)
ORDER BY g.group_code`

func (r *Repo) groupsFor(ctx context.Context, offeringIDs []string, shift *shared.AcademicShift) (map[string][]domain.Group, error) {
	var shiftArg *string
	if shift != nil {
		s := string(*shift)
		shiftArg = &s
	}
	rows, err := r.pool.Query(ctx, groupsQuery, offeringIDs, shiftArg)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := map[string][]domain.Group{}
	for rows.Next() {
		var g domain.Group
		if err := rows.Scan(&g.ID, &g.OfferingID, &g.GroupCode, &g.Shift, &g.TeacherName, &g.ScheduleText,
			&g.Capacity, &g.AcceptedCount, &g.PendingDirectCount, &g.WaitlistSameShiftCount,
			&g.WaitlistOppositeShiftCount, &g.Status, &g.CreatedAt, &g.UpdatedAt); err != nil {
			return nil, err
		}
		out[g.OfferingID] = append(out[g.OfferingID], g)
	}
	return out, rows.Err()
}

// ListOfferings returns active offerings for a semester with their groups.
func (r *Repo) ListOfferings(ctx context.Context, f app.OfferingFilter) ([]domain.OfferingSummary, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT o.id, o.semester_id, e.id, e.name, e.area, e.description, e.status, e.created_at, e.updated_at
		   FROM elective_offerings o
		   JOIN electives e ON e.id = o.elective_id
		  WHERE o.semester_id = $1 AND o.status = 'ACTIVE'
		  ORDER BY e.name`, f.SemesterID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var offerings []domain.OfferingSummary
	var ids []string
	for rows.Next() {
		var o domain.OfferingSummary
		if err := rows.Scan(&o.ID, &o.SemesterID, &o.Elective.ID, &o.Elective.Name, &o.Elective.Area,
			&o.Elective.Description, &o.Elective.Status, &o.Elective.CreatedAt, &o.Elective.UpdatedAt); err != nil {
			return nil, err
		}
		offerings = append(offerings, o)
		ids = append(ids, o.ID)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if len(ids) == 0 {
		return []domain.OfferingSummary{}, nil
	}

	groups, err := r.groupsFor(ctx, ids, f.Shift)
	if err != nil {
		return nil, err
	}

	result := make([]domain.OfferingSummary, 0, len(offerings))
	for _, o := range offerings {
		o.Groups = filterGroups(groups[o.ID], f.OnlyOpen)
		if f.OnlyOpen && len(o.Groups) == 0 {
			continue
		}
		if o.Groups == nil {
			o.Groups = []domain.Group{}
		}
		result = append(result, o)
	}
	return result, nil
}

func filterGroups(groups []domain.Group, onlyOpen bool) []domain.Group {
	if !onlyOpen {
		return groups
	}
	out := make([]domain.Group, 0, len(groups))
	for _, g := range groups {
		if g.HasOpenSeats() {
			out = append(out, g)
		}
	}
	return out
}

// GetOfferingDetail returns one offering with its groups and prerequisites.
func (r *Repo) GetOfferingDetail(ctx context.Context, offeringID string) (*domain.OfferingDetail, error) {
	var d domain.OfferingDetail
	err := r.pool.QueryRow(ctx,
		`SELECT o.id, o.semester_id, e.id, e.name, e.area, e.description, e.status, e.created_at, e.updated_at
		   FROM elective_offerings o
		   JOIN electives e ON e.id = o.elective_id
		  WHERE o.id = $1`, offeringID,
	).Scan(&d.ID, &d.SemesterID, &d.Elective.ID, &d.Elective.Name, &d.Elective.Area,
		&d.Elective.Description, &d.Elective.Status, &d.Elective.CreatedAt, &d.Elective.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	groups, err := r.groupsFor(ctx, []string{offeringID}, nil)
	if err != nil {
		return nil, err
	}
	d.Groups = groups[offeringID]
	if d.Groups == nil {
		d.Groups = []domain.Group{}
	}

	prereqs, err := r.ListEffectivePrerequisites(ctx, offeringID)
	if err != nil {
		return nil, err
	}
	d.Prerequisites = prereqs
	return &d, nil
}

// ListEffectivePrerequisites returns the active effective prerequisites of an
// offering (elective defaults + offering-specific), the set used for review.
func (r *Repo) ListEffectivePrerequisites(ctx context.Context, offeringID string) ([]domain.Prerequisite, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT id, offering_id, name, description, plan_type, source, status
		   FROM offering_prerequisites
		  WHERE offering_id = $1 AND status = 'ACTIVE'
		  ORDER BY source, name`, offeringID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []domain.Prerequisite{}
	for rows.Next() {
		var p domain.Prerequisite
		if err := rows.Scan(&p.ID, &p.OfferingID, &p.Name, &p.Description, &p.PlanType, &p.Source, &p.Status); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}
