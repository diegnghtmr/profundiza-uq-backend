package postgres_test

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"os"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	platformpg "github.com/uniquindio/profundiza-uq/internal/platform/postgres"
	reportingpg "github.com/uniquindio/profundiza-uq/internal/reporting/adapter/postgres"
	"github.com/uniquindio/profundiza-uq/internal/reporting/app"
	"github.com/uniquindio/profundiza-uq/internal/reporting/domain"
	"github.com/uniquindio/profundiza-uq/migrations"
)

// testPool connects to the throwaway database when TEST_DATABASE_URL is set and
// migrates it. The suite is skipped entirely when the variable is absent, like
// the enrollment integration suite.
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

// seedAdminAndSemester creates the minimal fixtures a report export needs.
func seedAdminAndSemester(t *testing.T, pool *pgxpool.Pool) (adminID, semesterID string) {
	t.Helper()
	ctx := context.Background()
	err := pool.QueryRow(ctx,
		`INSERT INTO admin_users (institutional_email, full_name, role)
		 VALUES ($1, 'Report Admin', 'ADMIN') RETURNING id`,
		"report-admin-"+randomSuffix()+"@uq.edu.co").Scan(&adminID)
	if err != nil {
		t.Fatalf("seed admin: %v", err)
	}
	err = pool.QueryRow(ctx,
		`INSERT INTO semesters (code, name, starts_at, ends_at, status)
		 VALUES ($1, 'Reporting Test', now(), now() + interval '120 days', 'DRAFT')
		 RETURNING id`,
		"RPT-"+randomSuffix()).Scan(&semesterID)
	if err != nil {
		t.Fatalf("seed semester: %v", err)
	}
	return adminID, semesterID
}

// randomSuffix returns a short random hex string so repeated test runs do not
// collide on the unique email / semester-code constraints.
func randomSuffix() string {
	var b [8]byte
	_, _ = rand.Read(b[:])
	return hex.EncodeToString(b[:])
}

func TestRepoClaimNextSkipLocked(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	repo := reportingpg.NewRepo(pool)

	adminID, semesterID := seedAdminAndSemester(t, pool)

	created, err := repo.Create(ctx, domain.ReportExport{
		RequestedByAdminID: adminID,
		SemesterID:         &semesterID,
		ReportType:         domain.ReportAcceptedRequests,
		Format:             domain.FormatXLSX,
		Filters:            map[string]any{"shift": "DAY"},
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if created.Status != domain.StatusRequested {
		t.Fatalf("expected REQUESTED, got %s", created.Status)
	}

	// Claim the job: it must flip to PROCESSING and stamp started_at.
	claimed, err := repo.ClaimNext(ctx)
	if err != nil {
		t.Fatalf("ClaimNext: %v", err)
	}
	if claimed.ID != created.ID || claimed.Status != domain.StatusProcessing {
		t.Fatalf("unexpected claim: %+v", claimed)
	}
	if claimed.StartedAt == nil {
		t.Fatal("expected started_at to be set after claim")
	}

	// A second claim finds no more REQUESTED rows.
	if _, err := repo.ClaimNext(ctx); !errors.Is(err, app.ErrNoJob) {
		t.Fatalf("expected ErrNoJob on empty queue, got %v", err)
	}

	// Complete it and verify the read model.
	if err := repo.MarkCompleted(ctx, created.ID, "/tmp/report.xlsx"); err != nil {
		t.Fatalf("MarkCompleted: %v", err)
	}
	got, err := repo.Get(ctx, created.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Status != domain.StatusCompleted || got.FilePath == nil || *got.FilePath != "/tmp/report.xlsx" {
		t.Fatalf("unexpected completed export: %+v", got)
	}
	if got.CompletedAt == nil {
		t.Fatal("expected completed_at to be set")
	}

	list, err := repo.ListBySemester(ctx, semesterID)
	if err != nil {
		t.Fatalf("ListBySemester: %v", err)
	}
	if len(list) != 1 || list[0].ID != created.ID {
		t.Fatalf("expected one export for semester, got %d", len(list))
	}
}
