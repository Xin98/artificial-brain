# Artificial Brain

ITER-0003 turns the ITER-0002 reminder seam into a reliable delivery closed loop on the existing identity/todo/conversation stack: reminder plans fan out into one delivery row per enabled channel at plan time, and the Worker now runs a River reminder worker that delivers due reminders exactly once per channel through the configured provider adapters (a fake outbox by default, with real SMTP and Aliyun SMS adapters behind configuration). Suppression is decided at execution time — a todo completed or cleared before its due instant never delivers — failed attempts retry with capped exponential backoff, and jobs past their attempt budget become business dead letters. Provider receipts are informational: a single shared-secret HMAC-SHA256 webhook records delivery evidence without ever flipping terminal delivery states. The dashboard shows four real reminder counters and a reminder records list. No real model calls happen in this iteration, and CI and Compose never talk to a real provider.

## Prerequisites

- Go 1.26.5 (or a newer 1.26 patch)
- Node.js 24.18.0, Corepack, and pnpm 11.19.0
- Docker with Docker Compose v2
- `curl`, `jq`, and Ruby (used by bounded migration and smoke tests)

Check the local toolchain with `make toolchain-check`, then install JavaScript dependencies with `corepack pnpm install --frozen-lockfile`.

## Run locally

Start the complete stack in the foreground:

```sh
make dev
```

Compose waits for PostgreSQL, runs the migration process to completion, then starts API and Worker — the Worker also runs the River reminder worker that delivers due reminders through the configured provider adapters — and finally starts Web after API readiness succeeds. The default host URLs are:

- Web workbench (session-gated dashboard): `http://localhost:3000/`
- Web todos / conversation / settings: `http://localhost:3000/todos`, `/conversation`, `/settings`
- Web login: `http://localhost:3000/login`
- Web system status page: `http://localhost:3000/status`
- Web liveness: `http://localhost:3000/health/live`
- API liveness: `http://localhost:8080/health/live`
- API readiness: `http://localhost:8080/health/ready`
- API system health: `http://localhost:8080/api/v1/system/health`

Web proxies `/api/v1/:path*` to the API, so the browser only ever talks to the Web origin. The Worker health port and PostgreSQL are private to the Compose network. Browser code never receives their Compose service names.

### Logging in locally

Compose runs in cloud mode with a fake SMS outbox. Login is a two-step flow: request a code for a phone number, then read the code from the double-gated dev inbox (`GET /api/v1/dev/sms-inbox?address=<phone>` — present only when `APP_ENV` is not `production` **and** `DEV_INBOX_ENABLED=true`), and verify it to receive the `ab_session` cookie. Every workbench route redirects to `/login` without a validated session.

Reminder delivery has its own double-gated dev outbox: messages sent by the fake reminder adapters land in `GET /api/v1/dev/reminder-outbox?address=<email-or-phone>` (present only when `APP_ENV` is not `production` **and** `REMINDER_DEV_OUTBOX_ENABLED=true`). It returns the latest rendered message bodies for that address — plaintext, dev-only, same exception class as the login dev inbox.

### Business API routes

All business routes return stable `{code, message, correlationId}` error envelopes and live behind the session cookie except where noted:

- `POST /api/v1/auth/login/request`, `POST /api/v1/auth/login/verify`, `POST /api/v1/auth/logout`, `GET /api/v1/auth/session`
- `GET|POST /api/v1/settings/contact-channels`, `POST /api/v1/settings/contact-channels/{channelId}/verify`, `PATCH /api/v1/settings/contact-channels/{channelId}`
- `GET|POST /api/v1/todos`, `GET|PATCH /api/v1/todos/{todoId}`, `POST /api/v1/todos/{todoId}/complete`
- `GET /api/v1/dashboard/summary?timezone=<IANA>`
- `POST /api/v1/conversation/messages`, `POST /api/v1/confirmations`, `POST /api/v1/confirmations/{confirmationId}/confirm`
- `GET /api/v1/reminders` (delivery records), `GET /api/v1/ops/reminder` (queue and delivery ops snapshot)
- `POST /api/v1/portability/export` (full-history zip bundle download), `POST /api/v1/portability/imports` (multipart `bundle` upload; two-phase import answering `201 {importId, preview}`), `GET /api/v1/portability/imports/{importId}`, `POST /api/v1/portability/imports/{importId}/confirm` (final report; `409 import_conflict` once committed, `422 bundle_invalid` / `bundle_too_large` on rejected bundles)
- `POST /api/v1/webhooks/receipts/sms` (no session cookie; authenticated by the shared `REMINDER_RECEIPT_SECRET` HMAC-SHA256 signature)
- `GET /api/v1/dev/sms-inbox?address=<phone>` (unauthenticated, double-gated, dev only)
- `GET /api/v1/dev/reminder-outbox?address=<email-or-phone>` (unauthenticated, double-gated, dev only)

