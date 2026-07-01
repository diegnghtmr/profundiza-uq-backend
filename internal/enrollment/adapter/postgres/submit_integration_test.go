package postgres_test

import (
	"context"
	"errors"
	"fmt"
	"os"
	"sync"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/uniquindio/profundiza-uq/internal/enrollment/app"
	enrollpg "github.com/uniquindio/profundiza-uq/internal/enrollment/adapter/postgres"
	"github.com/uniquindio/profundiza-uq/internal/enrollment/domain"
	platformpg "github.com/uniquindio/profundiza-uq/internal/platform/postgres"
	"github.com/uniquindio/profundiza-uq/migrations"
)

// TestMain gives the integration suite a clean slate: when a test database is
// configured it migrates and truncates the domain tables once, so fixtures are
// reproducible across repeated `go test` runs against the same container.
func TestMain(m *testing.M) {
	if url := os.Getenv("TEST_DATABASE_URL"); url != "" {
		ctx := context.Background()
		if pool, err := platformpg.Connect(ctx, url); err == nil {
			_ = platformpg.RunMigrations(ctx, pool, migrations.FS)
			_, _ = pool.Exec(ctx, `TRUNCATE
				enrollment_decisions, enrollment_requests, group_capacity_adjustments,
				offering_groups, offering_prerequisites, elective_offerings, prerequisites,
				electives, enrollment_windows, student_academic_records, students, semesters
				RESTART IDENTITY CASCADE`)
			pool.Close()
		}
	}
	os.Exit(m.Run())
}

// requires a reachable PostgreSQL. Set TEST_DATABASE_URL to run, e.g.
//
//	TEST_DATABASE_URL="postgres://postgres:test@localhost:55432/puq?sslmode=disable" go test ./...
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

// seedGroup creates an isolated semester/elective/offering/group with the given
// capacity and returns their ids.
func seedGroup(t *testing.T, pool *pgxpool.Pool, capacity int) (semesterID, offeringID, groupID string) {
	t.Helper()
	code := fmt.Sprintf("T-%d", randSuffix())
	// Fixtures use DRAFT to stay clear of the one-active-semester index; the
	// submission rules under test are independent of semester status.
	mustScan(t, pool, &semesterID,
		`INSERT INTO semesters(code,name,starts_at,ends_at,status)
		 VALUES ($1,'test',now(),now()+interval '60 days','DRAFT') RETURNING id`, code)
	// An active window is required for submissions.
	var windowID string
	mustScan(t, pool, &windowID,
		`INSERT INTO enrollment_windows(semester_id,name,starts_at,ends_at,status)
		 VALUES ($1,'open',now()-interval '1 hour',now()+interval '1 hour','ACTIVE') RETURNING id`, semesterID)
	var electiveID string
	mustScan(t, pool, &electiveID,
		`INSERT INTO electives(name,area) VALUES ('E','Area') RETURNING id`)
	mustScan(t, pool, &offeringID,
		`INSERT INTO elective_offerings(semester_id,elective_id) VALUES ($1,$2) RETURNING id`, semesterID, electiveID)
	mustScan(t, pool, &groupID,
		`INSERT INTO offering_groups(offering_id,group_code,shift,schedule_text,capacity)
		 VALUES ($1,'G1','DAY','Mon 8-10',$2) RETURNING id`, offeringID, capacity)
	return semesterID, offeringID, groupID
}

func seedStudent(t *testing.T, pool *pgxpool.Pool, shift string, n int) string {
	t.Helper()
	var id string
	mustScan(t, pool, &id,
		`INSERT INTO students(institutional_email,document_number,full_name,academic_shift)
		 VALUES ($1,$2,'Student',$3) RETURNING id`,
		fmt.Sprintf("s%d-%d@uniquindio.edu.co", randSuffix(), n), fmt.Sprintf("DOC%d-%d", randSuffix(), n), shift)
	return id
}

