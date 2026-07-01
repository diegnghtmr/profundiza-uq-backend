// Package postgres implements the identity ports (challenges, sessions,
// directory) with pgx.
package postgres

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/uniquindio/profundiza-uq/internal/identity/app"
	"github.com/uniquindio/profundiza-uq/internal/platform/authn"
)

// ChallengeRepo persists OTP login challenges.
type ChallengeRepo struct{ pool *pgxpool.Pool }

// NewChallengeRepo builds a ChallengeRepo.
func NewChallengeRepo(pool *pgxpool.Pool) *ChallengeRepo { return &ChallengeRepo{pool: pool} }

// Create stores a new challenge.
func (r *ChallengeRepo) Create(ctx context.Context, email, codeHash string, expiresAt time.Time) error {
	_, err := r.pool.Exec(ctx,
		`INSERT INTO login_challenges (email, code_hash, expires_at) VALUES ($1,$2,$3)`,
		email, codeHash, expiresAt)
	return err
}

// LatestActive returns the most recent unconsumed, unexpired challenge.
func (r *ChallengeRepo) LatestActive(ctx context.Context, email string, now time.Time) (*app.Challenge, bool, error) {
	var c app.Challenge
	err := r.pool.QueryRow(ctx,
		`SELECT id, email, code_hash, expires_at, attempts
		   FROM login_challenges
		  WHERE email = $1 AND consumed_at IS NULL AND expires_at > $2
		  ORDER BY created_at DESC LIMIT 1`, email, now,
	).Scan(&c.ID, &c.Email, &c.CodeHash, &c.ExpiresAt, &c.Attempts)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	return &c, true, nil
}

// IncrementAttempts records a failed verification attempt.
func (r *ChallengeRepo) IncrementAttempts(ctx context.Context, id string) error {
	_, err := r.pool.Exec(ctx, `UPDATE login_challenges SET attempts = attempts + 1 WHERE id = $1`, id)
	return err
}

// Consume marks a challenge as used.
func (r *ChallengeRepo) Consume(ctx context.Context, id string, now time.Time) error {
	_, err := r.pool.Exec(ctx, `UPDATE login_challenges SET consumed_at = $2 WHERE id = $1`, id, now)
	return err
}

// SessionRepo persists sessions.
type SessionRepo struct{ pool *pgxpool.Pool }

// NewSessionRepo builds a SessionRepo.
func NewSessionRepo(pool *pgxpool.Pool) *SessionRepo { return &SessionRepo{pool: pool} }

// Create inserts a session.
func (r *SessionRepo) Create(ctx context.Context, s app.SessionRecord) error {
	_, err := r.pool.Exec(ctx,
		`INSERT INTO sessions (id, subject_type, subject_id, role, csrf_token, expires_at)
		 VALUES ($1,$2,$3,$4,$5,$6)`,
		s.ID, string(s.SubjectType), s.SubjectID, string(s.Role), s.CSRFToken, s.ExpiresAt)
	return err
}

// Get loads a session by id.
func (r *SessionRepo) Get(ctx context.Context, id string) (*app.SessionRecord, bool, error) {
	var s app.SessionRecord
	var subjectType, role string
	err := r.pool.QueryRow(ctx,
		`SELECT id, subject_type, subject_id, role, csrf_token, expires_at FROM sessions WHERE id = $1`, id,
	).Scan(&s.ID, &subjectType, &s.SubjectID, &role, &s.CSRFToken, &s.ExpiresAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	s.SubjectType = authn.SubjectType(subjectType)
	s.Role = authn.Role(role)
	return &s, true, nil
}

// Delete removes a session.
func (r *SessionRepo) Delete(ctx context.Context, id string) error {
	_, err := r.pool.Exec(ctx, `DELETE FROM sessions WHERE id = $1`, id)
	return err
}

// DeleteExpired removes all sessions whose expires_at is in the past and
// returns the number of rows deleted.
func (r *SessionRepo) DeleteExpired(ctx context.Context) (int64, error) {
	tag, err := r.pool.Exec(ctx, `DELETE FROM sessions WHERE expires_at < now()`)
	if err != nil {
		return 0, err
	}
	return tag.RowsAffected(), nil
}

// DirectoryRepo resolves accounts across students and admin_users.
type DirectoryRepo struct{ pool *pgxpool.Pool }

// NewDirectoryRepo builds a DirectoryRepo.
func NewDirectoryRepo(pool *pgxpool.Pool) *DirectoryRepo { return &DirectoryRepo{pool: pool} }

// FindByEmail looks up an active account by institutional email, preferring an
// admin record when the same email exists in both tables.
func (r *DirectoryRepo) FindByEmail(ctx context.Context, email string) (*app.DirectoryUser, bool, error) {
	// Admins first (higher privilege), then students.
	if u, ok, err := r.scanAdmin(ctx,
		`SELECT id, role, full_name, institutional_email FROM admin_users
		  WHERE lower(institutional_email) = $1 AND status = 'ACTIVE'`, email); err != nil || ok {
		return u, ok, err
	}
	return r.scanStudent(ctx,
		`SELECT id, full_name, institutional_email FROM students
		  WHERE lower(institutional_email) = $1 AND status = 'ACTIVE'`, email)
}

// FindBySubject resolves an account from a session's subject reference.
func (r *DirectoryRepo) FindBySubject(ctx context.Context, st authn.SubjectType, id string) (*app.DirectoryUser, bool, error) {
	if st == authn.SubjectAdmin {
		return r.scanAdmin(ctx,
			`SELECT id, role, full_name, institutional_email FROM admin_users WHERE id = $1 AND status = 'ACTIVE'`, id)
	}
	return r.scanStudent(ctx,
		`SELECT id, full_name, institutional_email FROM students WHERE id = $1 AND status = 'ACTIVE'`, id)
}

func (r *DirectoryRepo) scanAdmin(ctx context.Context, sql, arg string) (*app.DirectoryUser, bool, error) {
	var u app.DirectoryUser
	var role string
	err := r.pool.QueryRow(ctx, sql, arg).Scan(&u.SubjectID, &role, &u.FullName, &u.Email)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	u.SubjectType = authn.SubjectAdmin
	u.Role = authn.Role(role)
	return &u, true, nil
}

func (r *DirectoryRepo) scanStudent(ctx context.Context, sql, arg string) (*app.DirectoryUser, bool, error) {
	var u app.DirectoryUser
	err := r.pool.QueryRow(ctx, sql, arg).Scan(&u.SubjectID, &u.FullName, &u.Email)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	u.SubjectType = authn.SubjectStudent
	u.Role = authn.RoleStudent
	return &u, true, nil
}