There is deliberately no raw `DELETE /api/v1/todos/{id}`: manual and smart deletes both require a one-time, TTL-bounded confirmation bound to the user, workspace, todo, and todo version.

Copy `.env.example` to an ignored `.env` only when local overrides are needed. Never commit real credentials.

| Variable | Default in Compose | Purpose |
| --- | --- | --- |
| `POSTGRES_DB` | `artificial_brain` | Local database name |
| `POSTGRES_USER` | `artificial_brain` | Local database user |
| `POSTGRES_PASSWORD` | `local-development-only` | Local-only PostgreSQL password; replace outside local development |
| `SERVICE_VERSION` | `dev` | Version attached to service logs and Worker leases |
| `API_HTTP_ADDRESS` | `:8080` | API listen address inside its container |
| `WORKER_HEALTH_ADDRESS` | `:8081` | Worker-only health listen address |
| `WORKER_HEARTBEAT_INTERVAL` | `2s` | Interval between Worker lease heartbeats |
| `WORKER_LEASE_TTL` | `6s` | Maximum Worker heartbeat age before degradation; must be at least twice the interval |
| `SHUTDOWN_TIMEOUT` | `10s` | Graceful API and Worker shutdown budget |
| `API_PORT` | `8080` | API host port; set to `0` for Docker assignment |
| `WEB_PORT` | `3000` | Web host port; set to `0` for Docker assignment |
| `DATABASE_URL` | supplied by Compose | Required PostgreSQL URL for direct Go process execution |
| `MIGRATIONS_DIR` | `/migrations` | Migration directory used only by the migrate command |
| `API_INTERNAL_URL` | `http://api:8080` | Server-only API base URL for Web; also baked into the Web rewrite destination at image build time |
| `APP_ENV` | `development` | Deployment environment; the dev inbox requires a non-`production` value |
| `DEV_INBOX_ENABLED` | `true` | Enables the fake SMS inbox route; `config.Load` fails if `true` with `APP_ENV=production` |
| `DEPLOYMENT_MODE` | `cloud` | Deployment form: `cloud` (open login) or `private` (single fixed administrator, `PRIVATE_ADMIN_PHONE` required) |
| `PRIVATE_ADMIN_PHONE` | unset | E.164 phone number of the fixed private-mode administrator; required when `DEPLOYMENT_MODE=private`, must stay unset in cloud mode |
| `MODEL_ADAPTER` | `deterministic` | Conversation model adapter: `deterministic` (embedded corpus) or `openai_compatible` |
| `MODEL_BASE_URL`, `MODEL_NAME`, `MODEL_API_KEY` | unset | Required when `MODEL_ADAPTER=openai_compatible`; never point CI or Compose at a real model |
| `MODEL_TIMEOUT` | `15s` | Timeout for OpenAI-compatible model calls |
| `PORTABILITY_MAX_BUNDLE_BYTES` | `33554432` | Maximum accepted portability export bundle size in bytes; configuration floor is `1048576` |
| `SESSION_TTL` | `168h` | Session cookie lifetime |
| `LOGIN_CHALLENGE_TTL` | `5m` | Login code lifetime |
| `CHANNEL_CODE_TTL` | `10m` | Contact-channel verification code lifetime |
| `CONFIRMATION_TTL` | `5m` | Delete-confirmation lifetime |
| `REMINDER_EMAIL_ADAPTER` | `fake` | Reminder email provider adapter: `fake` (dev outbox) or `smtp`; `config.Load` fails on `fake` with `APP_ENV=production` |
| `REMINDER_SMS_ADAPTER` | `fake` | Reminder SMS provider adapter: `fake` or `aliyun`; `config.Load` fails on `fake` with `APP_ENV=production` |
| `REMINDER_RECEIPT_SECRET` | `local-development-only` | Shared HMAC-SHA256 secret signing the receipt webhook; required for the API role |
| `REMINDER_DEV_OUTBOX_ENABLED` | `true` | Enables the reminder dev outbox route; `config.Load` fails if `true` with `APP_ENV=production` |
| `REMINDER_QUEUE_EMAIL_CONCURRENCY` | `2` | Worker River email queue concurrency |
| `REMINDER_QUEUE_SMS_CONCURRENCY` | `2` | Worker River SMS queue concurrency |
| `REMINDER_JOB_MAX_ATTEMPTS` | `5` | Reminder job attempts before the delivery dead-letters |

