// Package postgres is the driven adapter implementing the semester Repository
// port with pgx.
package postgres

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/uniquindio/profundiza-uq/internal/semester/app"
	"github.com/uniquindio/profundiza-uq/internal/semester/domain"
	"github.com/uniquindio/profundiza-uq/internal/shared/audit"
)

// Repo is a pgx-backed semester repository.
type Repo struct {
	pool *pgxpool.Pool
}

// NewRepo builds a Repo over a pgx pool.
func NewRepo(pool *pgxpool.Pool) *Repo {
	return &Repo{pool: pool}
}

const selectColumns = `id, code, name, starts_at, ends_at, status, created_at, updated_at`

// List returns all semesters ordered by start date descending.
func (r *Repo) List(ctx context.Context) ([]domain.Semester, error) {
	rows, err := r.pool.Query(ctx, `SELECT `+selectColumns+` FROM semesters ORDER BY starts_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []domain.Semester
	for rows.Next() {
		s, err := scan(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

// GetActive returns the active semester or nil when none exists.
func (r *Repo) GetActive(ctx context.Context) (*domain.Semester, error) {
	s, err := scan(r.pool.QueryRow(ctx, `SELECT `+selectColumns+` FROM semesters WHERE status='ACTIVE' LIMIT 1`))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &s, nil
}

// Get returns one semester by id or nil.
func (r *Repo) Get(ctx context.Context, id string) (*domain.Semester, error) {
	s, err := scan(r.pool.QueryRow(ctx, `SELECT `+selectColumns+` FROM semesters WHERE id=$1`, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &s, nil
}

// Create inserts a DRAFT semester and audits it.
func (r *Repo) Create(ctx context.Context, in app.CreateInput) (domain.Semester, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return domain.Semester{}, err
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	s, err := scan(tx.QueryRow(ctx,
		`INSERT INTO semesters (code, name, starts_at, ends_at, status)
		 VALUES ($1,$2,$3,$4,'DRAFT') RETURNING `+selectColumns,
		in.Code, in.Name, in.StartsAt, in.EndsAt))
	if err != nil {
		return domain.Semester{}, err
	}
	if err := audit.Write(ctx, tx, audit.Event{
		ActorType: audit.ActorAdmin, ActorID: in.ActorID, Action: "SEMESTER_CREATED",
		EntityType: "Semester", EntityID: s.ID, NewValue: map[string]any{"code": s.Code, "name": s.Name},
	}); err != nil {
		return domain.Semester{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return domain.Semester{}, err
	}
	return s, nil
}

// Activate closes the previously active semester and activates this one.
func (r *Repo) Activate(ctx context.Context, id, actorID string) (*domain.Semester, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	if _, err := tx.Exec(ctx, `UPDATE semesters SET status='CLOSED', updated_at=now() WHERE status='ACTIVE' AND id<>$1`, id); err != nil {
		return nil, err
	}
	s, err := scan(tx.QueryRow(ctx,
		`UPDATE semesters SET status='ACTIVE', updated_at=now() WHERE id=$1 RETURNING `+selectColumns, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if err := audit.Write(ctx, tx, audit.Event{
		ActorType: audit.ActorAdmin, ActorID: actorID, Action: "SEMESTER_ACTIVATED",
		EntityType: "Semester", EntityID: id, NewValue: map[string]any{"status": "ACTIVE"},
	}); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return &s, nil
}

// Close closes a semester with a mandatory reason.
func (r *Repo) Close(ctx context.Context, id, actorID, reason string) (*domain.Semester, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	s, err := scan(tx.QueryRow(ctx,
		`UPDATE semesters SET status='CLOSED', updated_at=now() WHERE id=$1 RETURNING `+selectColumns, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if err := audit.Write(ctx, tx, audit.Event{
		ActorType: audit.ActorAdmin, ActorID: actorID, Action: "SEMESTER_CLOSED",
		EntityType: "Semester", EntityID: id, NewValue: map[string]any{"status": "CLOSED"}, Reason: reason,
	}); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return &s, nil
}

type scannable interface{ Scan(dest ...any) error }

func scan(row scannable) (domain.Semester, error) {
	var s domain.Semester
	err := row.Scan(&s.ID, &s.Code, &s.Name, &s.StartsAt, &s.EndsAt, &s.Status, &s.CreatedAt, &s.UpdatedAt)
	return s, err
}
