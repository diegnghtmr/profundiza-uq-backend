-- Add a unique constraint on students.document_number so that each
-- government-issued document number (cédula) can only appear once.
-- Existing data must have distinct values before applying this migration.
ALTER TABLE students ADD CONSTRAINT uq_student_document UNIQUE (document_number);
