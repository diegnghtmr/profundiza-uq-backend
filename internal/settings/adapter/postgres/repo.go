// Package postgres is the driven adapter implementing the global-settings
// Repository port with pgx. Every upsert writes an audit event inside the same
// transaction (BR-012).
package postgres

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/uniquindio/profundiza-uq/internal/settings/app"
	"github.com/uniquindio/profundiza-uq/internal/settings/domain"
	"github.com/uniquindio/profundiza-uq/internal/shared/audit"
)

// Repo is a pgx-backed global-settings repository.
type Repo struct {
	pool *pgxpool.Pool
}

// NewRepo builds a Repo over a pgx pool.
func NewRepo(pool *pgxpool.Pool) *Repo { return &Repo{pool: pool} }

const columns = `key, value_json, description, updated_by_admin_user_id, updated_at`

// List returns a paginated page of settings plus the total count.
func (r *Repo) List(ctx context.Context, f app.ListFilter) ([]domain.GlobalSetting, int, error) {
	limit, offset := f.Normalize()

	var total int
	if err := r.pool.QueryRow(ctx, `SELECT count(*) FROM global_settings`).Scan(&total); err != nil {
		return nil, 0, err
	}

	rows, err := r.pool.Query(ctx,
		`SELECT `+columns+` FROM global_settings ORDER BY key LIMIT $1 OFFSET $2`, limit, offset)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	out := []domain.GlobalSetting{}
	for rows.Next() {
		s, err := scan(rows)
		if err != nil {
			return nil, 0, err
		}
		out = append(out, s)
	}
	return out, total, rows.Err()
}

// Upsert inserts or updates a setting and writes its audit event atomically.
// The change reason is recorded on the audit event (BR-012).
func (r *Repo) Upsert(ctx context.Context, in domain.UpsertSetting, actor app.Actor) (domain.GlobalSetting, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return domain.GlobalSetting{}, err
	}
	defer tx.Rollback(ctx) //nolint:errcheck // no-op after commit

	// Capture the previous value (if any) for the audit trail.
	var previous *domain.GlobalSetting
	prev, err := scan(tx.QueryRow(ctx, `SELECT `+columns+` FROM global_settings WHERE key = $1 FOR UPDATE`, in.Key))
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		previous = nil
	case err != nil:
		return domain.GlobalSetting{}, err
	default:
		previous = &prev
	}

	var actorID *string
	if actor.ID != "" {
		actorID = &actor.ID
	}

	s, err := scan(tx.QueryRow(ctx,
		`INSERT INTO global_settings (key, value_json, updated_by_admin_user_id)
		 VALUES ($1, $2, $3)
		 ON CONFLICT (key) DO UPDATE
		   SET value_json = EXCLUDED.value_json,
		       updated_by_admin_user_id = EXCLUDED.updated_by_admin_user_id,
		       updated_at = now()
		 RETURNING `+columns,
		in.Key, []byte(in.Value), actorID))
	if err != nil {
		return domain.GlobalSetting{}, err
	}

	actorType := audit.ActorSystem
	if actor.Type != "" {
		actorType = actor.Type
	}
	var prevValue any
	if previous != nil {
		prevValue = previous.Value
	}
	if err := audit.Write(ctx, tx, audit.Event{
		ActorType:     actorType,
		ActorID:       actor.ID,
		Action:        "GLOBAL_SETTING_UPDATED",
		EntityType:    "GLOBAL_SETTING",
		EntityID:      s.Key,
		PreviousValue: prevValue,
		NewValue:      s.Value,
		Reason:        in.Reason,
	}); err != nil {
		return domain.GlobalSetting{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return domain.GlobalSetting{}, err
	}
	return s, nil
}

type scannable interface {
	Scan(dest ...any) error
}

func scan(row scannable) (domain.GlobalSetting, error) {
	var s domain.GlobalSetting
	var value []byte
	err := row.Scan(&s.Key, &value, &s.Description, &s.UpdatedByAdminUserID, &s.UpdatedAt)
	if err != nil {
		return domain.GlobalSetting{}, err
	}
	s.Value = value
	return s, nil
}
