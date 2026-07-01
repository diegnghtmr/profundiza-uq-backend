// Package postgres is the driven adapter implementing the student Repository
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

	"github.com/uniquindio/profundiza-uq/internal/shared/audit"
	"github.com/uniquindio/profundiza-uq/internal/student/app"
	"github.com/uniquindio/profundiza-uq/internal/student/domain"
)

const (
	uniqueViolation     = "23505"
	foreignKeyViolation = "23503"
)

// Repo is a pgx-backed student repository.
type Repo struct {
	pool *pgxpool.Pool
}

// NewRepo builds a Repo over a pgx pool.
func NewRepo(pool *pgxpool.Pool) *Repo { return &Repo{pool: pool} }

const studentColumns = `id, institutional_email, document_number, full_name, academic_shift, status,
	completed_professional_electives_count, created_at, updated_at`

// List returns a filtered page of students plus the total matching count.
func (r *Repo) List(ctx context.Context, f app.ListFilter) ([]domain.Student, int, error) {
	limit, offset := f.Normalize()

	where := "WHERE 1=1"
	args := []any{}
	if f.Q != "" {
		args = append(args, "%"+f.Q+"%")
		p := "$" + strconv.Itoa(len(args))
		where += " AND (full_name ILIKE " + p + " OR institutional_email ILIKE " + p + " OR document_number ILIKE " + p + ")"
	}
	if f.Shift != nil {
		args = append(args, string(*f.Shift))
		where += " AND academic_shift = $" + strconv.Itoa(len(args))
	}
	if f.Status != nil {
		args = append(args, string(*f.Status))
		where += " AND status = $" + strconv.Itoa(len(args))
	}

	var total int
	if err := r.pool.QueryRow(ctx, `SELECT count(*) FROM students `+where, args...).Scan(&total); err != nil {
		return nil, 0, err
	}

	args = append(args, limit, offset)
	q := `SELECT ` + studentColumns + ` FROM students ` + where +
		` ORDER BY created_at DESC, id DESC LIMIT $` + strconv.Itoa(len(args)-1) + ` OFFSET $` + strconv.Itoa(len(args))
	rows, err := r.pool.Query(ctx, q, args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	out := []domain.Student{}
	for rows.Next() {
		s, err := scanStudent(rows)
		if err != nil {
			return nil, 0, err
		}
		out = append(out, s)
	}
	return out, total, rows.Err()
}

// Get returns a student by id, or nil when missing.
func (r *Repo) Get(ctx context.Context, id string) (*domain.Student, error) {
	row := r.pool.QueryRow(ctx, `SELECT `+studentColumns+` FROM students WHERE id = $1`, id)
	s, err := scanStudent(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &s, nil
}

// Create inserts a student and its creation audit event atomically.
func (r *Repo) Create(ctx context.Context, in domain.NewStudent, actor app.Actor) (domain.Student, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return domain.Student{}, err
	}
	defer tx.Rollback(ctx) //nolint:errcheck // no-op after commit

	row := tx.QueryRow(ctx,
		`INSERT INTO students (institutional_email, document_number, full_name, academic_shift,
			completed_professional_electives_count)
		 VALUES ($1, $2, $3, $4, $5)
		 RETURNING `+studentColumns,
		in.InstitutionalEmail, in.DocumentNumber, in.FullName, string(in.AcademicShift),
		in.CompletedProfessionalElectivesCount)
	s, err := scanStudent(row)
	if err != nil {
		return domain.Student{}, mapWriteErr(err)
	}

	if err := writeAudit(ctx, tx, actor, "STUDENT_CREATED", "STUDENT", s.ID, nil, s); err != nil {
		return domain.Student{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return domain.Student{}, err
	}
	return s, nil
}

// Update applies a partial change and records previous/new values in the audit.
func (r *Repo) Update(ctx context.Context, id string, patch domain.StudentPatch, actor app.Actor) (*domain.Student, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx) //nolint:errcheck // no-op after commit

	before, err := scanStudent(tx.QueryRow(ctx, `SELECT `+studentColumns+` FROM students WHERE id = $1 FOR UPDATE`, id))
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
	if patch.AcademicShift != nil {
		add("academic_shift", string(*patch.AcademicShift))
	}
	if patch.Status != nil {
		add("status", string(*patch.Status))
	}
	if patch.CompletedProfessionalElectivesCount != nil {
		add("completed_professional_electives_count", *patch.CompletedProfessionalElectivesCount)
	}

	after := before
	if set != "" {
		args = append(args, id)
		row := tx.QueryRow(ctx,
			`UPDATE students SET `+set+`, updated_at = now() WHERE id = $`+strconv.Itoa(len(args))+
				` RETURNING `+studentColumns, args...)
		after, err = scanStudent(row)
		if err != nil {
			return nil, mapWriteErr(err)
		}
	}

	if err := writeAudit(ctx, tx, actor, "STUDENT_UPDATED", "STUDENT", after.ID, before, after); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return &after, nil
}

const recordColumns = `id, student_id, semester_id, notes, source, created_at, updated_at`

// ListAcademicRecords returns the manual records of a student, newest first.
func (r *Repo) ListAcademicRecords(ctx context.Context, studentID string, semesterID *string) ([]domain.AcademicRecord, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT `+recordColumns+` FROM student_academic_records
		  WHERE student_id = $1 AND ($2::uuid IS NULL OR semester_id = $2)
		  ORDER BY created_at DESC, id DESC`, studentID, semesterID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []domain.AcademicRecord{}
	for rows.Next() {
		rec, err := scanRecord(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, rec)
	}
	return out, rows.Err()
}

// CreateAcademicRecord inserts a manual record and its audit event atomically.
// It returns nil when the parent student does not exist.
func (r *Repo) CreateAcademicRecord(ctx context.Context, studentID string, in domain.NewAcademicRecord, actor app.Actor) (*domain.AcademicRecord, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx) //nolint:errcheck // no-op after commit

	var exists bool
	if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM students WHERE id = $1)`, studentID).Scan(&exists); err != nil {
		return nil, err
	}
	if !exists {
		return nil, nil
	}

	row := tx.QueryRow(ctx,
		`INSERT INTO student_academic_records (student_id, semester_id, notes, source)
		 VALUES ($1, $2, $3, $4)
		 RETURNING `+recordColumns,
		studentID, in.SemesterID, in.Notes, in.Source)
	rec, err := scanRecord(row)
	if err != nil {
		return nil, mapWriteErr(err)
	}

	if err := writeAudit(ctx, tx, actor, "STUDENT_ACADEMIC_RECORD_CREATED", "STUDENT_ACADEMIC_RECORD", rec.ID, nil, rec); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return &rec, nil
}

