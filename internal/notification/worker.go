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

// maxDeliveryAttempts bounds retries for a transient send failure. A row is
// moved to the terminal FAILED status once it has been attempted this many
// times, instead of retrying forever.
const maxDeliveryAttempts = 5

// leaseThreshold bounds how long a single row may stay in the transient
// SENDING status before the reaper (see reapStuckSending) assumes the worker
// that claimed it died mid-send (crash, OOM, redeploy) and resets it back to
// PENDING. It must stay safely above the longest a single row's own send can
// take — smtpx.DefaultTimeout is 10s, so 60s leaves ample margin — so a row
// that is genuinely still in flight in a live worker is never reaped out
// from under it.
//
// This is evaluated per row, not per batch: deliver() re-stamps claimed_at
// immediately before sending each row (see deliver), so the lease reflects
// when that specific row's send started rather than when the whole batch (up
// to Worker.batch rows) was claimed. Without the per-row re-stamp, sequential
// delivery of a large batch (each send bounded by smtpx.DefaultTimeout, so a
// full batch can legitimately take minutes) could push a late row in the
// batch past leaseThreshold while it is still queued behind earlier rows,
// letting another replica's reaper reclaim and double-send it.
//
// With N worker replicas this, combined with the ownership guards in
// deliver's terminal UPDATEs (WHERE delivery_status='SENDING' AND
// claimed_at=$ownClaim), makes concurrent workers safe: a row can only be
// finalized by the worker that currently holds its lease, and a reaped row
// can only be re-claimed after its true owner has genuinely stopped sending
// it for longer than leaseThreshold.
const leaseThreshold = 60 * time.Second

// Worker drains the email outbox: it claims PENDING EMAIL notifications,
// sends them, and records the delivery outcome. Multiple workers (replicas)
// are safe: claiming uses FOR UPDATE SKIP LOCKED so two workers never claim
// the same row, and delivery is protected by a per-row lease (claimed_at)
// that deliver re-stamps immediately before sending and then requires as an
// ownership token on every write for that row — see leaseThreshold and
// deliver for how this prevents a double-send even after a reap.
//
// Claiming a row and sending its email are deliberately split into two
// separate, short database statements (see claim and deliver) instead of
// sending while a claiming transaction is open. FOR UPDATE SKIP LOCKED holds
// row locks and a pooled connection for as long as the transaction stays
// open; doing the SMTP send inside that transaction would let a stalled
// relay stall the transaction, starving the connection pool for every other
// request path (submit, decide, everything) and blocking graceful shutdown.
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

// job is one claimed outbox row.
type job struct {
	id       string
	to       string
	subject  string
	body     string
	attempts int
}

// drain reaps any rows stuck in SENDING past their lease, claims a batch of
// pending rows, and delivers each one. Delivery happens after claiming has
// committed and released its connection, so a stalled send only ties up the
// goroutine running drain, not a pooled DB connection.
func (w *Worker) drain(ctx context.Context) error {
	if err := w.reapStuckSending(ctx); err != nil {
		return err
	}

	jobs, err := w.claim(ctx)
	if err != nil {
		return err
	}
	for _, j := range jobs {
		w.deliver(ctx, j)
	}
	return nil
}

// reapStuckSending resets rows that have been sitting in SENDING for longer
// than leaseThreshold back to PENDING, clearing claimed_at. A row only stays
// in SENDING between claim() committing and deliver() recording a terminal
// outcome (SENT/PENDING-retry/FAILED); if the process dies in that window
// (crash, OOM, redeploy — exactly the failure mode a stalled SMTP relay can
// trigger) the row would otherwise be claimed but never retried, since
// claim() only selects PENDING rows. Resetting it back to PENDING here makes
// it claimable again instead of silently lost.
func (w *Worker) reapStuckSending(ctx context.Context) error {
	cutoff := time.Now().Add(-leaseThreshold)
	tag, err := w.pool.Exec(ctx,
		`UPDATE notifications
		    SET delivery_status = 'PENDING', claimed_at = NULL
		  WHERE channel = 'EMAIL' AND delivery_status = 'SENDING' AND claimed_at < $1`,
		cutoff)
	if err != nil {
		return err
	}
	if n := tag.RowsAffected(); n > 0 {
		w.logger.WarnContext(ctx, "notification_stuck_sending_reaped", slog.Int64("count", n))
	}
	return nil
}

