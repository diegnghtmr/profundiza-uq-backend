package app_test

import (
	"context"
	"errors"
	"testing"

	"github.com/uniquindio/profundiza-uq/internal/adminuser/app"
	"github.com/uniquindio/profundiza-uq/internal/adminuser/domain"
)

type fakeRepo struct {
	created   []domain.NewAdminUser
	listItems []domain.AdminUser
	getResult *domain.AdminUser
}

func (f *fakeRepo) List(_ context.Context, _ app.ListFilter) ([]domain.AdminUser, int, error) {
	return f.listItems, len(f.listItems), nil
}

func (f *fakeRepo) Create(_ context.Context, in domain.NewAdminUser, _ app.Actor) (domain.AdminUser, error) {
	f.created = append(f.created, in)
	return domain.AdminUser{ID: "new", InstitutionalEmail: in.InstitutionalEmail, FullName: in.FullName, Role: in.Role, Status: domain.StatusActive}, nil
}

func (f *fakeRepo) Get(_ context.Context, _ string) (*domain.AdminUser, error) {
	return f.getResult, nil
}

func (f *fakeRepo) Update(_ context.Context, _ string, _ domain.AdminUserPatch, _ app.Actor) (*domain.AdminUser, error) {
	return f.getResult, nil
}

func TestCreateRejectsInvalidWithoutHittingRepo(t *testing.T) {
	repo := &fakeRepo{}
	svc := app.NewService(repo)
	if _, err := svc.Create(context.Background(), domain.NewAdminUser{}, app.Actor{}); err == nil {
		t.Fatal("expected validation error")
	}
	if len(repo.created) != 0 {
		t.Error("repo.Create must not be called for invalid input")
	}
}

func TestCreatePassesValid(t *testing.T) {
	repo := &fakeRepo{}
	svc := app.NewService(repo)
	u, err := svc.Create(context.Background(), domain.NewAdminUser{
		InstitutionalEmail: "a@uq.edu.co", FullName: "Admin", Role: domain.RoleSuperAdmin,
	}, app.Actor{Type: "ADMIN", ID: "x"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if u.ID != "new" || len(repo.created) != 1 {
		t.Error("create did not reach the repository")
	}
}

func TestListNormalizesPagination(t *testing.T) {
	svc := app.NewService(&fakeRepo{listItems: []domain.AdminUser{{ID: "1"}}})
	page, err := svc.List(context.Background(), app.ListFilter{Page: -3, PageSize: 9999})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if page.Page != 1 || page.PageSize != 100 || page.Total != 1 {
		t.Errorf("unexpected page: %+v", page)
	}
}

func TestGetMapsMissingToNotFound(t *testing.T) {
	svc := app.NewService(&fakeRepo{getResult: nil})
	if _, err := svc.Get(context.Background(), "x"); !errors.Is(err, app.ErrNotFound) {
		t.Errorf("expected ErrNotFound, got %v", err)
	}
}
