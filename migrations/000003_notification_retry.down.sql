ALTER TABLE notifications DROP COLUMN IF EXISTS next_attempt_at;
ALTER TABLE notifications DROP COLUMN IF EXISTS attempt_count;

ALTER TABLE notifications DROP CONSTRAINT IF EXISTS notifications_delivery_status_check;
ALTER TABLE notifications ADD CONSTRAINT notifications_delivery_status_check
  CHECK (delivery_status IN ('PENDING','SENT','FAILED','CANCELLED'));
