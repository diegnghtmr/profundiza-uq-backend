package app

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/uniquindio/profundiza-uq/internal/reporting/domain"
)

// fakeRepo is an in-memory Repository for worker unit tests. It hands each
// queued job out exactly once via ClaimNext and records terminal transitions.
type fakeRepo struct {
	mu        sync.Mutex
	queue     []domain.ReportExport
	completed map[string]string // id -> filePath
	failed    map[string]string // id -> reason
}

func newFakeRepo(jobs ...domain.ReportExport) *fakeRepo {
	return &fakeRepo{
		queue:     jobs,
		completed: map[string]string{},
		failed:    map[string]string{},
	}
}

func (f *fakeRepo) Create(context.Context, domain.ReportExport) (domain.ReportExport, error) {
	return domain.ReportExport{}, errors.New("not implemented")
}

func (f *fakeRepo) Get(context.Context, string) (domain.ReportExport, error) {
	return domain.ReportExport{}, ErrNotFound
}

func (f *fakeRepo) ListBySemester(context.Context, string) ([]domain.ReportExport, error) {
	return nil, nil
}

func (f *fakeRepo) ClaimNext(context.Context) (domain.ReportExport, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.queue) == 0 {
		return domain.ReportExport{}, ErrNoJob
	}
	job := f.queue[0]
	f.queue = f.queue[1:]
	job.Status = domain.StatusProcessing
	return job, nil
}

func (f *fakeRepo) MarkCompleted(_ context.Context, id, filePath string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.completed[id] = filePath
	return nil
}

func (f *fakeRepo) MarkFailed(_ context.Context, id, reason string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.failed[id] = reason
	return nil
}

// fakeGenerator records the jobs it was asked to generate and returns a
// configurable path or error.
type fakeGenerator struct {
	path string
	err  error
	seen []string
}

func (g *fakeGenerator) Generate(_ context.Context, e domain.ReportExport) (string, error) {
	g.seen = append(g.seen, e.ID)
	if g.err != nil {
		return "", g.err
	}
	return g.path, nil
}

func quietLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func TestWorkerClaimsGeneratesAndCompletes(t *testing.T) {
	job := domain.ReportExport{
		ID:         "report-1",
		ReportType: domain.ReportAcceptedRequests,
		Format:     domain.FormatXLSX,
		SemesterID: strptr("sem-1"),
		Status:     domain.StatusRequested,
	}
	repo := newFakeRepo(job)
	gen := &fakeGenerator{path: "/tmp/report-1.xlsx"}
	w := NewWorker(repo, gen, quietLogger(), time.Second)

	ctx := context.Background()

	processed, err := w.processOne(ctx)
	if err != nil {
		t.Fatalf("processOne returned error: %v", err)
	}
	if !processed {
		t.Fatal("expected a job to be processed")
	}
	if got := gen.seen; len(got) != 1 || got[0] != "report-1" {
		t.Fatalf("generator saw %v, want [report-1]", got)
	}
	if path, ok := repo.completed["report-1"]; !ok || path != "/tmp/report-1.xlsx" {
		t.Fatalf("job not marked completed with expected path; completed=%v", repo.completed)
	}
	if len(repo.failed) != 0 {
		t.Fatalf("job should not be marked failed; failed=%v", repo.failed)
	}

	// Queue is now empty: a second poll reports no work.
	processed, err = w.processOne(ctx)
	if err != nil {
		t.Fatalf("second processOne returned error: %v", err)
	}
	if processed {
		t.Fatal("expected no job on empty queue")
	}
}

func TestWorkerMarksFailedOnGeneratorError(t *testing.T) {
	job := domain.ReportExport{
		ID:         "report-2",
		ReportType: domain.ReportCapacity,
		Format:     domain.FormatPDF,
		SemesterID: strptr("sem-1"),
		Status:     domain.StatusRequested,
	}
	repo := newFakeRepo(job)
	gen := &fakeGenerator{err: errors.New("disk full")}
	w := NewWorker(repo, gen, quietLogger(), time.Second)

	processed, err := w.processOne(context.Background())
	if err != nil {
		t.Fatalf("processOne returned error: %v", err)
	}
	if !processed {
		t.Fatal("expected the job to be handled (moved to FAILED)")
	}
	if reason, ok := repo.failed["report-2"]; !ok || reason != "disk full" {
		t.Fatalf("job not marked failed with expected reason; failed=%v", repo.failed)
	}
	if len(repo.completed) != 0 {
		t.Fatalf("job should not be marked completed; completed=%v", repo.completed)
	}
}

func TestWorkerRunDrainsQueueThenStops(t *testing.T) {
	jobs := []domain.ReportExport{
		{ID: "a", ReportType: domain.ReportWaitlist, Format: domain.FormatXLSX, SemesterID: strptr("s"), Status: domain.StatusRequested},
		{ID: "b", ReportType: domain.ReportByGroup, Format: domain.FormatXLSX, SemesterID: strptr("s"), Status: domain.StatusRequested},
	}
	repo := newFakeRepo(jobs...)
	gen := &fakeGenerator{path: "/tmp/out.xlsx"}
	w := NewWorker(repo, gen, quietLogger(), 10*time.Millisecond)

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	w.Run(ctx) // returns when ctx expires

	if len(repo.completed) != 2 {
		t.Fatalf("expected 2 completed jobs, got %d (%v)", len(repo.completed), repo.completed)
	}
}

func strptr(s string) *string { return &s }
