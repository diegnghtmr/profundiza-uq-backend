package postgres_test

import (
	"context"
	"errors"
	"fmt"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/uniquindio/profundiza-uq/internal/enrollment/domain"
	platformpg "github.com/uniquindio/profundiza-uq/internal/platform/postgres"
	reviewpg "github.com/uniquindio/profundiza-uq/internal/review/adapter/postgres"
	"github.com/uniquindio/profundiza-uq/internal/review/app"
	"github.com/uniquindio/profundiza-uq/migrations"
)

// No TestMain: this package deliberately does NOT truncate. It shares the test
// database with other integration packages (enrollment, reporting) whose own
// TestMain issues `TRUNCATE ... CASCADE`; adding a second cascading truncate
// here would race with those when `go test ./...` runs the package binaries
// concurrently. Every fixture below is globally unique (per-process runID plus a
// monotonic suffix) and assertions only touch rows this package created, so
// leftover rows from previous runs are harmless. Migrations are applied by
// testPool.

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

// --- fixtures ---

type fixture struct {
	semesterID string
	offeringID string
	studentID  string
	adminID    string
}

// newFixture seeds an isolated semester/elective/offering, a student and an
// admin. Callers add groups and requests as needed.
func newFixture(t *testing.T, pool *pgxpool.Pool, studentShift string) fixture {
	t.Helper()
	var f fixture
	code := fmt.Sprintf("R-%d", randSuffix())
	mustScan(t, pool, &f.semesterID,
		`INSERT INTO semesters(code,name,starts_at,ends_at,status)
		 VALUES ($1,'test',now(),now()+interval '60 days','DRAFT') RETURNING id`, code)
	f.offeringID = seedOffering(t, pool, f.semesterID)
	mustScan(t, pool, &f.studentID,
		`INSERT INTO students(institutional_email,document_number,full_name,academic_shift)
		 VALUES ($1,$2,'Student',$3) RETURNING id`,
		fmt.Sprintf("s%d@uniquindio.edu.co", randSuffix()), fmt.Sprintf("DOC%d", randSuffix()), studentShift)
	// admin_users is not truncated between runs (see TestMain), so the email must
	// be unique across runs and across concurrent package processes; a per-process
	// runID plus the monotonic suffix guarantees that.
	mustScan(t, pool, &f.adminID,
		`INSERT INTO admin_users(institutional_email,full_name,role)
		 VALUES ($1,'Admin','ADMIN') RETURNING id`, fmt.Sprintf("a%d-%d@uniquindio.edu.co", runID, randSuffix()))
	return f
}

func seedOffering(t *testing.T, pool *pgxpool.Pool, semesterID string) string {
	t.Helper()
	var electiveID, offeringID string
	mustScan(t, pool, &electiveID,
		`INSERT INTO electives(name,area) VALUES ($1,'Area') RETURNING id`, fmt.Sprintf("E-%d", randSuffix()))
	mustScan(t, pool, &offeringID,
		`INSERT INTO elective_offerings(semester_id,elective_id) VALUES ($1,$2) RETURNING id`, semesterID, electiveID)
	return offeringID
}

func seedGroupIn(t *testing.T, pool *pgxpool.Pool, offeringID, shift string, capacity int) string {
	t.Helper()
	var id string
	mustScan(t, pool, &id,
		`INSERT INTO offering_groups(offering_id,group_code,shift,schedule_text,capacity)
		 VALUES ($1,$2,$3,'Mon 8-10',$4) RETURNING id`,
		offeringID, fmt.Sprintf("G-%d", randSuffix()), shift, capacity)
	return id
}

// seedRequest inserts an enrollment request in the given group/status directly.
func seedRequest(t *testing.T, pool *pgxpool.Pool, f fixture, offeringID, groupID, shift, offeringShift string, priority domain.PriorityGroup, status domain.RequestStatus) string {
	t.Helper()
	var id string
	mustScan(t, pool, &id,
		`INSERT INTO enrollment_requests
		   (semester_id, student_id, offering_id, offering_group_id,
		    student_shift, offering_shift, priority_group, status, idempotency_key)
		 VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9) RETURNING id`,
		f.semesterID, f.studentID, offeringID, groupID, shift, offeringShift,
		string(priority), string(status), fmt.Sprintf("idem-%d", randSuffix()))
	return id
}

func groupOf(t *testing.T, pool *pgxpool.Pool, requestID string) (groupID, status string) {
	t.Helper()
	if err := pool.QueryRow(context.Background(),
		`SELECT offering_group_id, status FROM enrollment_requests WHERE id=$1`, requestID,
	).Scan(&groupID, &status); err != nil {
		t.Fatalf("read request: %v", err)
	}
	return groupID, status
}

// --- tests ---

