// Package postgres implements the notification read Repository with pgx.
package postgres

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/uniquindio/profundiza-uq/internal/notification/domain"
)

// Repo is the pgx-backed notification repository.
type Repo struct{ pool *pgxpool.Pool }

// NewRepo builds a notification Repo.
func NewRepo(pool *pgxpool.Pool) *Repo { return &Repo{pool: pool} }

// ListInApp returns the recipient's in-app notifications with a total count.
func (r *Repo) ListInApp(ctx context.Context, recipientUserID string, page, pageSize int) ([]domain.Notification, int, error) {
	var total int
	if err := r.pool.QueryRow(ctx,
		`SELECT count(*) FROM notifications WHERE recipient_user_id = $1 AND channel = 'IN_APP'`,
		recipientUserID,
	).Scan(&total); err != nil {
		return nil, 0, err
	}

	rows, err := r.pool.Query(ctx,
		`SELECT id, recipient_user_id, recipient_email, type, channel, subject, body,
		        delivery_status, related_entity_type, related_entity_id, read_at, created_at,
		        sent_at, failed_at, failure_reason
		   FROM notifications
		  WHERE recipient_user_id = $1 AND channel = 'IN_APP'
		  ORDER BY created_at DESC
		  LIMIT $2 OFFSET $3`, recipientUserID, pageSize, (page-1)*pageSize)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	out := []domain.Notification{}
	for rows.Next() {
		var n domain.Notification
		if err := rows.Scan(&n.ID, &n.RecipientUserID, &n.RecipientEmail, &n.Type, &n.Channel, &n.Subject,
			&n.Body, &n.DeliveryStatus, &n.RelatedEntityType, &n.RelatedEntityID, &n.ReadAt, &n.CreatedAt,
			&n.SentAt, &n.FailedAt, &n.FailureReason); err != nil {
			return nil, 0, err
		}
		out = append(out, n)
	}
	return out, total, rows.Err()
}

// MarkRead stamps read_at for an unread notification owned by the recipient.
func (r *Repo) MarkRead(ctx context.Context, id, recipientUserID string, now time.Time) (bool, error) {
	tag, err := r.pool.Exec(ctx,
		`UPDATE notifications SET read_at = $3
		  WHERE id = $1 AND recipient_user_id = $2 AND channel = 'IN_APP' AND read_at IS NULL`,
		id, recipientUserID, now)
	if err != nil {
		return false, err
	}
	return tag.RowsAffected() > 0, nil
}

// UnreadCount returns the number of unread in-app notifications.
func (r *Repo) UnreadCount(ctx context.Context, recipientUserID string) (int, error) {
	var n int
	err := r.pool.QueryRow(ctx,
		`SELECT count(*) FROM notifications
		  WHERE recipient_user_id = $1 AND channel = 'IN_APP' AND read_at IS NULL`,
		recipientUserID,
	).Scan(&n)
	return n, err
}
