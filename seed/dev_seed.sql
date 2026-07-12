-- Development seed for Profundiza UQ. Idempotent: safe to run repeatedly.
-- Apply with:  docker compose exec -T postgres psql -U postgres -d profundiza_uq < backend/seed/dev_seed.sql
-- (or against the local DB).  Login uses OTP — in development the code is printed
-- in the API log line "login_code_issued_dev".

-- Users -----------------------------------------------------------------------
-- The *@uniquindio.edu.co "demo" rows back the LoginPage "jump straight in"
-- shortcuts (student@ / admin@ / superadmin@); keep them in sync with
-- frontend/src/features/auth/pages/LoginPage.tsx DEV_EMAILS.
INSERT INTO admin_users (id, institutional_email, full_name, role) VALUES
  ('a0000000-0000-0000-0000-000000000001', 'claudia@uniquindio.edu.co',    'Claudia Marín',    'ADMIN'),
  ('a0000000-0000-0000-0000-000000000002', 'super@uniquindio.edu.co',      'Super Admin',      'SUPER_ADMIN'),
  ('a0000000-0000-0000-0000-0000000000aa', 'admin@uniquindio.edu.co',      'Admin Demo',       'ADMIN'),
  ('a0000000-0000-0000-0000-0000000000bb', 'superadmin@uniquindio.edu.co', 'Super Admin Demo', 'SUPER_ADMIN')
ON CONFLICT (institutional_email) DO NOTHING;

INSERT INTO students (id, institutional_email, document_number, full_name, academic_shift) VALUES
  ('50000000-0000-0000-0000-000000000001', 'daniela@uniquindio.edu.co', '1094000001', 'Daniela Restrepo', 'DAY'),
  ('50000000-0000-0000-0000-000000000002', 'carlos@uniquindio.edu.co',  '1094000002', 'Carlos Gómez',     'NIGHT'),
  ('50000000-0000-0000-0000-000000000003', 'lucia@uniquindio.edu.co',   '1094000003', 'Lucía Morales',    'DAY'),
  ('50000000-0000-0000-0000-0000000000aa', 'student@uniquindio.edu.co', '1094000099', 'Student Demo',     'DAY')
ON CONFLICT (institutional_email) DO NOTHING;

-- Semester + active window ----------------------------------------------------
INSERT INTO semesters (id, code, name, starts_at, ends_at, status) VALUES
  ('51000000-0000-0000-0000-000000000001', '2026-2', '2026 Semester II',
   now() - interval '5 days', now() + interval '120 days', 'ACTIVE')
ON CONFLICT (code) DO NOTHING;

-- Upsert (not DO NOTHING): re-running the seed must always leave the demo window
-- open relative to *now*, otherwise a day-old window expires and the app degrades
-- to its "no open window" state (no countdown, catalog effectively read-only).
INSERT INTO enrollment_windows (id, semester_id, name, starts_at, ends_at, status) VALUES
  ('52000000-0000-0000-0000-000000000001', '51000000-0000-0000-0000-000000000001',
   'Main enrollment window', now() - interval '1 hour', now() + interval '3 hours', 'ACTIVE')
ON CONFLICT (id) DO UPDATE
  SET starts_at = EXCLUDED.starts_at,
      ends_at   = EXCLUDED.ends_at,
      status    = EXCLUDED.status;

-- Electives -------------------------------------------------------------------
INSERT INTO electives (id, name, area, description) VALUES
  ('53000000-0000-0000-0000-000000000001', 'Machine Learning',  'Data Science & AI',
   'Supervised and unsupervised learning, model evaluation and a capstone applied project on real datasets.'),
  ('53000000-0000-0000-0000-000000000002', 'Cloud Computing',   'Systems & Infrastructure',
   'Cloud architectures, containers and serverless on AWS/GCP.'),
  ('53000000-0000-0000-0000-000000000003', 'Cybersecurity',     'Networks & Security',
   'Threat modeling, secure coding and applied penetration testing fundamentals.'),
  ('53000000-0000-0000-0000-000000000004', 'Software Architecture', 'Software Engineering',
   'Architectural styles, quality attributes and architecture decision records.')
