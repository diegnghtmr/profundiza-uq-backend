// Package notification provides the database-backed outbox. Enqueue inserts
// PENDING notification rows inside the caller's transaction so that a business
// state change and the notifications it triggers commit atomically.
package notification

import (
	"context"

	"github.com/jackc/pgx/v5/pgconn"

	"github.com/uniquindio/profundiza-uq/internal/notification/domain"
)

// Execer is satisfied by *pgxpool.Pool and pgx.Tx.
type Execer interface {
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
}

// Message describes a notification to enqueue across channels.
type Message struct {
	RecipientUserID   string // empty => NULL
	RecipientEmail    string
	Type              string
	Subject           string
	Body              string
	RelatedEntityType string // empty => NULL
	RelatedEntityID   string // empty => NULL
}

// Enqueue writes both an in-app and an email notification in PENDING state. Pass
// the transaction that performed the originating change.
func Enqueue(ctx context.Context, ex Execer, m Message) error {
	for _, channel := range []string{domain.ChannelInApp, domain.ChannelEmail} {
		if _, err := ex.Exec(ctx,
			`INSERT INTO notifications
			   (recipient_user_id, recipient_email, type, channel, subject, body,
			    delivery_status, related_entity_type, related_entity_id)
			 VALUES (NULLIF($1,'')::uuid, $2, $3, $4, $5, $6, 'PENDING', NULLIF($7,''), NULLIF($8,'')::uuid)`,
			m.RecipientUserID, m.RecipientEmail, m.Type, channel, m.Subject, m.Body,
			m.RelatedEntityType, m.RelatedEntityID,
		); err != nil {
			return err
		}
	}
	return nil
}