// --- helpers ---

type scannable interface {
	Scan(dest ...any) error
}

func scanStudent(row scannable) (domain.Student, error) {
	var s domain.Student
	err := row.Scan(&s.ID, &s.InstitutionalEmail, &s.DocumentNumber, &s.FullName, &s.AcademicShift,
		&s.Status, &s.CompletedProfessionalElectivesCount, &s.CreatedAt, &s.UpdatedAt)
	return s, err
}

func scanRecord(row scannable) (domain.AcademicRecord, error) {
	var rec domain.AcademicRecord
	err := row.Scan(&rec.ID, &rec.StudentID, &rec.SemesterID, &rec.Notes, &rec.Source, &rec.CreatedAt, &rec.UpdatedAt)
	return rec, err
}

func writeAudit(ctx context.Context, ex audit.Execer, actor app.Actor, action, entityType, entityID string, prev, next any) error {
	return audit.Write(ctx, ex, audit.Event{
		ActorType:     actorType(actor),
		ActorID:       actor.ID,
		Action:        action,
		EntityType:    entityType,
		EntityID:      entityID,
		PreviousValue: prev,
		NewValue:      next,
	})
}

func actorType(a app.Actor) string {
	if a.Type == "" {
		return audit.ActorSystem
	}
	return a.Type
}

func mapWriteErr(err error) error {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		switch pgErr.Code {
		case uniqueViolation:
			return app.ErrEmailTaken
		case foreignKeyViolation:
			return app.ErrStudentNotFound
		}
	}
	return err
}
