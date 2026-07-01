// Package app holds notification read/mark-read use cases for the in-app inbox.
package app

import (
	"context"
	"time"

	"github.com/uniquindio/profundiza-uq/internal/notification/domain"
)

// Repository is the notification read port.
type Repository interface {
	ListInApp(ctx context.Context, recipientUserID string, page, pageSize int) ([]domain.Notification, int, error)
	MarkRead(ctx context.Context, id, recipientUserID string, now time.Time) (bool, error)
	UnreadCount(ctx context.Context, recipientUserID string) (int, error)
}

// Service exposes notification use cases.
type Service struct{ repo Repository }

// NewService wires the notification service.
func NewService(repo Repository) *Service { return &Service{repo: repo} }

// ListInApp returns the recipient's in-app notifications, newest first.
func (s *Service) ListInApp(ctx context.Context, recipientUserID string, page, pageSize int) ([]domain.Notification, int, error) {
	if page < 1 {
		page = 1
	}
	if pageSize < 1 || pageSize > 100 {
		pageSize = 20
	}
	return s.repo.ListInApp(ctx, recipientUserID, page, pageSize)
}

// MarkRead marks one notification read for its owner. Returns false if it does
// not exist or is not owned by the recipient.
func (s *Service) MarkRead(ctx context.Context, id, recipientUserID string) (bool, error) {
	return s.repo.MarkRead(ctx, id, recipientUserID, time.Now())
}

// UnreadCount returns the recipient's unread in-app count.
func (s *Service) UnreadCount(ctx context.Context, recipientUserID string) (int, error) {
	return s.repo.UnreadCount(ctx, recipientUserID)
}
