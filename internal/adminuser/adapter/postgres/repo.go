// Package postgres is the driven adapter implementing the admin-user Repository
// port with pgx. Every state-changing operation writes an audit event inside
// the same transaction (BR-012).
package postgres

import (
	"context"
	"errors"
	"strconv"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/uniquindio/profundiza-uq/internal/adminuser/app"
	"github.com/uniquindio/profundiza-uq/internal/adminuser/domain"
	"github.com/uniquindio/profundiza-uq/internal/shared/audit"
)

const uniqueViolation = "23505"

// Repo is a pgx-backed admin-user repository.
type Repo struct {
	pool *pgxpool.Pool
}

// NewRepo builds a Repo over a pgx pool.
func NewRepo(pool *pgxpool.Pool) *Repo { return &Repo{pool: pool} }

const columns = `id, institutional_email, full_name, role, status, created_at, updated_at`

// List returns a filtered page of admin users plus the total matching count.
func (r *Repo) List(ctx context.Context, f app.ListFilter) ([]domain.AdminUser, int, error) {
	limit, offset := f.Normalize()

	where := "WHERE 1=1"
	args := []any{}
	if f.Role != nil {
		args = append(args, string(*f.Role))
		where += " AND role = $" + strconv.Itoa(len(args))
	}
	if f.Status != nil {
		args = append(args, string(*f.Status))
		where += " AND status = $" + strconv.Itoa(len(args))
	}

	var total int
	if err := r.pool.QueryRow(ctx, `SELECT count(*) FROM admin_users `+where, args...).Scan(&total); err != nil {
		return nil, 0, err
	}

	args = append(args, limit, offset)
	q := `SELECT ` + columns + ` FROM admin_users ` + where +
		` ORDER BY created_at DESC, id DESC LIMIT $` + strconv.Itoa(len(args)-1) + ` OFFSET $` + strconv.Itoa(len(args))
	rows, err := r.pool.Query(ctx, q, args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	out := []domain.AdminUser{}
	for rows.Next() {
		u, err := scan(rows)
		if err != nil {
			return nil, 0, err
		}
		out = append(out, u)
	}
	return out, total, rows.Err()
}

// Get returns an admin user by id, or nil when missing.
func (r *Repo) Get(ctx context.Context, id string) (*domain.AdminUser, error) {
	u, err := scan(r.pool.QueryRow(ctx, `SELECT `+columns+` FROM admin_users WHERE id = $1`, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &u, nil
}

// Create inserts an admin user and its creation audit event atomically.
func (r *Repo) Create(ctx context.Context, in domain.NewAdminUser, actor app.Actor) (domain.AdminUser, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return domain.AdminUser{}, err
	}
	defer tx.Rollback(ctx) //nolint:errcheck // no-op after commit

	u, err := scan(tx.QueryRow(ctx,
		`INSERT INTO admin_users (institutional_email, full_name, role)
		 VALUES ($1, $2, $3)
		 RETURNING `+columns,
		in.InstitutionalEmail, in.FullName, string(in.Role)))
	if err != nil {
		return domain.AdminUser{}, mapWriteErr(err)
	}

	if err := writeAudit(ctx, tx, actor, "ADMIN_USER_CREATED", u.ID, nil, u); err != nil {
		return domain.AdminUser{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return domain.AdminUser{}, err
	}
	return u, nil
}

// Update applies a partial change and records previous/new values in the audit.
func (r *Repo) Update(ctx context.Context, id string, patch domain.AdminUserPatch, actor app.Actor) (*domain.AdminUser, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx) //nolint:errcheck // no-op after commit

	before, err := scan(tx.QueryRow(ctx, `SELECT `+columns+` FROM admin_users WHERE id = $1 FOR UPDATE`, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	set := ""
	args := []any{}
	add := func(col string, v any) {
		args = append(args, v)
		if set != "" {
			set += ", "
		}
		set += col + " = $" + strconv.Itoa(len(args))
	}
	if patch.FullName != nil {
		add("full_name", *patch.FullName)
	}
	if patch.Role != nil {
		add("role", string(*patch.Role))
	}
	if patch.Status != nil {
		add("status", string(*patch.Status))
	}

	after := before
	if set != "" {
		args = append(args, id)
		after, err = scan(tx.QueryRow(ctx,
			`UPDATE admin_users SET `+set+`, updated_at = now() WHERE id = $`+strconv.Itoa(len(args))+
				` RETURNING `+columns, args...))
		if err != nil {
			return nil, mapWriteErr(err)
		}
	}

	if err := writeAudit(ctx, tx, actor, "ADMIN_USER_UPDATED", after.ID, before, after); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return &after, nil
}

// --- helpers ---

type scannable interface {
	Scan(dest ...any) error
}

func scan(row scannable) (domain.AdminUser, error) {
	var u domain.AdminUser
	err := row.Scan(&u.ID, &u.InstitutionalEmail, &u.FullName, &u.Role, &u.Status, &u.CreatedAt, &u.UpdatedAt)
	return u, err
}

func writeAudit(ctx context.Context, ex audit.Execer, actor app.Actor, action, entityID string, prev, next any) error {
	actorType := audit.ActorSystem
	if actor.Type != "" {
		actorType = actor.Type
	}
	return audit.Write(ctx, ex, audit.Event{
		ActorType:     actorType,
		ActorID:       actor.ID,
		Action:        action,
		EntityType:    "ADMIN_USER",
		EntityID:      entityID,
		PreviousValue: prev,
		NewValue:      next,
	})
}

func mapWriteErr(err error) error {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == uniqueViolation {
		return app.ErrEmailTaken
	}
	return err
}
