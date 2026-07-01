package app_test

import (
	"context"
	"errors"
	"testing"

	"github.com/uniquindio/profundiza-uq/internal/enrollment/app"
	"github.com/uniquindio/profundiza-uq/internal/enrollment/domain"
)

// spyStore implements app.Store for service-level unit tests. ListMine returns
// a configurable set of views; Submit counts each call and optionally fails.
// Cancel and Get are unused by the SubmitBatch tests.
type spyStore struct {
	activeViews []app.RequestView
	submitCalls int
	submitErr   error
}

func (s *spyStore) Submit(_ context.Context, _ app.SubmitInput) (app.SubmittedRequest, error) {
	s.submitCalls++
	if s.submitErr != nil {
		return app.SubmittedRequest{}, s.submitErr
	}
	return app.SubmittedRequest{ID: "req-stub"}, nil
}

func (s *spyStore) Cancel(_ context.Context, _ app.CancelInput) (app.RequestView, error) {
	return app.RequestView{}, nil
}

func (s *spyStore) ListMine(_ context.Context, _, _ string) ([]app.RequestView, error) {
	return s.activeViews, nil
}

func (s *spyStore) Get(_ context.Context, _ string) (*app.RequestView, error) {
	return nil, nil
}

// makeActiveViews returns n RequestViews all with StatusSubmitted (active).
func makeActiveViews(n int) []app.RequestView {
	views := make([]app.RequestView, n)
	for i := range views {
		views[i] = app.RequestView{Status: domain.StatusSubmitted}
	}
	return views
}

// TestSubmitBatch_ExceedingCapRejectedUpfront verifies Fix #1: a batch that
// would push the student over MaxElectivesPerSemester is rejected BEFORE any
// store.Submit call, so the student's row count remains unchanged. Before the
// fix, SubmitBatch did not pre-validate the cap and would commit items 0..N-2
// before the Nth Submit returned ErrMaxElectivesReached.
func TestSubmitBatch_ExceedingCapRejectedUpfront(t *testing.T) {
	// Student already has 2 active requests; batch of 3 → total would be 5 > 4.
	store := &spyStore{activeViews: makeActiveViews(2)}
	svc := app.NewEnrollmentService(store)

	_, err := svc.SubmitBatch(context.Background(), "sem-1", "stu-1", "base-key",
		[]string{"g1", "g2", "g3"})

	if !errors.Is(err, domain.ErrMaxElectivesReached) {
		t.Fatalf("expected ErrMaxElectivesReached, got %v", err)
	}
	if store.submitCalls != 0 {
		t.Errorf("Submit must NOT be called when the batch is rejected upfront; got %d call(s)", store.submitCalls)
	}
}

// TestSubmitBatch_RejectsDuplicateGroupIDs verifies Fix #1(a): a batch
// containing the same offering-group ID more than once is rejected immediately
// with ErrDuplicateBatchItem and no store writes are performed.
func TestSubmitBatch_RejectsDuplicateGroupIDs(t *testing.T) {
	store := &spyStore{activeViews: makeActiveViews(0)}
	svc := app.NewEnrollmentService(store)

	_, err := svc.SubmitBatch(context.Background(), "sem-1", "stu-1", "base-key",
		[]string{"g1", "g1"})

	if !errors.Is(err, app.ErrDuplicateBatchItem) {
		t.Fatalf("expected ErrDuplicateBatchItem, got %v", err)
	}
	if store.submitCalls != 0 {
		t.Errorf("Submit must NOT be called when duplicates are detected; got %d call(s)", store.submitCalls)
	}
}

// TestSubmitBatch_ExactlyAtCapSucceeds verifies the boundary: a student with 2
// active requests submitting exactly 2 more (total == MaxElectivesPerSemester)
// must succeed, with Submit called once per item.
func TestSubmitBatch_ExactlyAtCapSucceeds(t *testing.T) {
	store := &spyStore{activeViews: makeActiveViews(2)}
	svc := app.NewEnrollmentService(store)

	results, err := svc.SubmitBatch(context.Background(), "sem-1", "stu-1", "base-key",
		[]string{"g1", "g2"})

	if err != nil {
		t.Fatalf("batch exactly at the cap should succeed, got %v", err)
	}
	if len(results) != 2 {
		t.Errorf("expected 2 results, got %d", len(results))
	}
	if store.submitCalls != 2 {
		t.Errorf("expected 2 Submit calls, got %d", store.submitCalls)
	}
}
