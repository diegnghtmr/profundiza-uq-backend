package file_test

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	platformpg "github.com/uniquindio/profundiza-uq/internal/platform/postgres"
	reportingfile "github.com/uniquindio/profundiza-uq/internal/reporting/adapter/file"
	"github.com/uniquindio/profundiza-uq/internal/reporting/domain"
	"github.com/uniquindio/profundiza-uq/migrations"
)

func testPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	url := os.Getenv("TEST_DATABASE_URL")
	if url == "" {
		t.Skip("TEST_DATABASE_URL not set; skipping integration test")
	}
	ctx := context.Background()
	pool, err := platformpg.Connect(ctx, url)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	if err := platformpg.RunMigrations(ctx, pool, migrations.FS); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	t.Cleanup(pool.Close)
	return pool
}

func suffix() string {
	var b [6]byte
	_, _ = rand.Read(b[:])
	return hex.EncodeToString(b[:])
}

// TestDataSourceQueriesAreValid runs every report query against the real schema,
// proving the SQL (column names, joins) matches the migration. The semester has
// no enrollment data, so each query simply returns zero rows.
func TestDataSourceQueriesAreValid(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()

	var semesterID string
	if err := pool.QueryRow(ctx,
		`INSERT INTO semesters (code, name, starts_at, ends_at, status)
		 VALUES ($1, 'Gen Test', now(), now() + interval '90 days', 'DRAFT') RETURNING id`,
		"GEN-"+suffix()).Scan(&semesterID); err != nil {
		t.Fatalf("seed semester: %v", err)
	}

	ds := reportingfile.NewPostgresData(pool)
	types := []domain.ReportType{
		domain.ReportGeneralSemester, domain.ReportAcceptedRequests, domain.ReportRejectedRequests,
		domain.ReportCancelledRequests, domain.ReportWaitlist, domain.ReportByGroup,
		domain.ReportCapacity, domain.ReportAdminReview,
		// Fallback path (no dedicated query):
		domain.ReportByElective, domain.ReportByStudent, domain.ReportAudit,
	}
	for _, rt := range types {
		t.Run(string(rt), func(t *testing.T) {
			_, err := ds.Fetch(ctx, domain.ReportExport{ReportType: rt, SemesterID: &semesterID})
			if err != nil {
				t.Fatalf("Fetch(%s): %v", rt, err)
			}
		})
	}
}

// TestGeneratorWritesFiles proves the XLSX and PDF renderers produce non-empty
// files on disk in the configured base directory.
func TestGeneratorWritesFiles(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()

	var semesterID string
	if err := pool.QueryRow(ctx,
		`INSERT INTO semesters (code, name, starts_at, ends_at, status)
		 VALUES ($1, 'Gen Files', now(), now() + interval '90 days', 'DRAFT') RETURNING id`,
		"GENF-"+suffix()).Scan(&semesterID); err != nil {
		t.Fatalf("seed semester: %v", err)
	}

	baseDir := t.TempDir()
	gen := reportingfile.NewGenerator(reportingfile.NewPostgresData(pool), baseDir)

	for _, format := range []domain.Format{domain.FormatXLSX, domain.FormatPDF} {
		export := domain.ReportExport{
			ID:         "report-" + suffix(),
			ReportType: domain.ReportCapacity,
			Format:     format,
			SemesterID: &semesterID,
		}
		path, err := gen.Generate(ctx, export)
		if err != nil {
			t.Fatalf("Generate(%s): %v", format, err)
		}
		if filepath.Dir(path) != baseDir {
			t.Fatalf("file written outside base dir: %s", path)
		}
		info, err := os.Stat(path)
		if err != nil {
			t.Fatalf("stat %s: %v", path, err)
		}
		if info.Size() == 0 {
			t.Fatalf("generated %s file is empty", format)
		}
	}
}
