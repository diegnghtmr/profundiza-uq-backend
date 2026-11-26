# Profundiza UQ API reference

## Runtime layout

[cmd/api](../cmd/api/main.go) connects the HTTP server, database, and background workers and handles graceful shutdown. Domain rules do not import framework adapters: `adapter → app → domain`. `internal/shared/domain` supplies shared values such as `AcademicShift`; `internal/enrollment/domain` owns classification, decisions, maximum-elective checks, and status and priority types. The enrollment application layer coordinates use cases through ports; the PostgreSQL adapter implements submission transactions with `SELECT ... FOR UPDATE`. `internal/semester` is a complete domain/application/HTTP/PostgreSQL slice. `internal/platform` handles environment loading, HTTP error envelopes and trace/logging/recovery middleware, the database pool, and embedded migrations protected by a PostgreSQL advisory lock.

## Route map

All paths below are under `/api/v1`. The public health and readiness endpoints are `/healthz` and `/readyz` outside that prefix. See the HTTP adapters for exact request shapes and individual authorization requirements.

| Area | Routes |
| --- | --- |
| Identity | `POST /auth/login/start`, `POST /auth/login/verify`, `POST /auth/logout`, `GET /me` |
| Semesters | `GET /semesters`, `GET /semesters/{id}`; admin `POST /semesters`, `POST /semesters/{id}/activate`, `POST /semesters/{id}/close` |
| Windows | `GET /enrollment-windows`, `GET /enrollment-windows/{id}`; admin `POST /enrollment-windows`, `PATCH /enrollment-windows/{id}` |
| Catalog | `GET /offerings`, `GET /offerings/{id}`, `GET /offerings/{id}/prerequisites`; admin operations under `/offerings`, `/electives`, and `/offering-groups`, including `POST /offerings`, `POST /offerings/{id}/groups`, `POST /offerings/{id}/prerequisites`, `PATCH /offering-groups/{id}`, `POST /offering-groups/{id}/capacity-adjustments` |
| Enrollment | `POST /enrollment-requests`, `POST /enrollment-requests/batch`, `GET /enrollment-requests`, `GET /enrollment-requests/{id}`, `POST /enrollment-requests/{id}/cancel` |
| Review | `GET /admin/review-queues`, `POST /admin/enrollment-requests/{id}/decisions` |
| Notifications | `GET /notifications`, `POST /notifications/{id}/read`; a background worker sends queued email |
| Reports | `GET /reports`, `POST /reports`, `GET /reports/{id}`, `GET /reports/{id}/download`; exports are generated in the background as XLSX or PDF |
| Student records | `GET /students`, `POST /students`, `GET /students/{id}`, `PATCH /students/{id}`, `POST /students/import`, and academic record reads/writes under `/students/{id}/academic-records` |
| Super-admin | `GET /admin/users`, `POST /admin/users`, `PATCH /admin/users/{id}`; `GET /admin/global-settings`, `PUT /admin/global-settings/{key}` |
| Audit | `GET /audit-events`; domain mutations such as enrollment submission write audit records inside the transaction |

Review, reports, student administration, and audit reads require an administrator role. User and settings administration require super-admin permissions. Student import accepts JSON, not multipart uploads. SQL queries use pgx directly rather than sqlc; logging is via `log/slog`, not OpenTelemetry.
