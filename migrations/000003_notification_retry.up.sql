-- Support decoupling SMTP delivery from the claiming transaction and add a
-- bounded retry with backoff for transient send failures:
--   * 'SENDING' is a new transient delivery_status the worker sets while a
--     row is claimed and the send is in flight, outside the claiming tx.
--   * attempt_count tracks how many delivery attempts have been made so the
--     worker can stop retrying and move a row to the terminal 'FAILED'
--     status after a bounded number of tries.
--   * next_attempt_at gates when a retried row becomes claimable again,
--     giving the retry a backoff instead of an immediate re-send.
ALTER TABLE notifications DROP CONSTRAINT IF EXISTS notifications_delivery_status_check;
ALTER TABLE notifications ADD CONSTRAINT notifications_delivery_status_check
  CHECK (delivery_status IN ('PENDING','SENDING','SENT','FAILED','CANCELLED'));

ALTER TABLE notifications ADD COLUMN attempt_count INT NOT NULL DEFAULT 0;
ALTER TABLE notifications ADD COLUMN next_attempt_at TIMESTAMPTZ;
