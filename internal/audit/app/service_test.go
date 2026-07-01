package app_test

import (
	"context"
	"testing"

	"github.com/uniquindio/profundiza-uq/internal/audit/app"
	"github.com/uniquindio/profundiza-uq/internal/audit/domain"
)

type fakeRepo struct {
	gotFilter app.ListFilter
	items     []domain.AuditEvent
}

func (f *fakeRepo) List(_ context.Context, filter app.ListFilter) ([]domain.AuditEvent, int, error) {
	f.gotFilter = filter
	return f.items, len(f.items), nil
}

func TestListNormalizesPaginationAndForwardsFilter(t *testing.T) {
	entity := "STUDENT"
	repo := &fakeRepo{items: []domain.AuditEvent{{ID: 1}, {ID: 2}}}
	svc := app.NewService(repo)
	page, err := svc.List(context.Background(), app.ListFilter{Page: 0, PageSize: 1000, EntityType: &entity})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if page.Page != 1 || page.PageSize != 100 {
		t.Errorf("pagination not normalized: page=%d size=%d", page.Page, page.PageSize)
	}
	if page.Total != 2 {
		t.Errorf("total = %d, want 2", page.Total)
	}
	if repo.gotFilter.EntityType == nil || *repo.gotFilter.EntityType != "STUDENT" {
		t.Error("entity type filter not forwarded to repository")
	}
}

func TestListReturnsEmptySliceNotNil(t *testing.T) {
	svc := app.NewService(&fakeRepo{items: nil})
	page, _ := svc.List(context.Background(), app.ListFilter{})
	if page.Items == nil {
		t.Error("items should be an empty slice, not nil")
	}
}
