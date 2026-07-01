// Package postgres is the driven adapter implementing the audit read Repository
// port with pgx. It only reads the append-only audit_events table.
package postgres

import (
	"context"
	"strconv"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/uniquindio/profundiza-uq/internal/audit/app"
	"github.com/uniquindio/profundiza-uq/internal/audit/domain"
)

// Repo is a pgx-backed audit read repository.
type Repo struct {
	pool *pgxpool.Pool
}

// NewRepo builds a Repo over a pgx pool.
func NewRepo(pool *pgxpool.Pool) *Repo { return &Repo{pool: pool} }

const columns = `id, actor_type, actor_id, action, entity_type, entity_id,
	previous_value_json, new_value_json, reason, created_at`

// List returns a filtered page of audit events plus the total matching count,
// ordered newest first.
func (r *Repo) List(ctx context.Context, f app.ListFilter) ([]domain.AuditEvent, int, error) {
	limit, offset := f.Normalize()

	where := "WHERE 1=1"
	args := []any{}
	if f.EntityType != nil {
		args = append(args, *f.EntityType)
		where += " AND entity_type = $" + strconv.Itoa(len(args))
	}
	if f.EntityID != nil {
		args = append(args, *f.EntityID)
		where += " AND entity_id = $" + strconv.Itoa(len(args))
	}
	if f.ActorID != nil {
		args = append(args, *f.ActorID)
		where += " AND actor_id = $" + strconv.Itoa(len(args))
	}

	var total int
	if err := r.pool.QueryRow(ctx, `SELECT count(*) FROM audit_events `+where, args...).Scan(&total); err != nil {
		return nil, 0, err
	}

	args = append(args, limit, offset)
	q := `SELECT ` + columns + ` FROM audit_events ` + where +
		` ORDER BY created_at DESC, id DESC LIMIT $` + strconv.Itoa(len(args)-1) + ` OFFSET $` + strconv.Itoa(len(args))
	rows, err := r.pool.Query(ctx, q, args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	out := []domain.AuditEvent{}
	for rows.Next() {
		var (
			e        domain.AuditEvent
			actorID  *string
			entityID *string
			prev     []byte
			next     []byte
			reason   *string
		)
		if err := rows.Scan(&e.ID, &e.ActorType, &actorID, &e.Action, &e.EntityType, &entityID,
			&prev, &next, &reason, &e.CreatedAt); err != nil {
			return nil, 0, err
		}
		e.ActorID = actorID
		e.EntityID = entityID
		e.PreviousValue = prev
		e.NewValue = next
		e.Reason = reason
		out = append(out, e)
	}
	return out, total, rows.Err()
}
