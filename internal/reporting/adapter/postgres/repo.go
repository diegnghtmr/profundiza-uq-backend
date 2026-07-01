// Package postgres is the driven adapter implementing the reporting Repository
// port with pgx.
package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/uniquindio/profundiza-uq/internal/reporting/app"
	"github.com/uniquindio/profundiza-uq/internal/reporting/domain"
)

// Repo is a pgx-backed report-export repository.
type Repo struct {
	pool *pgxpool.Pool
}

// NewRepo builds a Repo over a pgx pool.
func NewRepo(pool *pgxpool.Pool) *Repo {
	return &Repo{pool: pool}
}

const selectColumns = `id, requested_by_admin_user_id, semester_id, report_type, format, status,
	filters_json, file_path, failure_reason, requested_at, started_at, completed_at`

// Create inserts a new export in REQUESTED state and returns the stored row.
func (r *Repo) Create(ctx context.Context, e domain.ReportExport) (domain.ReportExport, error) {
	filters, err := json.Marshal(e.Filters)
	if err != nil {
		return domain.ReportExport{}, err
	}
	row := r.pool.QueryRow(ctx,
		`INSERT INTO report_exports
			(requested_by_admin_user_id, semester_id, report_type, format, status, filters_json)
		 VALUES ($1, $2, $3, $4, $5, $6)
		 RETURNING `+selectColumns,
		e.RequestedByAdminID, e.SemesterID, string(e.ReportType), string(e.Format),
		string(domain.StatusRequested), filters)
	return scan(row)
}

// Get returns the export by id, or app.ErrNotFound when it does not exist.
func (r *Repo) Get(ctx context.Context, id string) (domain.ReportExport, error) {
	row := r.pool.QueryRow(ctx,
		`SELECT `+selectColumns+` FROM report_exports WHERE id = $1`, id)
	e, err := scan(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.ReportExport{}, app.ErrNotFound
	}
	if err != nil {
		return domain.ReportExport{}, err
	}
	return e, nil
}

// ListBySemester returns exports for a semester, newest first.
func (r *Repo) ListBySemester(ctx context.Context, semesterID string) ([]domain.ReportExport, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT `+selectColumns+`
		 FROM report_exports
		 WHERE semester_id = $1
		 ORDER BY requested_at DESC`, semesterID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []domain.ReportExport
	for rows.Next() {
		e, err := scan(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// ClaimNext atomically claims the oldest REQUESTED export, flipping it to
// PROCESSING. The CTE selects a single queued row with FOR UPDATE SKIP LOCKED so
// concurrent workers never claim the same job, then the UPDATE stamps
// started_at. It returns app.ErrNoJob when the queue is empty.
func (r *Repo) ClaimNext(ctx context.Context) (domain.ReportExport, error) {
	row := r.pool.QueryRow(ctx,
		`WITH claimed AS (
			SELECT id
			FROM report_exports
			WHERE status = 'REQUESTED'
			ORDER BY requested_at
			FOR UPDATE SKIP LOCKED
			LIMIT 1
		)
		UPDATE report_exports e
		SET status = 'PROCESSING', started_at = now()
		FROM claimed
		WHERE e.id = claimed.id
		RETURNING `+columnsWithPrefix("e"))
	e, err := scan(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.ReportExport{}, app.ErrNoJob
	}
	if err != nil {
		return domain.ReportExport{}, err
	}
	return e, nil
}

// MarkCompleted records the generated file path and COMPLETED status.
func (r *Repo) MarkCompleted(ctx context.Context, id, filePath string) error {
	_, err := r.pool.Exec(ctx,
		`UPDATE report_exports
		 SET status = 'COMPLETED', file_path = $2, failure_reason = NULL, completed_at = now()
		 WHERE id = $1`, id, filePath)
	return err
}

// MarkFailed records the failure reason and FAILED status.
func (r *Repo) MarkFailed(ctx context.Context, id, reason string) error {
	_, err := r.pool.Exec(ctx,
		`UPDATE report_exports
		 SET status = 'FAILED', failure_reason = $2, completed_at = now()
		 WHERE id = $1`, id, reason)
	return err
}

// columnsWithPrefix returns selectColumns with each column qualified by the
// given table alias, for use in RETURNING clauses on aliased UPDATEs.
func columnsWithPrefix(alias string) string {
	return alias + `.id, ` + alias + `.requested_by_admin_user_id, ` + alias + `.semester_id, ` +
		alias + `.report_type, ` + alias + `.format, ` + alias + `.status, ` +
		alias + `.filters_json, ` + alias + `.file_path, ` + alias + `.failure_reason, ` +
		alias + `.requested_at, ` + alias + `.started_at, ` + alias + `.completed_at`
}

// scannable is satisfied by both pgx.Row and pgx.Rows.
type scannable interface {
	Scan(dest ...any) error
}

func scan(row scannable) (domain.ReportExport, error) {
	var (
		e           domain.ReportExport
		semesterID  *string
		reportType  string
		format      string
		status      string
		filtersJSON []byte
		filePath    *string
		failure     *string
		startedAt   *time.Time
		completedAt *time.Time
	)
	if err := row.Scan(
		&e.ID, &e.RequestedByAdminID, &semesterID, &reportType, &format, &status,
		&filtersJSON, &filePath, &failure, &e.RequestedAt, &startedAt, &completedAt,
	); err != nil {
		return domain.ReportExport{}, err
	}

	e.SemesterID = semesterID
	e.ReportType = domain.ReportType(reportType)
	e.Format = domain.Format(format)
	e.Status = domain.Status(status)
	e.FilePath = filePath
	e.FailureReason = failure
	e.StartedAt = startedAt
	e.CompletedAt = completedAt
	if len(filtersJSON) > 0 {
		_ = json.Unmarshal(filtersJSON, &e.Filters)
	}
	if e.Filters == nil {
		e.Filters = map[string]any{}
	}
	return e, nil
}