Integration tests for the PostgreSQL adapters additionally use `TEST_DATABASE_URL` (skipped when unset).

## Private deployment

The same Compose stack also runs as a single-host private deployment: one
administrator identified by `PRIVATE_ADMIN_PHONE`, selected with
`DEPLOYMENT_MODE=private`, and real model/SMTP/SMS adapters under
`APP_ENV=production` (the fake adapters and dev inbox/outbox are forbidden
there). Every phone number except the administrator's receives
`registration_closed`. Quick start, network-exposure guidance, backup and
restore, upgrade, and offline install:
[`deploy/private/README.md`](deploy/private/README.md).

## Health semantics

Liveness proves only that the process HTTP loop is responding. Readiness verifies dependencies needed to serve correctly and returns a stable non-2xx response when they are unavailable. API system health reports fixed API, database, and Worker components. A database failure or expired Worker heartbeat makes the aggregate `degraded`; the Web page still renders actionable component state. If Web cannot obtain or validate the API report, it renders `unavailable` instead of leaking the raw failure.

The API reads the Worker heartbeat lease from PostgreSQL; Web never calls Worker directly. Health checks are read-only and do not run migrations or create schema.

## Migration ownership

`backend/cmd/migrate` is the only schema owner. It uses tern as a one-shot adapter over the immutable migrations in `deploy/migrations`. API and Worker only verify schema compatibility and fail safely when the schema is absent or incompatible. Compose blocks API and Worker until migration succeeds.

## Verification

Human and CI verification use the same Make entry points:

```sh
make toolchain-check   # verify pinned local tools
make harness-test     # repository, Make graph, secret, and workflow policy
make format           # mutate Go, Web, and repository files into canonical format
make format-check     # read-only formatting check
make lint             # go vet plus Web lint
make architecture-test
make test             # Go race tests plus Web tests
make build            # production Go and Web builds
make verify           # all Docker-free, read-only pre-commit gates
make migration-test   # isolated empty-schema, migration ownership, and adapter DB proof (schema v8)
make smoke-test       # complete healthy/degraded/recovered Compose proof plus the authenticated end-to-end loop, including the reminder delivery, suppression, receipt, ops, worker-restart recovery, portability, private-mode, backup/restore, and upgrade drill blocks
```

Local data operations wrap the guarded `deploy/private` operator scripts and
the offline image bundle:

```sh
make backup                                   # pg_dump archive + sha256 sidecar under deploy/private/backups/
make restore BACKUP=<archive> CONFIRM=restore # destructive, CONFIRM-gated restore of one archive
make offline-bundle                           # docker save every stack image into .artifacts/offline/ with a load recipe
```

`make verify` never calls `make format`, Docker, migration tests, or smoke tests. Run the complete local acceptance sequence as CI does:

```sh
corepack pnpm install --frozen-lockfile
make verify
make migration-test
make smoke-test
```

## Shutdown and local data

Press Ctrl-C during `make dev` to stop the foreground Compose process; API and Worker use their configured graceful shutdown budget. Run `make down` to stop and remove this project's containers and network. **`make down` preserves data.**

Permanent local cleanup is separately guarded:

```sh
make clean-local-data CONFIRM=delete
```

This destroys only this Compose project's local named volume. The command refuses to run without the exact confirmation value.

## Diagnostics

- Tool mismatch: run `make toolchain-check`, then install the exact Node.js/pnpm pins or a compatible Go 1.26 patch.
- Port collision: set `API_PORT` or `WEB_PORT` in the local environment; smoke tests use Docker-assigned ports automatically.
- Startup ordering: run `docker compose ps --all`. A failed `migrate` container intentionally prevents API and Worker startup.
- Readiness failure: query the API readiness and system-health URLs above; responses are stable and credential-free.
- Recent service output: run `docker compose logs --no-color --tail 200`. Do not publish raw local logs without checking them for environment-specific sensitive values; automated smoke failure diagnostics redact supported credential forms.
- Resetting a disposable local schema: use only the guarded `make clean-local-data CONFIRM=delete`, then `make dev`.
