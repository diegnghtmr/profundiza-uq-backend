-- The old check constraint (re-added below) does not allow 'SENDING'. Any row
-- still in that transient status at rollback time must be reset first, or
-- adding the constraint back would fail.
UPDATE notifications SET delivery_status = 'PENDING' WHERE delivery_status = 'SENDING';

ALTER TABLE notifications DROP COLUMN IF EXISTS claimed_at;
ALTER TABLE notifications DROP COLUMN IF EXISTS next_attempt_at;
ALTER TABLE notifications DROP COLUMN IF EXISTS attempt_count;

ALTER TABLE notifications DROP CONSTRAINT IF EXISTS notifications_delivery_status_check;
ALTER TABLE notifications ADD CONSTRAINT notifications_delivery_status_check
  CHECK (delivery_status IN ('PENDING','SENT','FAILED','CANCELLED'));