// TestDecide_CreateGroupAcceptanceReassignsGroup is the core fix: a waitlisted
// student in the original group is accepted INTO a different (admin-created)
// target group of the same offering. The decision must both set status=ACCEPTED
// and reassign offering_group_id to the target, atomically.
func TestDecide_CreateGroupAcceptanceReassignsGroup(t *testing.T) {
	pool := testPool(t)
	repo := reviewpg.NewRepo(pool)
	f := newFixture(t, pool, "DAY")

	original := seedGroupIn(t, pool, f.offeringID, "DAY", 1)
	target := seedGroupIn(t, pool, f.offeringID, "DAY", 5) // newly created group with room
	reqID := seedRequest(t, pool, f, f.offeringID, original, "DAY", "DAY",
		domain.PriorityWaitlistSameShift, domain.StatusWaitlistSameShift)

	res, err := repo.Decide(context.Background(), app.DecisionInput{
		RequestID:     reqID,
		AdminUserID:   f.adminID,
		DecisionType:  domain.DecisionCreateGroupAcceptance,
		Reason:        "accepted into the newly created group",
		TargetGroupID: target,
	})
	if err != nil {
		t.Fatalf("CREATE_GROUP_ACCEPTANCE should succeed, got %v", err)
	}
	if res.Request.Status != string(domain.StatusAccepted) {
		t.Errorf("status = %q, want ACCEPTED", res.Request.Status)
	}
	if res.Request.OfferingGroupID != target {
		t.Errorf("offering_group_id = %q, want target %q", res.Request.OfferingGroupID, target)
	}
	// Confirm the persisted row (not just the returned struct) was reassigned.
	gotGroup, gotStatus := groupOf(t, pool, reqID)
	if gotGroup != target || gotStatus != string(domain.StatusAccepted) {
		t.Errorf("persisted row: group=%q status=%q, want group=%q status=ACCEPTED", gotGroup, gotStatus, target)
	}
}

// TestDecide_CreateGroupAcceptanceRejectsCrossOffering verifies a student cannot
// be moved into a group of a DIFFERENT offering, and the request is untouched.
func TestDecide_CreateGroupAcceptanceRejectsCrossOffering(t *testing.T) {
	pool := testPool(t)
	repo := reviewpg.NewRepo(pool)
	f := newFixture(t, pool, "DAY")

	original := seedGroupIn(t, pool, f.offeringID, "DAY", 1)
	otherOffering := seedOffering(t, pool, f.semesterID)
	foreignGroup := seedGroupIn(t, pool, otherOffering, "DAY", 5)
	reqID := seedRequest(t, pool, f, f.offeringID, original, "DAY", "DAY",
		domain.PriorityWaitlistSameShift, domain.StatusWaitlistSameShift)

	_, err := repo.Decide(context.Background(), app.DecisionInput{
		RequestID:     reqID,
		AdminUserID:   f.adminID,
		DecisionType:  domain.DecisionCreateGroupAcceptance,
		Reason:        "trying to move across offerings",
		TargetGroupID: foreignGroup,
	})
	if !errors.Is(err, reviewpg.ErrTargetGroupOfferingMismatch) {
		t.Fatalf("cross-offering target should fail with ErrTargetGroupOfferingMismatch, got %v", err)
	}
	gotGroup, gotStatus := groupOf(t, pool, reqID)
	if gotGroup != original || gotStatus != string(domain.StatusWaitlistSameShift) {
		t.Errorf("request must be untouched: group=%q status=%q", gotGroup, gotStatus)
	}
}

// TestDecide_CreateGroupAcceptanceUnknownTarget verifies a non-existent target
// group is rejected as not found.
func TestDecide_CreateGroupAcceptanceUnknownTarget(t *testing.T) {
	pool := testPool(t)
	repo := reviewpg.NewRepo(pool)
	f := newFixture(t, pool, "DAY")

	original := seedGroupIn(t, pool, f.offeringID, "DAY", 1)
	reqID := seedRequest(t, pool, f, f.offeringID, original, "DAY", "DAY",
		domain.PriorityWaitlistSameShift, domain.StatusWaitlistSameShift)

	_, err := repo.Decide(context.Background(), app.DecisionInput{
		RequestID:     reqID,
		AdminUserID:   f.adminID,
		DecisionType:  domain.DecisionCreateGroupAcceptance,
		Reason:        "pointing at a group that does not exist",
		TargetGroupID: "00000000-0000-0000-0000-000000000000",
	})
	if !errors.Is(err, reviewpg.ErrTargetGroupNotFound) {
		t.Fatalf("unknown target group should fail with ErrTargetGroupNotFound, got %v", err)
	}
}

