-- Drop the uniqueness guards. The deduplication in the up migration is not
-- reversible (the removed duplicate rows were exact copies).
DROP INDEX IF EXISTS uq_offering_prerequisites_active_name;
DROP INDEX IF EXISTS uq_prerequisites_active_name;
