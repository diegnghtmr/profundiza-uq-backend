-- Prevent duplicate ACTIVE prerequisites per elective (and per offering), and
-- clean up the duplicates that had accumulated. Root cause: the seed's plain
-- `ON CONFLICT DO NOTHING` was a silent no-op — the tables had no natural unique
-- key and the seed did not supply an explicit id, so every re-run inserted fresh
-- rows with new random UUIDs that could never conflict.
--
-- A partial unique index (WHERE status='ACTIVE') follows the codebase's
-- "one active X" pattern: it blocks a second active row with the same name while
-- still allowing that name to be re-added after the previous one is deactivated.

-- 1. Collapse existing ACTIVE duplicates, keeping the earliest-created row.
DELETE FROM prerequisites
WHERE id IN (
    SELECT id FROM (
        SELECT id, row_number() OVER (
            PARTITION BY elective_id, name ORDER BY created_at, id
        ) AS rn
        FROM prerequisites
        WHERE status = 'ACTIVE'
    ) ranked
    WHERE rn > 1
);

DELETE FROM offering_prerequisites
WHERE id IN (
    SELECT id FROM (
        SELECT id, row_number() OVER (
            PARTITION BY offering_id, name ORDER BY created_at, id
        ) AS rn
        FROM offering_prerequisites
        WHERE status = 'ACTIVE'
    ) ranked
    WHERE rn > 1
);

-- 2. Enforce uniqueness going forward.
CREATE UNIQUE INDEX uq_prerequisites_active_name
    ON prerequisites (elective_id, name)
    WHERE status = 'ACTIVE';

CREATE UNIQUE INDEX uq_offering_prerequisites_active_name
    ON offering_prerequisites (offering_id, name)
    WHERE status = 'ACTIVE';