// TestDecide_CreateGroupAcceptanceMissingTarget verifies the domain rule is
// enforced end-to-end: no targetGroupId means ErrTargetGroupRequired.
func TestDecide_CreateGroupAcceptanceMissingTarget(t *testing.T) {
	pool := testPool(t)
	repo := reviewpg.NewRepo(pool)
	f := newFixture(t, pool, "DAY")

	original := seedGroupIn(t, pool, f.offeringID, "DAY", 1)
	reqID := seedRequest(t, pool, f, f.offeringID, original, "DAY", "DAY",
		domain.PriorityWaitlistSameShift, domain.StatusWaitlistSameShift)

	_, err := repo.Decide(context.Background(), app.DecisionInput{
		RequestID:    reqID,
		AdminUserID:  f.adminID,
		DecisionType: domain.DecisionCreateGroupAcceptance,
		Reason:       "no target group supplied",
	})
	if !errors.Is(err, domain.ErrTargetGroupRequired) {
		t.Fatalf("missing target group should fail with ErrTargetGroupRequired, got %v", err)
	}
}

// TestDecide_CreateGroupAcceptanceDuplicateActiveInTarget verifies the
// uq_active_request_per_group edge case: if the student already holds an active
// request in the target group, the reassignment must surface a typed, mapped
// error instead of leaking a raw unique-violation, and must not partially apply.
func TestDecide_CreateGroupAcceptanceDuplicateActiveInTarget(t *testing.T) {
	pool := testPool(t)
	repo := reviewpg.NewRepo(pool)
	f := newFixture(t, pool, "DAY")

	original := seedGroupIn(t, pool, f.offeringID, "DAY", 1)
	target := seedGroupIn(t, pool, f.offeringID, "DAY", 5)
	// The student is waitlisted in the original group AND already has an active
	// request in the target group.
	reqID := seedRequest(t, pool, f, f.offeringID, original, "DAY", "DAY",
		domain.PriorityWaitlistSameShift, domain.StatusWaitlistSameShift)
	_ = seedRequest(t, pool, f, f.offeringID, target, "DAY", "DAY",
		domain.PriorityWaitlistSameShift, domain.StatusWaitlistSameShift)

	_, err := repo.Decide(context.Background(), app.DecisionInput{
		RequestID:     reqID,
		AdminUserID:   f.adminID,
		DecisionType:  domain.DecisionCreateGroupAcceptance,
		Reason:        "student already active in the target group",
		TargetGroupID: target,
	})
	if !errors.Is(err, reviewpg.ErrDuplicateActiveInTargetGroup) {
		t.Fatalf("duplicate active in target should fail with ErrDuplicateActiveInTargetGroup, got %v", err)
	}
	// The original request must remain untouched (transaction rolled back).
	gotGroup, gotStatus := groupOf(t, pool, reqID)
	if gotGroup != original || gotStatus != string(domain.StatusWaitlistSameShift) {
		t.Errorf("request must be untouched after conflict: group=%q status=%q", gotGroup, gotStatus)
	}
}

// TestDecide_PlainAcceptDoesNotReassign is the regression guard: plain ACCEPT
// keeps the student in the original group; offering_group_id is never touched.
func TestDecide_PlainAcceptDoesNotReassign(t *testing.T) {
	pool := testPool(t)
	repo := reviewpg.NewRepo(pool)
	f := newFixture(t, pool, "DAY")

	original := seedGroupIn(t, pool, f.offeringID, "DAY", 5)
	reqID := seedRequest(t, pool, f, f.offeringID, original, "DAY", "DAY",
		domain.PriorityDirectSameShift, domain.StatusPendingReview)

	res, err := repo.Decide(context.Background(), app.DecisionInput{
		RequestID:    reqID,
		AdminUserID:  f.adminID,
		DecisionType: domain.DecisionAccept,
		Reason:       "earliest valid request by arrival order",
	})
	if err != nil {
		t.Fatalf("plain ACCEPT should succeed, got %v", err)
	}
	if res.Request.Status != string(domain.StatusAccepted) {
		t.Errorf("status = %q, want ACCEPTED", res.Request.Status)
	}
	if res.Request.OfferingGroupID != original {
		t.Errorf("offering_group_id changed to %q, want original %q", res.Request.OfferingGroupID, original)
	}
}

// --- helpers ---

func mustScan(t *testing.T, pool *pgxpool.Pool, dest any, sql string, args ...any) {
	t.Helper()
	if err := pool.QueryRow(context.Background(), sql, args...).Scan(dest); err != nil {
		t.Fatalf("seed query failed: %v\nSQL: %s", err, sql)
	}
}

// runID is unique per test-process so rows in tables that are NOT truncated
// between runs (admin_users) never collide across repeated or concurrent runs.
var runID = time.Now().UnixNano()

var suffixMu sync.Mutex
var suffixSeq int64

func randSuffix() int64 {
	suffixMu.Lock()
	defer suffixMu.Unlock()
	suffixSeq++
	return suffixSeq
}
