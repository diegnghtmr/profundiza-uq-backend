package postgres_test

import (
	"context"
	"errors"
	"fmt"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	identitypg "github.com/uniquindio/profundiza-uq/internal/identity/adapter/postgres"
	"github.com/uniquindio/profundiza-uq/internal/identity/app"
	"github.com/uniquindio/profundiza-uq/internal/platform/authn"
	platformpg "github.com/uniquindio/profundiza-uq/internal/platform/postgres"
	"github.com/uniquindio/profundiza-uq/migrations"
)

// TestMain applies migrations once and truncates the sessions table so each
// run starts with a clean slate. Other tables are left intact so seeds from
// parallel packages do not interfere.
func TestMain(m *testing.M) {
	if url := os.Getenv("TEST_DATABASE_URL"); url != "" {
		ctx := context.Background()
		if pool, err := platformpg.Connect(ctx, url); err == nil {
			_ = platformpg.RunMigrations(ctx, pool, migrations.FS)
			_, _ = pool.Exec(ctx, `TRUNCATE sessions`)
			pool.Close()
		}
	}
	os.Exit(m.Run())
}

// testPool opens a connection pool to the test database (skips when
// TEST_DATABASE_URL is not set) and ensures migrations are applied.
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

// TestSessionRepo_DeleteExpired verifies Fix #3: only expired sessions are
// removed; sessions still within their TTL are left untouched.
func TestSessionRepo_DeleteExpired(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	// Isolate this test from any leftover rows.
	if _, err := pool.Exec(ctx, `TRUNCATE sessions`); err != nil {
		t.Fatalf("truncate sessions: %v", err)
	}

	repo := identitypg.NewSessionRepo(pool)

	// Insert a session that has already expired (expires_at in the past).
	expired := app.SessionRecord{
		ID:          "sess-expired-integration-1",
		SubjectType: authn.SubjectStudent,
		SubjectID:   "00000000-0000-0000-0000-000000000001",
		Role:        authn.RoleStudent,
		CSRFToken:   "csrf-expired",
		ExpiresAt:   time.Now().Add(-24 * time.Hour),
	}
	if err := repo.Create(ctx, expired); err != nil {
		t.Fatalf("create expired session: %v", err)
	}

	// Insert a session that is still valid (expires_at in the future).
	valid := app.SessionRecord{
		ID:          "sess-valid-integration-1",
		SubjectType: authn.SubjectStudent,
		SubjectID:   "00000000-0000-0000-0000-000000000002",
		Role:        authn.RoleStudent,
		CSRFToken:   "csrf-valid",
		ExpiresAt:   time.Now().Add(24 * time.Hour),
	}
	if err := repo.Create(ctx, valid); err != nil {
		t.Fatalf("create valid session: %v", err)
	}

	// DeleteExpired should remove exactly the one expired row.
	n, err := repo.DeleteExpired(ctx)
	if err != nil {
		t.Fatalf("DeleteExpired: %v", err)
	}
	if n != 1 {
		t.Errorf("DeleteExpired: want 1 row deleted, got %d", n)
	}

	// Expired session must be gone.
	_, ok, err := repo.Get(ctx, expired.ID)
	if err != nil {
		t.Fatalf("get expired session: %v", err)
	}
	if ok {
		t.Error("expired session still present after DeleteExpired")
	}

	// Valid session must still be present.
	_, ok, err = repo.Get(ctx, valid.ID)
	if err != nil {
		t.Fatalf("get valid session: %v", err)
	}
	if !ok {
		t.Error("valid session was incorrectly removed by DeleteExpired")
	}
}

// TestStudentDocumentNumberUnique verifies Fix #5: migration 000002 adds a
// UNIQUE constraint on students.document_number so inserting two students with
// the same cédula fails with a PostgreSQL unique-violation (23505).
func TestStudentDocumentNumberUnique(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()

	// Use a process-unique suffix to avoid collisions across test runs.
	docNumber := fmt.Sprintf("DOC-UNIQ-%d", uniqueSuffix())
	email1 := fmt.Sprintf("uniq1-%d@uniquindio.edu.co", uniqueSuffix())
	email2 := fmt.Sprintf("uniq2-%d@uniquindio.edu.co", uniqueSuffix())

	// First student with this document_number must succeed.
	if _, err := pool.Exec(ctx,
		`INSERT INTO students (institutional_email, document_number, full_name, academic_shift)
		 VALUES ($1, $2, 'Student A', 'DAY')`,
		email1, docNumber,
	); err != nil {
		t.Fatalf("first insert: %v", err)
	}

	// Second student with the same document_number must be rejected.
	_, err := pool.Exec(ctx,
		`INSERT INTO students (institutional_email, document_number, full_name, academic_shift)
		 VALUES ($1, $2, 'Student B', 'DAY')`,
		email2, docNumber,
	)
	if err == nil {
		t.Fatal("want unique-violation error for duplicate document_number, got nil")
	}
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) || pgErr.Code != "23505" {
		t.Errorf("want PostgreSQL unique-violation (23505), got: %v", err)
	}
}

var suffixMu sync.Mutex
var suffixSeq int64

// uniqueSuffix returns a process-unique increasing integer for building
// collision-free test fixture values.
func uniqueSuffix() int64 {
	suffixMu.Lock()
	defer suffixMu.Unlock()
	suffixSeq++
	return suffixSeq
}