// TestSubmit_NoOverbookingUnderConcurrency is the critical no-overbooking test: with a
// single direct seat, many simultaneous same-shift submissions must yield
// exactly one DIRECT/PENDING_REVIEW and the rest on the same-shift waitlist,
// with unique arrival sequences. No overbooking, fair order.
func TestSubmit_NoOverbookingUnderConcurrency(t *testing.T) {
	pool := testPool(t)
	repo := enrollpg.NewSubmitRepo(pool)
	semesterID, _, groupID := seedGroup(t, pool, 1)

	const n = 25
	students := make([]string, n)
	for i := range students {
		students[i] = seedStudent(t, pool, "DAY", i)
	}

	var wg sync.WaitGroup
	results := make([]app.SubmittedRequest, n)
	errs := make([]error, n)
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			results[i], errs[i] = repo.Submit(context.Background(), app.SubmitInput{
				SemesterID:      semesterID,
				StudentID:       students[i],
				OfferingGroupID: groupID,
				IdempotencyKey:  fmt.Sprintf("key-%d-%d", randSuffix(), i),
			})
		}(i)
	}
	wg.Wait()

	direct, waitlist := 0, 0
	seen := map[int64]bool{}
	for i, res := range results {
		if errs[i] != nil {
			t.Fatalf("submit %d failed: %v", i, errs[i])
		}
		if seen[res.ArrivalSequence] {
			t.Fatalf("duplicate arrival sequence %d", res.ArrivalSequence)
		}
		seen[res.ArrivalSequence] = true
		switch res.Status {
		case domain.StatusPendingReview:
			direct++
		case domain.StatusWaitlistSameShift:
			waitlist++
		default:
			t.Fatalf("unexpected status %q", res.Status)
		}
	}
	if direct != 1 {
		t.Errorf("expected exactly 1 direct seat, got %d", direct)
	}
	if waitlist != n-1 {
		t.Errorf("expected %d waitlisted, got %d", n-1, waitlist)
	}
}

// TestSubmit_IsIdempotent verifies idempotency: replaying the same key returns the
// original request without creating a duplicate.
func TestSubmit_IsIdempotent(t *testing.T) {
	pool := testPool(t)
	repo := enrollpg.NewSubmitRepo(pool)
	semesterID, _, groupID := seedGroup(t, pool, 5)
	student := seedStudent(t, pool, "DAY", 0)
	key := fmt.Sprintf("idem-%d", randSuffix())

	in := app.SubmitInput{SemesterID: semesterID, StudentID: student, OfferingGroupID: groupID, IdempotencyKey: key}
	first, err := repo.Submit(context.Background(), in)
	if err != nil {
		t.Fatalf("first submit: %v", err)
	}
	second, err := repo.Submit(context.Background(), in)
	if err != nil {
		t.Fatalf("second submit: %v", err)
	}
	if first.ID != second.ID {
		t.Errorf("idempotent replay returned a different request: %s vs %s", first.ID, second.ID)
	}
	if !second.Existed {
		t.Error("second submit should be flagged as an idempotent replay")
	}
}

// TestSubmit_DuplicateActiveRequestRejected verifies that re-submitting a group
// the student already holds an active request for surfaces the typed
// app.ErrDuplicateActiveRequest (mapped to HTTP 409), not an opaque error — the
// uq_active_request_per_group index must never leak as a 500. A fresh
// idempotency key is used so this is a genuine duplicate, not a replay.
func TestSubmit_DuplicateActiveRequestRejected(t *testing.T) {
	pool := testPool(t)
	repo := enrollpg.NewSubmitRepo(pool)
	semesterID, _, groupID := seedGroup(t, pool, 5)
	student := seedStudent(t, pool, "DAY", 0)

	first := app.SubmitInput{SemesterID: semesterID, StudentID: student, OfferingGroupID: groupID, IdempotencyKey: fmt.Sprintf("dup-a-%d", randSuffix())}
	if _, err := repo.Submit(context.Background(), first); err != nil {
		t.Fatalf("first submit: %v", err)
	}

	second := app.SubmitInput{SemesterID: semesterID, StudentID: student, OfferingGroupID: groupID, IdempotencyKey: fmt.Sprintf("dup-b-%d", randSuffix())}
	_, err := repo.Submit(context.Background(), second)
	if !errors.Is(err, app.ErrDuplicateActiveRequest) {
		t.Fatalf("expected ErrDuplicateActiveRequest, got %v", err)
	}
}