ON CONFLICT DO NOTHING;

-- Base prerequisites ----------------------------------------------------------
INSERT INTO prerequisites (elective_id, name, description) VALUES
  ('53000000-0000-0000-0000-000000000001', 'Probability & Statistics', 'Passed Probability & Statistics.'),
  ('53000000-0000-0000-0000-000000000001', 'Programming II',           'Solid Python or Java.'),
  ('53000000-0000-0000-0000-000000000003', 'Computer Networks',        'Passed Computer Networks.')
ON CONFLICT (elective_id, name) WHERE status = 'ACTIVE' DO NOTHING;

-- Offerings for the active semester ------------------------------------------
INSERT INTO elective_offerings (id, semester_id, elective_id) VALUES
  ('54000000-0000-0000-0000-000000000001', '51000000-0000-0000-0000-000000000001', '53000000-0000-0000-0000-000000000001'),
  ('54000000-0000-0000-0000-000000000002', '51000000-0000-0000-0000-000000000001', '53000000-0000-0000-0000-000000000002'),
  ('54000000-0000-0000-0000-000000000003', '51000000-0000-0000-0000-000000000001', '53000000-0000-0000-0000-000000000003'),
  ('54000000-0000-0000-0000-000000000004', '51000000-0000-0000-0000-000000000001', '53000000-0000-0000-0000-000000000004')
ON CONFLICT (semester_id, elective_id) DO NOTHING;

-- Effective (offering) prerequisites -----------------------------------------
INSERT INTO offering_prerequisites (offering_id, name, description, source) VALUES
  ('54000000-0000-0000-0000-000000000001', 'Probability & Statistics', 'Passed Probability & Statistics.', 'ELECTIVE_DEFAULT'),
  ('54000000-0000-0000-0000-000000000001', 'Programming II',           'Solid Python or Java.',            'ELECTIVE_DEFAULT'),
  ('54000000-0000-0000-0000-000000000003', 'Computer Networks',        'Passed Computer Networks.',        'ELECTIVE_DEFAULT')
ON CONFLICT (offering_id, name) WHERE status = 'ACTIVE' DO NOTHING;

-- Groups ----------------------------------------------------------------------
INSERT INTO offering_groups (id, offering_id, group_code, shift, teacher_name, schedule_text, capacity) VALUES
  ('55000000-0000-0000-0000-000000000001', '54000000-0000-0000-0000-000000000001', 'ML-D1', 'DAY',   'Prof. Vargas',  'Mon & Wed 8:00–10:00', 30),
  ('55000000-0000-0000-0000-000000000002', '54000000-0000-0000-0000-000000000001', 'ML-N1', 'NIGHT', 'Prof. Vargas',  'Tue & Thu 18:00–20:00', 2),
  ('55000000-0000-0000-0000-000000000003', '54000000-0000-0000-0000-000000000002', 'CC-D1', 'DAY',   'Prof. Salazar', 'Mon & Wed 10:00–12:00', 25),
  ('55000000-0000-0000-0000-000000000004', '54000000-0000-0000-0000-000000000003', 'CS-N1', 'NIGHT', 'Prof. Rincón',  'Tue & Thu 20:00–22:00', 20),
  ('55000000-0000-0000-0000-000000000005', '54000000-0000-0000-0000-000000000004', 'SA-D1', 'DAY',   'Prof. Castaño', 'Fri 8:00–12:00', 18)
ON CONFLICT (offering_id, group_code) DO NOTHING;

-- Global settings --------------------------------------------------------------
-- The institutional email allow-list consumed by the login flow (identity's
-- DomainPolicy). A super-admin can edit this at runtime from the Settings page;
-- the backend falls back to ALLOWED_EMAIL_DOMAINS config when it is absent.
INSERT INTO global_settings (key, value_json, description) VALUES
  ('allowed_email_domains', '["uniquindio.edu.co"]',
   'Institutional email domains allowed to request a login code.')
ON CONFLICT (key) DO NOTHING;
