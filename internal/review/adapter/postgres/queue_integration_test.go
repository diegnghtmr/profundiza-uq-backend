package postgres_test

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/uniquindio/profundiza-uq/internal/enrollment/domain"
	reviewpg "github.com/uniquindio/profundiza-uq/internal/review/adapter/postgres"
	"github.com/uniquindio/profundiza-uq/internal/review/app"
)

// seedGroupWithStatus inserts an offering group with an explicit status so tests
// can assert that non-ACTIVE groups are excluded from the offering group list.
func seedGroupWithStatus(t *testing.T, pool *pgxpool.Pool, offeringID, shift, status string, capacity int) string {
	t.Helper()
	var id string
	mustScan(t, pool, &id,
		`INSERT INTO offering_groups(offering_id,group_code,shift,schedule_text,capacity,status)
		 VALUES ($1,$2,$3,'Mon 8-10',$4,$5) RETURNING id`,
		offeringID, uniqueKey("G-"), shift, capacity, status)
	return id
}

// TestListQueue_OfferingGroupsCarriesFullActiveGroupList is the contract fix that
// makes the frontend CREATE_GROUP_ACCEPTANCE target selector usable: a queue
// item's offering must expose ALL active groups of its offering (so the admin can
// target a sibling group the request is NOT in), scoped per offering. Before the
// fix the repo only knew the request's own group, so the offering group list had
// exactly one entry and the frontend had zero target options.
func TestListQueue_OfferingGroupsCarriesFullActiveGroupList(t *testing.T) {
	pool := testPool(t)
	repo := reviewpg.NewRepo(pool)
	f := newFixture(t, pool, "DAY")

	own := seedGroupIn(t, pool, f.offeringID, "DAY", 5)     // the request's own group
	sibling := seedGroupIn(t, pool, f.offeringID, "NIGHT", 5) // a sibling the request is NOT in
	// An INACTIVE group of the same offering must be excluded (only active groups).
	inactive := seedGroupWithStatus(t, pool, f.offeringID, "DAY", "INACTIVE", 5)
	// A group belonging to a DIFFERENT offering must never leak into this
	// offering's group list (per-offering scoping).
	otherOffering := seedOffering(t, pool, f.semesterID)
	foreign := seedGroupWithStatus(t, pool, otherOffering, "DAY", "ACTIVE", 5)

	reqID := seedRequest(t, pool, f, f.offeringID, own, "DAY", "DAY",
		domain.PriorityDirectSameShift, domain.StatusPendingReview)

	items, _, err := repo.ListQueue(context.Background(), app.QueueFilter{
		SemesterID: f.semesterID, Page: 1, PageSize: 50,
	})
	if err != nil {
		t.Fatalf("ListQueue: %v", err)
	}

	var item app.QueueItem
	found := false
	for _, it := range items {
		if it.Request.ID == reqID {
			item = it
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("seeded request %q not found in queue", reqID)
	}

	// item.Group (the request's own group) must stay exactly as before.
	if item.Group.ID != own {
		t.Errorf("item.Group.ID = %q, want request's own group %q", item.Group.ID, own)
	}

	got := map[string]bool{}
	for _, g := range item.OfferingGroups {
		got[g.ID] = true
	}
	if !got[own] {
		t.Errorf("offering groups must contain the request's own group %q; got %v", own, keys(got))
	}
	if !got[sibling] {
		t.Errorf("offering groups must contain the sibling group %q the request is not in; got %v", sibling, keys(got))
	}
	if got[inactive] {
		t.Errorf("offering groups must NOT contain the INACTIVE group %q", inactive)
	}
	if got[foreign] {
		t.Errorf("offering groups must NOT contain a group %q from a different offering", foreign)
	}
	if len(item.OfferingGroups) != 2 {
		t.Errorf("offering groups length = %d, want 2 (own + sibling), got %v", len(item.OfferingGroups), keys(got))
	}
}

func keys(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
