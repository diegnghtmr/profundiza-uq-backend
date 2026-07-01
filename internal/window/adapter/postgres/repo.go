// Package postgres implements the window Repository with pgx.
package postgres

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/uniquindio/profundiza-uq/internal/shared/audit"
	"github.com/uniquindio/profundiza-uq/internal/window/app"
	"github.com/uniquindio/profundiza-uq/internal/window/domain"
)

// Repo is the pgx-backed window repository.
type Repo struct{ pool *pgxpool.Pool }

// NewRepo builds a window Repo.
func NewRepo(pool *pgxpool.Pool) *Repo { return &Repo{pool: pool} }

const cols = `id, semester_id, name, starts_at, ends_at, target_shift, status, created_at, updated_at`

type scannable interface{ Scan(dest ...any) error }

func scan(row scannable) (domain.Window, error) {
	var w domain.Window
	err := row.Scan(&w.ID, &w.SemesterID, &w.Name, &w.StartsAt, &w.EndsAt, &w.TargetShift, &w.Status, &w.CreatedAt, &w.UpdatedAt)
	return w, err
}

// ListBySemester lists a semester's windows by start time.
func (r *Repo) ListBySemester(ctx context.Context, semesterID string) ([]domain.Window, error) {
	rows, err := r.pool.Query(ctx, `SELECT `+cols+` FROM enrollment_windows WHERE semester_id=$1 ORDER BY starts_at`, semesterID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []domain.Window{}
	for rows.Next() {
		w, err := scan(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, w)
	}
	return out, rows.Err()
}

// Get returns one window or nil.
func (r *Repo) Get(ctx context.Context, id string) (*domain.Window, error) {
	w, err := scan(r.pool.QueryRow(ctx, `SELECT `+cols+` FROM enrollment_windows WHERE id=$1`, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &w, nil
}

// Create inserts a window and audits it.
func (r *Repo) Create(ctx context.Context, in app.CreateInput) (domain.Window, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return domain.Window{}, err
	}
	defer tx.Rollback(ctx) //nolint:errcheck
	w, err := scan(tx.QueryRow(ctx,
		`INSERT INTO enrollment_windows (semester_id, name, starts_at, ends_at, target_shift)
		 VALUES ($1,$2,$3,$4,$5) RETURNING `+cols,
		in.SemesterID, in.Name, in.StartsAt, in.EndsAt, in.TargetShift))
	if err != nil {
		return domain.Window{}, err
	}
	if err := audit.Write(ctx, tx, audit.Event{ActorType: audit.ActorAdmin, ActorID: in.ActorID,
		Action: "WINDOW_CREATED", EntityType: "EnrollmentWindow", EntityID: w.ID,
		NewValue: map[string]any{"semesterId": w.SemesterID, "name": w.Name}}); err != nil {
		return domain.Window{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return domain.Window{}, err
	}
	return w, nil
}

// Update updates a window's editable fields.
func (r *Repo) Update(ctx context.Context, in app.UpdateInput) (domain.Window, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return domain.Window{}, err
	}
	defer tx.Rollback(ctx) //nolint:errcheck
	w, err := scan(tx.QueryRow(ctx,
		`UPDATE enrollment_windows SET
		   name = COALESCE($2, name), starts_at = COALESCE($3, starts_at), ends_at = COALESCE($4, ends_at),
		   target_shift = COALESCE($5, target_shift), status = COALESCE($6, status), updated_at = now()
		 WHERE id = $1 RETURNING `+cols,
		in.ID, in.Name, in.StartsAt, in.EndsAt, in.TargetShift, in.Status))
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Window{}, app.ErrNotFound
	}
	if err != nil {
		return domain.Window{}, err
	}
	if err := audit.Write(ctx, tx, audit.Event{ActorType: audit.ActorAdmin, ActorID: in.ActorID,
		Action: "WINDOW_UPDATED", EntityType: "EnrollmentWindow", EntityID: w.ID}); err != nil {
		return domain.Window{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return domain.Window{}, err
	}
	return w, nil
}
