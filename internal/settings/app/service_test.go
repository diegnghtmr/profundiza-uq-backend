package app_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/uniquindio/profundiza-uq/internal/settings/app"
	"github.com/uniquindio/profundiza-uq/internal/settings/domain"
)

type fakeRepo struct {
	upserts   []domain.UpsertSetting
	listItems []domain.GlobalSetting
}

func (f *fakeRepo) List(_ context.Context, _ app.ListFilter) ([]domain.GlobalSetting, int, error) {
	return f.listItems, len(f.listItems), nil
}

func (f *fakeRepo) Upsert(_ context.Context, in domain.UpsertSetting, _ app.Actor) (domain.GlobalSetting, error) {
	f.upserts = append(f.upserts, in)
	return domain.GlobalSetting{Key: in.Key, Value: in.Value}, nil
}

func TestUpsertRejectsInvalidWithoutHittingRepo(t *testing.T) {
	repo := &fakeRepo{}
	svc := app.NewService(repo)
	if _, err := svc.Upsert(context.Background(), domain.UpsertSetting{Key: "k", Value: json.RawMessage(`{}`), Reason: "x"}, app.Actor{}); err == nil {
		t.Fatal("short reason should be rejected")
	}
	if len(repo.upserts) != 0 {
		t.Error("repo.Upsert must not be called for invalid input")
	}
}

func TestUpsertPassesValid(t *testing.T) {
	repo := &fakeRepo{}
	svc := app.NewService(repo)
	s, err := svc.Upsert(context.Background(), domain.UpsertSetting{
		Key: "feature.flag", Value: json.RawMessage(`{"enabled":true}`), Reason: "rollout",
	}, app.Actor{Type: "ADMIN", ID: "a1"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if s.Key != "feature.flag" || len(repo.upserts) != 1 {
		t.Error("upsert did not reach the repository")
	}
}

func TestListNormalizesPagination(t *testing.T) {
	svc := app.NewService(&fakeRepo{listItems: []domain.GlobalSetting{{Key: "a"}}})
	page, err := svc.List(context.Background(), app.ListFilter{Page: 0, PageSize: 0})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if page.Page != 1 || page.PageSize != 20 || page.Total != 1 {
		t.Errorf("unexpected page: %+v", page)
	}
}
