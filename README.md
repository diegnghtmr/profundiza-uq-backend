# Profundiza UQ — Backend

Go modular monolith (pragmatic Hexagonal Architecture) for the professional-elective enrollment system. Implements the consistency-critical core: server-generated arrival order, per-group capacity with row-level locking, and the priority classification rules.

## Stack

Go 1.26 · chi · pgx · PostgreSQL · embedded SQL migrations · `log/slog` structured logging. (sqlc / OpenTelemetry are planned; current queries are hand-written pgx.)

## Architecture

```
cmd/api                 process entrypoint (wiring, server, graceful shutdown)
internal/
  shared/domain         cross-context value objects (AcademicShift)
  enrollment/
    domain              PURE rules: Classify, ApplyDecision, max-electives, status/priority enums (unit-tested)
    app                 use cases + ports (Submitter)
    adapter/postgres    transactional submit — SELECT ... FOR UPDATE
  semester/
    domain | app | adapter/{postgres,http}   full vertical slice (reference for new modules)
  platform/
    config              env-based config
    httpx               APIError envelope, trace/logging/recover middleware
    postgres            pool + embedded migration runner
migrations              000001_init.{up,down}.sql + embed.go
```

Dependency rule: `adapter -> app -> domain`. The domain imports nothing framework-specific.

## Run

```bash
# From the repo root — full stack (backend, postgres, mailpit, frontend):
docker compose up --build

# Or just the API locally against a Postgres:
cp env.example .env            # then export the vars, or use a loader
go run ./cmd/api
```

`GET /healthz` / `GET /readyz` are unauthenticated liveness/readiness probes;
the application API is mounted under `/api/v1` (see [Modules](#modules)).

## Configuration

All configuration comes from the environment (`env.example` documents every
variable; secrets are never committed). Security-sensitive settings **fail
closed**:

| Variable                 | Default              | Notes                                                        |
| ------------------------ | -------------------- | ------------------------------------------------------------ |
| `APP_ENV`                | `production`         | `development` \| `staging` \| `production`. Unset → strict; an unknown value is rejected at startup. |
| `COOKIE_SECURE`          | `true`               | Must be `true` in non-development.                           |
| `ALLOWED_EMAIL_DOMAINS`  | — (empty)            | Comma-separated allow-list; **must be set** in non-development. |
| `DATABASE_URL`           | local docker pg      | pgx connection string.                                       |
| `SMTP_ADDR` / `MAIL_FROM`| Mailpit locally      | In development the login code is also logged (never in prod).|
| `HTTP_ADDR`              | `:8080`              | Listen address.                                              |

`Config.Validate()` runs at startup and a failure is fatal, so a production
deploy that forgets `APP_ENV=production`, an email allow-list, or `COOKIE_SECURE`
refuses to boot rather than running permissively. The local `docker-compose`
stack opts into development explicitly.

## Tests

```bash
go test ./...                  # pure domain unit tests (no DB needed)

# Integration + concurrency tests need a Postgres. Point at a test database
# (the compose stack exposes one) and run:
TEST_DATABASE_URL="postgres://postgres:postgres@localhost:5432/profundiza_uq_test?sslmode=disable" go test ./...
```

The headline test, `TestSubmit_NoOverbookingUnderConcurrency`, fires 25 concurrent same-shift submissions at a capacity-1 group and asserts exactly one direct seat and a fair, unique arrival order — the #1 risk (no overbooking).

## Modules

All modules follow `adapter -> app -> domain` and are wired in `cmd/api/main.go`.

| Module | Endpoints |
| :-- | :-- |
| `identity` | `POST /auth/login/start`, `/auth/login/verify`, `/auth/logout`, `GET /me` — OTP + HttpOnly cookie sessions + RBAC |
| `semester` | `GET /semesters`, `GET /semesters/{id}`, `POST /semesters`, `/{id}/activate`, `/{id}/close` |
| `window` | `GET/POST /enrollment-windows`, `GET/PATCH /enrollment-windows/{id}` |
| `catalog` | `GET /offerings`, `/offerings/{id}`, `/offerings/{id}/prerequisites`; admin: `POST /offerings`, `/offerings/{id}/groups`, `/offerings/{id}/prerequisites`, `GET/POST/PATCH /electives…`, `PATCH /offering-groups/{id}`, `POST /offering-groups/{id}/capacity-adjustments` |
| `enrollment` | `POST /enrollment-requests` (+ `/batch`), `GET /enrollment-requests`, `/{id}`, `POST /{id}/cancel` — concurrency-safe submit |
| `review` | `GET /admin/review-queues`, `POST /admin/enrollment-requests/{id}/decisions` |
| `notification` | `GET /notifications`, `POST /notifications/{id}/read` + email outbox worker |
| `reporting` | `GET/POST /reports`, `GET /reports/{id}`, `/reports/{id}/download` — async XLSX/PDF worker |
| `student` | `GET/POST /students`, `GET/PATCH /students/{id}`, `/students/import`, academic records |
| `adminuser` | `GET/POST /admin/users`, `PATCH /admin/users/{id}` (super-admin) |
| `settings` | `GET /admin/global-settings`, `PUT /admin/global-settings/{key}` (super-admin) |
| `audit` | `GET /audit-events` (read); `internal/shared/audit` writes append-only events inside each mutation tx |

**Cross-cutting:** every state change writes an `audit_events` row and enqueues notifications in the same transaction; concurrency is protected by row locks; migrations are guarded by a `pg_advisory_lock`.

**Known gaps / next:** `sqlc` + OpenTelemetry adoption; `students/import` multipart upload; richer report column sets.
