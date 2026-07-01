-- Profundiza UQ — initial schema.
-- Integrity rules (TRD §11) are enforced at the database level so that
-- consistency does not depend on application code alone. Enum-like columns use
-- TEXT + CHECK constraints (pragmatic: easy to evolve, sqlc-friendly).
-- All timestamps are stored in UTC.

CREATE EXTENSION IF NOT EXISTS "pgcrypto"; -- gen_random_uuid()

-- ---------------------------------------------------------------------------
-- Identity
-- ---------------------------------------------------------------------------

CREATE TABLE students (
    id                                     UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    institutional_email                    TEXT NOT NULL UNIQUE,
    document_number                        TEXT NOT NULL,
    full_name                              TEXT NOT NULL,
    academic_shift                         TEXT NOT NULL CHECK (academic_shift IN ('DAY','NIGHT')),
    status                                 TEXT NOT NULL DEFAULT 'ACTIVE' CHECK (status IN ('ACTIVE','INACTIVE')),
    completed_professional_electives_count INT  NOT NULL DEFAULT 0 CHECK (completed_professional_electives_count >= 0),
    created_at                             TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at                             TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE admin_users (
    id                  UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    institutional_email TEXT NOT NULL UNIQUE,
    full_name           TEXT NOT NULL,
    role                TEXT NOT NULL CHECK (role IN ('ADMIN','SUPER_ADMIN')),
    status              TEXT NOT NULL DEFAULT 'ACTIVE' CHECK (status IN ('ACTIVE','INACTIVE')),
    created_at          TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at          TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- Passwordless one-time codes / magic links. Tokens are stored hashed.
CREATE TABLE login_challenges (
    id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    email         TEXT NOT NULL,
    code_hash     TEXT NOT NULL,
    expires_at    TIMESTAMPTZ NOT NULL,
    consumed_at   TIMESTAMPTZ,
    attempts      INT NOT NULL DEFAULT 0,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX idx_login_challenges_email ON login_challenges (email, created_at DESC);

CREATE TABLE sessions (
    id            TEXT PRIMARY KEY,                 -- opaque session id stored in HttpOnly cookie
    subject_type  TEXT NOT NULL CHECK (subject_type IN ('STUDENT','ADMIN')),
    subject_id    UUID NOT NULL,
    role          TEXT NOT NULL CHECK (role IN ('STUDENT','ADMIN','SUPER_ADMIN')),
    csrf_token    TEXT NOT NULL,
    expires_at    TIMESTAMPTZ NOT NULL,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX idx_sessions_subject ON sessions (subject_type, subject_id);

-- ---------------------------------------------------------------------------
-- Semester & enrollment windows
-- ---------------------------------------------------------------------------

CREATE TABLE semesters (
    id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    code       TEXT NOT NULL UNIQUE,
    name       TEXT NOT NULL,
    starts_at  TIMESTAMPTZ NOT NULL,
    ends_at    TIMESTAMPTZ NOT NULL,
    status     TEXT NOT NULL DEFAULT 'DRAFT' CHECK (status IN ('DRAFT','ACTIVE','CLOSED')),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT semester_dates_valid CHECK (ends_at > starts_at)
);
-- At most one ACTIVE semester at a time.
CREATE UNIQUE INDEX uq_one_active_semester ON semesters ((status)) WHERE status = 'ACTIVE';

CREATE TABLE enrollment_windows (
    id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    semester_id  UUID NOT NULL REFERENCES semesters(id) ON DELETE CASCADE,
    name         TEXT NOT NULL,
    starts_at    TIMESTAMPTZ NOT NULL,
    ends_at      TIMESTAMPTZ NOT NULL,
    target_shift TEXT CHECK (target_shift IN ('DAY','NIGHT')),  -- NULL = all shifts
    status       TEXT NOT NULL DEFAULT 'ACTIVE' CHECK (status IN ('ACTIVE','INACTIVE','CLOSED')),
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT window_dates_valid CHECK (ends_at > starts_at)
);
CREATE INDEX idx_windows_semester ON enrollment_windows (semester_id);

-- ---------------------------------------------------------------------------
-- Catalog: electives, prerequisites, offerings, groups
-- ---------------------------------------------------------------------------

CREATE TABLE electives (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name        TEXT NOT NULL,
    area        TEXT NOT NULL,
    description TEXT NOT NULL DEFAULT '',
    status      TEXT NOT NULL DEFAULT 'ACTIVE' CHECK (status IN ('ACTIVE','INACTIVE','CLOSED')),
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE prerequisites (
    id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    elective_id  UUID NOT NULL REFERENCES electives(id) ON DELETE CASCADE,
    name         TEXT NOT NULL,
    description  TEXT NOT NULL DEFAULT '',
    plan_type    TEXT,
    status       TEXT NOT NULL DEFAULT 'ACTIVE' CHECK (status IN ('ACTIVE','INACTIVE','CLOSED')),
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at   TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX idx_prerequisites_elective ON prerequisites (elective_id);

CREATE TABLE elective_offerings (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    semester_id UUID NOT NULL REFERENCES semesters(id) ON DELETE CASCADE,
    elective_id UUID NOT NULL REFERENCES electives(id) ON DELETE RESTRICT,
    status      TEXT NOT NULL DEFAULT 'ACTIVE' CHECK (status IN ('ACTIVE','INACTIVE','CLOSED')),
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT uq_offering_per_semester UNIQUE (semester_id, elective_id)
);

CREATE TABLE offering_prerequisites (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    offering_id     UUID NOT NULL REFERENCES elective_offerings(id) ON DELETE CASCADE,
    prerequisite_id UUID REFERENCES prerequisites(id) ON DELETE SET NULL,
    name            TEXT NOT NULL,
    description     TEXT NOT NULL DEFAULT '',
    plan_type       TEXT,
    source          TEXT NOT NULL CHECK (source IN ('ELECTIVE_DEFAULT','OFFERING_SPECIFIC')),
    status          TEXT NOT NULL DEFAULT 'ACTIVE' CHECK (status IN ('ACTIVE','INACTIVE','CLOSED')),
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX idx_offering_prereqs_offering ON offering_prerequisites (offering_id);

CREATE TABLE offering_groups (
    id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    offering_id  UUID NOT NULL REFERENCES elective_offerings(id) ON DELETE CASCADE,
    group_code   TEXT NOT NULL,
    shift        TEXT NOT NULL CHECK (shift IN ('DAY','NIGHT')),
    teacher_name TEXT,
    schedule_text TEXT NOT NULL,
    capacity     INT NOT NULL CHECK (capacity >= 0),
    status       TEXT NOT NULL DEFAULT 'ACTIVE' CHECK (status IN ('ACTIVE','INACTIVE','CLOSED')),
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT uq_group_code_per_offering UNIQUE (offering_id, group_code)
);
CREATE INDEX idx_groups_offering ON offering_groups (offering_id);

-- ---------------------------------------------------------------------------
-- Student academic records (manual support data)
-- ---------------------------------------------------------------------------

CREATE TABLE student_academic_records (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    student_id  UUID NOT NULL REFERENCES students(id) ON DELETE CASCADE,
    semester_id UUID REFERENCES semesters(id) ON DELETE SET NULL,
    notes       TEXT NOT NULL DEFAULT '',
    source      TEXT NOT NULL DEFAULT 'MANUAL',
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX idx_academic_records_student ON student_academic_records (student_id);

-- ---------------------------------------------------------------------------
-- Enrollment requests & decisions
-- ---------------------------------------------------------------------------

-- Server-generated official arrival order (BR-003). A global monotonic sequence
-- preserves order within any semester (a subsequence stays ordered).
CREATE SEQUENCE enrollment_arrival_seq;

CREATE TABLE enrollment_requests (
    id                  UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    semester_id         UUID NOT NULL REFERENCES semesters(id) ON DELETE RESTRICT,
    student_id          UUID NOT NULL REFERENCES students(id) ON DELETE RESTRICT,
    offering_id         UUID NOT NULL REFERENCES elective_offerings(id) ON DELETE RESTRICT,
    offering_group_id   UUID NOT NULL REFERENCES offering_groups(id) ON DELETE RESTRICT,
    enrollment_window_id UUID REFERENCES enrollment_windows(id) ON DELETE SET NULL,
    student_shift       TEXT NOT NULL CHECK (student_shift IN ('DAY','NIGHT')),
    offering_shift      TEXT NOT NULL CHECK (offering_shift IN ('DAY','NIGHT')),
    priority_group      TEXT NOT NULL CHECK (priority_group IN ('DIRECT_SAME_SHIFT','WAITLIST_SAME_SHIFT','WAITLIST_OPPOSITE_SHIFT')),
    status              TEXT NOT NULL CHECK (status IN (
                            'SUBMITTED','PENDING_REVIEW','WAITLIST_SAME_SHIFT','WAITLIST_OPPOSITE_SHIFT',
                            'ACCEPTED','REJECTED','CANCELLED_BY_STUDENT','CANCELLED_BY_ADMIN')),
    arrival_sequence    BIGINT NOT NULL DEFAULT nextval('enrollment_arrival_seq'),
    idempotency_key     TEXT NOT NULL,
    submitted_at        TIMESTAMPTZ NOT NULL DEFAULT now(),
    cancelled_at        TIMESTAMPTZ,
    created_at          TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at          TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- Idempotency (BR-013): the same key for a student in a semester returns the
-- same request instead of duplicating it.
CREATE UNIQUE INDEX uq_request_idempotency
    ON enrollment_requests (student_id, semester_id, idempotency_key);

-- A student cannot hold two ACTIVE requests for the same group in a semester,
-- but cancelled/rejected history is preserved (TRD §11.2).
CREATE UNIQUE INDEX uq_active_request_per_group
    ON enrollment_requests (student_id, semester_id, offering_group_id)
    WHERE status IN ('SUBMITTED','PENDING_REVIEW','WAITLIST_SAME_SHIFT','WAITLIST_OPPOSITE_SHIFT','ACCEPTED');

CREATE INDEX idx_requests_group_status ON enrollment_requests (offering_group_id, status);
CREATE INDEX idx_requests_review_order ON enrollment_requests (offering_group_id, priority_group, arrival_sequence);
CREATE INDEX idx_requests_student ON enrollment_requests (student_id, semester_id);

-- Append-only decision history.
CREATE TABLE enrollment_decisions (
    id                    UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    enrollment_request_id UUID NOT NULL REFERENCES enrollment_requests(id) ON DELETE RESTRICT,
    admin_user_id         UUID NOT NULL REFERENCES admin_users(id) ON DELETE RESTRICT,
    decision_type         TEXT NOT NULL CHECK (decision_type IN (
                              'ACCEPT','REJECT','ADMIN_CANCEL','MOVE_TO_REVIEW',
                              'CREATE_GROUP_ACCEPTANCE','CAPACITY_ADJUSTMENT_ACCEPTANCE')),
    previous_status       TEXT NOT NULL,
    new_status            TEXT NOT NULL,
    reason                TEXT NOT NULL CHECK (length(btrim(reason)) >= 3),
    created_at            TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX idx_decisions_request ON enrollment_decisions (enrollment_request_id, created_at);

-- Capacity adjustments audit trail for groups.
CREATE TABLE group_capacity_adjustments (
    id               UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    offering_group_id UUID NOT NULL REFERENCES offering_groups(id) ON DELETE CASCADE,
    admin_user_id    UUID NOT NULL REFERENCES admin_users(id) ON DELETE RESTRICT,
    previous_capacity INT NOT NULL,
    new_capacity     INT NOT NULL CHECK (new_capacity >= 0),
    reason           TEXT NOT NULL CHECK (length(btrim(reason)) >= 3),
    created_at       TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- ---------------------------------------------------------------------------
-- Notifications (DB-backed outbox) & reports
-- ---------------------------------------------------------------------------

CREATE TABLE notifications (
    id                  UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    recipient_user_id   UUID,
    recipient_email     TEXT NOT NULL,
    type                TEXT NOT NULL,
    channel             TEXT NOT NULL CHECK (channel IN ('EMAIL','IN_APP')),
    subject             TEXT NOT NULL,
    body                TEXT NOT NULL,
    delivery_status     TEXT NOT NULL DEFAULT 'PENDING' CHECK (delivery_status IN ('PENDING','SENT','FAILED','CANCELLED')),
    related_entity_type TEXT,
    related_entity_id   UUID,
    read_at             TIMESTAMPTZ,
    created_at          TIMESTAMPTZ NOT NULL DEFAULT now(),
    sent_at             TIMESTAMPTZ,
    failed_at           TIMESTAMPTZ,
    failure_reason      TEXT
);
CREATE INDEX idx_notifications_outbox ON notifications (delivery_status, created_at) WHERE delivery_status = 'PENDING';
CREATE INDEX idx_notifications_recipient ON notifications (recipient_user_id, created_at DESC);

CREATE TABLE report_exports (
    id                       UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    requested_by_admin_user_id UUID NOT NULL REFERENCES admin_users(id) ON DELETE RESTRICT,
    semester_id              UUID REFERENCES semesters(id) ON DELETE SET NULL,
    report_type              TEXT NOT NULL,
    format                   TEXT NOT NULL CHECK (format IN ('XLSX','PDF')),
    status                   TEXT NOT NULL DEFAULT 'REQUESTED' CHECK (status IN ('REQUESTED','PROCESSING','COMPLETED','FAILED','EXPIRED')),
    filters_json             JSONB NOT NULL DEFAULT '{}'::jsonb,
    file_path                TEXT,
    failure_reason           TEXT,
    requested_at             TIMESTAMPTZ NOT NULL DEFAULT now(),
    started_at               TIMESTAMPTZ,
    completed_at             TIMESTAMPTZ
);
CREATE INDEX idx_reports_queue ON report_exports (status, requested_at) WHERE status = 'REQUESTED';

-- ---------------------------------------------------------------------------
-- Global settings & audit
-- ---------------------------------------------------------------------------

CREATE TABLE global_settings (
    key                      TEXT PRIMARY KEY,
    value_json               JSONB NOT NULL,
    description              TEXT NOT NULL DEFAULT '',
    updated_by_admin_user_id UUID REFERENCES admin_users(id) ON DELETE SET NULL,
    updated_at               TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- Append-only audit events (BR-012, TRD §11.3).
CREATE TABLE audit_events (
    id                 BIGSERIAL PRIMARY KEY,
    actor_type         TEXT NOT NULL,
    actor_id           UUID,
    action             TEXT NOT NULL,
    entity_type        TEXT NOT NULL,
    entity_id          TEXT,
    previous_value_json JSONB,
    new_value_json     JSONB,
    reason             TEXT,
    ip_address         TEXT,
    user_agent         TEXT,
    created_at         TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX idx_audit_entity ON audit_events (entity_type, entity_id, created_at DESC);
CREATE INDEX idx_audit_actor ON audit_events (actor_id, created_at DESC);
