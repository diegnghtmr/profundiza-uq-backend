package app_test

import (
	"context"
	"errors"
	"testing"

	shared "github.com/uniquindio/profundiza-uq/internal/shared/domain"
	"github.com/uniquindio/profundiza-uq/internal/student/app"
	"github.com/uniquindio/profundiza-uq/internal/student/domain"
)

// fakeRepo is an in-memory Repository for use-case tests.
type fakeRepo struct {
	created   []domain.NewStudent
	listItems []domain.Student
	listTotal int
	getResult *domain.Student
	createErr error
}

func (f *fakeRepo) List(_ context.Context, filter app.ListFilter) ([]domain.Student, int, error) {
	f.listTotal = len(f.listItems)
	return f.listItems, f.listTotal, nil
}

func (f *fakeRepo) Create(_ context.Context, in domain.NewStudent, _ app.Actor) (domain.Student, error) {
	if f.createErr != nil {
		return domain.Student{}, f.createErr
	}
	f.created = append(f.created, in)
	return domain.Student{ID: "new", InstitutionalEmail: in.InstitutionalEmail, FullName: in.FullName, AcademicShift: in.AcademicShift, Status: domain.StatusActive}, nil
}

func (f *fakeRepo) Get(_ context.Context, _ string) (*domain.Student, error) {
	return f.getResult, nil
}

func (f *fakeRepo) Update(_ context.Context, _ string, _ domain.StudentPatch, _ app.Actor) (*domain.Student, error) {
	return f.getResult, nil
}

func (f *fakeRepo) ListAcademicRecords(_ context.Context, _ string, _ *string) ([]domain.AcademicRecord, error) {
	return nil, nil
}

func (f *fakeRepo) CreateAcademicRecord(_ context.Context, _ string, _ domain.NewAcademicRecord, _ app.Actor) (*domain.AcademicRecord, error) {
	return &domain.AcademicRecord{ID: "r1"}, nil
}

func TestCreateRejectsInvalidWithoutHittingRepo(t *testing.T) {
	repo := &fakeRepo{}
	svc := app.NewService(repo)
	_, err := svc.Create(context.Background(), domain.NewStudent{}, app.Actor{})
	if err == nil {
		t.Fatal("expected validation error")
	}
	var ve domain.ValidationError
	if !errors.As(err, &ve) {
		t.Fatalf("expected ValidationError, got %v", err)
	}
	if len(repo.created) != 0 {
		t.Error("repo.Create must not be called for invalid input")
	}
}

func TestListNormalizesPagination(t *testing.T) {
	repo := &fakeRepo{listItems: []domain.Student{{ID: "1"}}}
	svc := app.NewService(repo)
	page, err := svc.List(context.Background(), app.ListFilter{Page: 0, PageSize: 0})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if page.Page != 1 || page.PageSize != 20 {
		t.Errorf("defaults not applied: page=%d size=%d", page.Page, page.PageSize)
	}
	if page.Total != 1 {
		t.Errorf("total = %d, want 1", page.Total)
	}
}

func TestListClampsPageSize(t *testing.T) {
	svc := app.NewService(&fakeRepo{})
	page, _ := svc.List(context.Background(), app.ListFilter{Page: 2, PageSize: 500})
	if page.PageSize != 100 {
		t.Errorf("pageSize clamp failed: %d", page.PageSize)
	}
}

func TestGetMapsMissingToNotFound(t *testing.T) {
	svc := app.NewService(&fakeRepo{getResult: nil})
	if _, err := svc.Get(context.Background(), "x"); !errors.Is(err, app.ErrStudentNotFound) {
		t.Errorf("expected ErrStudentNotFound, got %v", err)
	}
}

func TestImportCountsAcceptedAndRejected(t *testing.T) {
	repo := &fakeRepo{}
	svc := app.NewService(repo)
	rows := []domain.NewStudent{
		{InstitutionalEmail: "a@uq.edu.co", DocumentNumber: "1", FullName: "A", AcademicShift: shared.ShiftDay},
		{}, // invalid -> rejected
		{InstitutionalEmail: "b@uq.edu.co", DocumentNumber: "2", FullName: "B", AcademicShift: shared.ShiftNight},
	}
	res, err := svc.Import(context.Background(), rows, app.Actor{Type: "ADMIN", ID: "admin1"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.AcceptedRows != 2 || res.RejectedRows != 1 {
		t.Errorf("accepted=%d rejected=%d, want 2/1", res.AcceptedRows, res.RejectedRows)
	}
	if len(res.Errors) != 1 {
		t.Errorf("expected 1 error message, got %d", len(res.Errors))
	}
}

func TestImportReportsRepoConflicts(t *testing.T) {
	repo := &fakeRepo{createErr: app.ErrEmailTaken}
	svc := app.NewService(repo)
	rows := []domain.NewStudent{
		{InstitutionalEmail: "a@uq.edu.co", DocumentNumber: "1", FullName: "A", AcademicShift: shared.ShiftDay},
	}
	res, _ := svc.Import(context.Background(), rows, app.Actor{})
	if res.AcceptedRows != 0 || res.RejectedRows != 1 {
		t.Errorf("conflict row should be rejected: accepted=%d rejected=%d", res.AcceptedRows, res.RejectedRows)
	}
}