// TestSubmit_OppositeShiftGoesLast verifies the opposite-shift rule: an opposite-shift student is
// always classified into the opposite-shift waitlist even with free seats.
func TestSubmit_OppositeShiftGoesLast(t *testing.T) {
	pool := testPool(t)
	repo := enrollpg.NewSubmitRepo(pool)
	semesterID, _, groupID := seedGroup(t, pool, 10) // DAY group with free seats
	night := seedStudent(t, pool, "NIGHT", 0)

	res, err := repo.Submit(context.Background(), app.SubmitInput{
		SemesterID: semesterID, StudentID: night, OfferingGroupID: groupID,
		IdempotencyKey: fmt.Sprintf("opp-%d", randSuffix()),
	})
	if err != nil {
		t.Fatalf("submit: %v", err)
	}
	if res.Status != domain.StatusWaitlistOppositeShift {
		t.Errorf("opposite-shift status = %q, want WAITLIST_OPPOSITE_SHIFT", res.Status)
	}
	if res.PriorityGroup != domain.PriorityWaitlistOppositeShift {
		t.Errorf("priority = %q, want WAITLIST_OPPOSITE_SHIFT", res.PriorityGroup)
	}
}

// TestSubmit_BlockedOutsideWindow verifies the active-window rule: with no active
// enrollment window, submission is rejected.
func TestSubmit_BlockedOutsideWindow(t *testing.T) {
	pool := testPool(t)
	repo := enrollpg.NewSubmitRepo(pool)

	// Seed a semester/elective/offering/group WITHOUT any active window.
	var semesterID, electiveID, offeringID, groupID string
	code := fmt.Sprintf("NW-%d", randSuffix())
	mustScan(t, pool, &semesterID,
		`INSERT INTO semesters(code,name,starts_at,ends_at,status) VALUES ($1,'nw',now(),now()+interval '60 days','DRAFT') RETURNING id`, code)
	mustScan(t, pool, &electiveID, `INSERT INTO electives(name,area) VALUES ('E','A') RETURNING id`)
	mustScan(t, pool, &offeringID, `INSERT INTO elective_offerings(semester_id,elective_id) VALUES ($1,$2) RETURNING id`, semesterID, electiveID)
	mustScan(t, pool, &groupID, `INSERT INTO offering_groups(offering_id,group_code,shift,schedule_text,capacity) VALUES ($1,'G1','DAY','Mon',5) RETURNING id`, offeringID)
	student := seedStudent(t, pool, "DAY", 0)

	_, err := repo.Submit(context.Background(), app.SubmitInput{
		SemesterID: semesterID, StudentID: student, OfferingGroupID: groupID,
		IdempotencyKey: fmt.Sprintf("nw-%d", randSuffix()),
	})
	if err == nil || err.Error() != enrollpg.ErrWindowClosed.Error() {
		t.Fatalf("expected ErrWindowClosed, got %v", err)
	}
}

