-- Support decoupling SMTP delivery from the claiming transaction and add a
-- bounded retry with backoff for transient send failures:
--   * 'SENDING' is a new transient delivery_status the worker sets while a
--     row is claimed and the send is in flight, outside the claiming tx.
--   * attempt_count tracks how many delivery attempts have been made so the
--     worker can stop retrying and move a row to the terminal 'FAILED'
--     status after a bounded number of tries.
--   * next_attempt_at gates when a retried row becomes claimable again,
--     giving the retry a backoff instead of an immediate re-send.
--   * claimed_at is a lease timestamp, distinct from next_attempt_at (a
--     backoff schedule): it records when a row was flipped to 'SENDING' so a
--     reaper can detect a row whose worker died mid-send (crash, OOM,
--     redeploy) and reset it back to 'PENDING' instead of leaving it stuck
--     in 'SENDING' forever, which would otherwise be a silent, unrecoverable
--     notification loss.
ALTER TABLE notifications DROP CONSTRAINT IF EXISTS notifications_delivery_status_check;
ALTER TABLE notifications ADD CONSTRAINT notifications_delivery_status_check
  CHECK (delivery_status IN ('PENDING','SENDING','SENT','FAILED','CANCELLED'));

ALTER TABLE notifications ADD COLUMN attempt_count INT NOT NULL DEFAULT 0;
ALTER TABLE notifications ADD COLUMN next_attempt_at TIMESTAMPTZ;
ALTER TABLE notifications ADD COLUMN claimed_at TIMESTAMPTZ;
