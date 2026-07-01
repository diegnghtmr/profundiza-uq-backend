package notification

import (
	"context"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Sender delivers an email. Implemented by an SMTP adapter.
type Sender interface {
	Send(ctx context.Context, to, subject, body string) error
}

// Worker drains the email outbox: it claims PENDING EMAIL notifications, sends
// them, and records the delivery outcome. Multiple workers are safe — claiming
// uses FOR UPDATE SKIP LOCKED.
type Worker struct {
	pool     *pgxpool.Pool
	sender   Sender
	logger   *slog.Logger
	interval time.Duration
	batch    int
}

// NewWorker builds an outbox Worker.
func NewWorker(pool *pgxpool.Pool, sender Sender, logger *slog.Logger, interval time.Duration) *Worker {
	if interval <= 0 {
		interval = 5 * time.Second
	}
	return &Worker{pool: pool, sender: sender, logger: logger, interval: interval, batch: 20}
}

// Run polls until the context is cancelled.
func (w *Worker) Run(ctx context.Context) {
	ticker := time.NewTicker(w.interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := w.drain(ctx); err != nil {
				w.logger.ErrorContext(ctx, "outbox_drain_failed", slog.Any("error", err))
			}
		}
	}
}

// drain processes one batch of pending email notifications.
func (w *Worker) drain(ctx context.Context) error {
	tx, err := w.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	rows, err := tx.Query(ctx,
		`SELECT id, recipient_email, subject, body
		   FROM notifications
		  WHERE channel = 'EMAIL' AND delivery_status = 'PENDING'
		  ORDER BY created_at
		  LIMIT $1
		  FOR UPDATE SKIP LOCKED`, w.batch)
	if err != nil {
		return err
	}

	type job struct{ id, to, subject, body string }
	var jobs []job
	for rows.Next() {
		var j job
		if err := rows.Scan(&j.id, &j.to, &j.subject, &j.body); err != nil {
			rows.Close()
			return err
		}
		jobs = append(jobs, j)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}

	now := time.Now()
	for _, j := range jobs {
		if err := w.sender.Send(ctx, j.to, j.subject, j.body); err != nil {
			if _, e := tx.Exec(ctx,
				`UPDATE notifications SET delivery_status='FAILED', failed_at=$2, failure_reason=$3 WHERE id=$1`,
				j.id, now, err.Error()); e != nil {
				return e
			}
			w.logger.WarnContext(ctx, "notification_delivery_failed", slog.String("id", j.id), slog.Any("error", err))
			continue
		}
		if _, e := tx.Exec(ctx,
			`UPDATE notifications SET delivery_status='SENT', sent_at=$2 WHERE id=$1`, j.id, now); e != nil {
			return e
		}
	}
	return tx.Commit(ctx)
}