// claim opens a short transaction, selects up to batch eligible PENDING rows
// with FOR UPDATE SKIP LOCKED, flips them to SENDING so no other worker
// re-claims them while delivery is in flight, and commits — releasing the
// connection before any SMTP I/O happens.
func (w *Worker) claim(ctx context.Context) ([]job, error) {
	tx, err := w.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	rows, err := tx.Query(ctx,
		`SELECT id, recipient_email, subject, body, attempt_count
		   FROM notifications
		  WHERE channel = 'EMAIL' AND delivery_status = 'PENDING'
		    AND (next_attempt_at IS NULL OR next_attempt_at <= now())
		  ORDER BY created_at
		  LIMIT $1
		  FOR UPDATE SKIP LOCKED`, w.batch)
	if err != nil {
		return nil, err
	}

	var jobs []job
	for rows.Next() {
		var j job
		if err := rows.Scan(&j.id, &j.to, &j.subject, &j.body, &j.attempts); err != nil {
			rows.Close()
			return nil, err
		}
		jobs = append(jobs, j)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}

	for _, j := range jobs {
		if _, err := tx.Exec(ctx,
			`UPDATE notifications SET delivery_status='SENDING', claimed_at=now() WHERE id=$1`, j.id); err != nil {
			return nil, err
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return jobs, nil
}

// deliver sends one claimed job's email (outside any DB transaction) and
// records the outcome in a single follow-up statement — a lone Exec is
// already atomic, so no explicit transaction is needed and the pooled
// connection is only held for that one short statement.
//
// Before sending, deliver re-stamps claimed_at to "now" for this row alone
// (see leaseThreshold) and uses that exact value as an ownership token: every
// subsequent write for this row, including the terminal one, is guarded by
// WHERE delivery_status='SENDING' AND claimed_at=$ownClaim. If another
// replica's reaper has already reclaimed the row (because this worker sat on
// it past leaseThreshold, or crashed), the re-stamp or the terminal update
// affects zero rows and this call backs off instead of clobbering state that
// another worker now owns — closing the multi-replica double-send/clobber
// race.
//
// A transient send failure is retried with backoff (see retryBackoff)
// instead of being marked terminally FAILED, up to maxDeliveryAttempts.
func (w *Worker) deliver(ctx context.Context, j job) {
	ownClaim := time.Now()
	tag, err := w.pool.Exec(ctx,
		`UPDATE notifications SET claimed_at=$2 WHERE id=$1 AND delivery_status='SENDING'`,
		j.id, ownClaim)
	if err != nil {
		w.logger.ErrorContext(ctx, "notification_lease_restamp_failed", slog.String("id", j.id), slog.Any("error", err))
		return
	}
	if tag.RowsAffected() == 0 {
		// Lost ownership before the send even started (already reaped and
		// possibly re-claimed by another replica). Do not send — the new
		// owner will.
		w.logger.WarnContext(ctx, "notification_lease_lost_before_send", slog.String("id", j.id))
		return
	}

	now := time.Now()
	sendErr := w.sender.Send(ctx, j.to, j.subject, j.body)
	if sendErr == nil {
		res, err := w.pool.Exec(ctx,
			`UPDATE notifications SET delivery_status='SENT', sent_at=$2, claimed_at=NULL
			  WHERE id=$1 AND delivery_status='SENDING' AND claimed_at=$3`, j.id, now, ownClaim)
		if err != nil {
			w.logger.ErrorContext(ctx, "notification_mark_sent_failed", slog.String("id", j.id), slog.Any("error", err))
		} else if res.RowsAffected() == 0 {
			w.logger.WarnContext(ctx, "notification_lease_lost_after_send",
				slog.String("id", j.id), slog.String("outcome", "sent"))
		}
		return
	}

	attempts := j.attempts + 1
	if attempts >= maxDeliveryAttempts {
		res, err := w.pool.Exec(ctx,
			`UPDATE notifications SET delivery_status='FAILED', failed_at=$2, failure_reason=$3, attempt_count=$4, claimed_at=NULL
			  WHERE id=$1 AND delivery_status='SENDING' AND claimed_at=$5`,
			j.id, now, sendErr.Error(), attempts, ownClaim)
		if err != nil {
			w.logger.ErrorContext(ctx, "notification_mark_failed_failed", slog.String("id", j.id), slog.Any("error", err))
		} else if res.RowsAffected() == 0 {
			w.logger.WarnContext(ctx, "notification_lease_lost_after_send",
				slog.String("id", j.id), slog.String("outcome", "failed"))
		}
		w.logger.WarnContext(ctx, "notification_delivery_failed_terminal",
			slog.String("id", j.id), slog.Int("attempts", attempts), slog.Any("error", sendErr))
		return
	}

	next := now.Add(retryBackoff(attempts))
	res, err := w.pool.Exec(ctx,
		`UPDATE notifications SET delivery_status='PENDING', attempt_count=$2, failure_reason=$3, next_attempt_at=$4, claimed_at=NULL
		  WHERE id=$1 AND delivery_status='SENDING' AND claimed_at=$5`,
		j.id, attempts, sendErr.Error(), next, ownClaim)
	if err != nil {
		w.logger.ErrorContext(ctx, "notification_mark_retry_failed", slog.String("id", j.id), slog.Any("error", err))
	} else if res.RowsAffected() == 0 {
		w.logger.WarnContext(ctx, "notification_lease_lost_after_send",
			slog.String("id", j.id), slog.String("outcome", "retry_scheduled"))
	}
	w.logger.WarnContext(ctx, "notification_delivery_retry_scheduled",
		slog.String("id", j.id), slog.Int("attempts", attempts), slog.Time("nextAttemptAt", next), slog.Any("error", sendErr))
}

// retryBackoff returns the delay before a failed row becomes claimable again,
// given its 1-indexed attempt count: 30s, 60s, 120s, 240s, ... capped at 5
// minutes so retries never drift arbitrarily far into the future.
func retryBackoff(attempt int) time.Duration {
	const base = 30 * time.Second
	const maxBackoff = 5 * time.Minute
	if attempt < 1 {
		attempt = 1
	}
	if attempt > 10 { // guard against shift overflow for pathological inputs
		return maxBackoff
	}
	d := base << uint(attempt-1)
	if d <= 0 || d > maxBackoff {
		return maxBackoff
	}
	return d
}

// isLeaseExpired reports whether a row claimed at claimedAt is eligible for
// reapStuckSending to reset back to PENDING, i.e. whether its SENDING lease
// has run out as of now. It mirrors reapStuckSending's SQL condition
// (claimed_at < now - leaseThreshold) and is the pure decision at the heart
// of the reaper, factored out so it is unit-testable without a database.
func isLeaseExpired(claimedAt, now time.Time) bool {
	return now.Sub(claimedAt) > leaseThreshold
}