// TestSubmit_StudentCapNotExceededUnderConcurrency is the per-semester cap concurrency
// test: a student who already holds 3 active requests fires 4 concurrent
// submissions to 4 distinct groups in the same semester. Exactly 1 must
// succeed (filling the 4th slot) and the other 3 must fail with
// ErrMaxElectivesReached. Without the FOR UPDATE on the student row the
// plain count(*) races under READ COMMITTED and may allow more than one.
func TestSubmit_StudentCapNotExceededUnderConcurrency(t *testing.T) {
	pool := testPool(t)
	repo := enrollpg.NewSubmitRepo(pool)

	// All 4 extra groups share the same semester so the per-semester cap counts across them.
	semesterID, _, firstGroupID := seedGroup(t, pool, 10)

	// Pre-seed 3 active requests for the student in the same semester to 3
	// different groups, bringing the student right up to the cap minus one.
	studentID := seedStudent(t, pool, "DAY", 0)

	// Reuse the enrollment window already created by seedGroup for semesterID.
	var windowID string
	mustScan(t, pool, &windowID,
		`SELECT id FROM enrollment_windows WHERE semester_id = $1 LIMIT 1`, semesterID)

	// Seed 3 additional groups in the same semester and insert SUBMITTED
	// requests directly so the student starts with activeCount = 3.
	for i := 0; i < 3; i++ {
		var electiveID, offeringID, groupID string
		mustScan(t, pool, &electiveID,
			`INSERT INTO electives(name,area) VALUES ($1,'Area') RETURNING id`,
			fmt.Sprintf("pre-elective-%d-%d", randSuffix(), i))
		mustScan(t, pool, &offeringID,
			`INSERT INTO elective_offerings(semester_id,elective_id) VALUES ($1,$2) RETURNING id`,
			semesterID, electiveID)
		mustScan(t, pool, &groupID,
			`INSERT INTO offering_groups(offering_id,group_code,shift,schedule_text,capacity)
			 VALUES ($1,'G1','DAY','Mon 8-10',10) RETURNING id`, offeringID)
		var reqID string
		mustScan(t, pool, &reqID,
			`INSERT INTO enrollment_requests
			   (semester_id, student_id, offering_id, offering_group_id, enrollment_window_id,
			    student_shift, offering_shift, priority_group, status, idempotency_key)
			 VALUES ($1,$2,$3,$4,$5,'DAY','DAY','DIRECT_SAME_SHIFT','SUBMITTED',$6)
			 RETURNING id`,
			semesterID, studentID, offeringID, groupID, windowID,
			fmt.Sprintf("pre-key-%d-%d", randSuffix(), i))
	}

	// Seed 3 more distinct target groups (the 4th target group is firstGroupID
	// from seedGroup above). The student will race to submit to all 4.
	extraGroupIDs := make([]string, 3)
	for i := range extraGroupIDs {
		var electiveID, offeringID string
		mustScan(t, pool, &electiveID,
			`INSERT INTO electives(name,area) VALUES ($1,'Area') RETURNING id`,
			fmt.Sprintf("race-elective-%d-%d", randSuffix(), i))
		mustScan(t, pool, &offeringID,
			`INSERT INTO elective_offerings(semester_id,elective_id) VALUES ($1,$2) RETURNING id`,
			semesterID, electiveID)
		mustScan(t, pool, &extraGroupIDs[i],
			`INSERT INTO offering_groups(offering_id,group_code,shift,schedule_text,capacity)
			 VALUES ($1,'G1','DAY','Mon 8-10',10) RETURNING id`, offeringID)
	}

	targetGroups := append([]string{firstGroupID}, extraGroupIDs...)

	const racers = 4
	type result struct {
		res app.SubmittedRequest
		err error
	}
	ch := make(chan result, racers)

	var wg sync.WaitGroup
	for i := 0; i < racers; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			r, e := repo.Submit(context.Background(), app.SubmitInput{
				SemesterID:      semesterID,
				StudentID:       studentID,
				OfferingGroupID: targetGroups[i],
				IdempotencyKey:  fmt.Sprintf("race-key-%d-%d", randSuffix(), i),
			})
			ch <- result{r, e}
		}(i)
	}
	wg.Wait()
	close(ch)

	successes, capErrors, otherErrors := 0, 0, 0
	for res := range ch {
		switch {
		case res.err == nil:
			successes++
		case errors.Is(res.err, domain.ErrMaxElectivesReached):
			capErrors++
		default:
			otherErrors++
			t.Logf("unexpected error: %v", res.err)
		}
	}

	if otherErrors > 0 {
		t.Fatalf("got %d unexpected errors (not ErrMaxElectivesReached)", otherErrors)
	}
	if successes != 1 {
		t.Errorf("per-semester cap concurrency: expected exactly 1 success, got %d (cap exceeded)", successes)
	}
	if capErrors != racers-1 {
		t.Errorf("per-semester cap concurrency: expected %d ErrMaxElectivesReached, got %d", racers-1, capErrors)
	}
}

// --- helpers ---

func mustScan(t *testing.T, pool *pgxpool.Pool, dest any, sql string, args ...any) {
	t.Helper()
	if err := pool.QueryRow(context.Background(), sql, args...).Scan(dest); err != nil {
		t.Fatalf("seed query failed: %v\nSQL: %s", err, sql)
	}
}

var suffixMu sync.Mutex
var suffixSeq int64

// randSuffix returns a process-unique increasing integer for building unique
// test fixtures without relying on randomness.
func randSuffix() int64 {
	suffixMu.Lock()
	defer suffixMu.Unlock()
	suffixSeq++
	return suffixSeq
}
