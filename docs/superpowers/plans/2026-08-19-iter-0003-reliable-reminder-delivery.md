# ITER-0003 Reliable Reminder Delivery Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Turn the ITER-0002 reminder seam into a reliable delivery closed loop: River-backed durable scheduling inserted atomically into business transactions, email/SMS delivery through real (stdlib-only) adapters gated by fakes, execution-time suppression with business idempotency, retries with business dead letters, signed receipt callbacks, real four-state dashboard counters with a delivery record list, and a deterministic JSON ops endpoint.

**Architecture:** River lives only in `reminder/adapters/outbound/river` (insert-only client in the API via `InsertTx` into the caller's ambient transaction) and `reminder/adapters/inbound/worker` (River worker calling the `SendReminder` application command — pipeline approach A). One `ReminderDelivery` row per plan channel is created at plan time with a UNIQUE business idempotency key; suppression is decided by re-reading Todo/Plan/Channel at execution time, with best-effort `JobCancel` on revoke. Dead letters are business terminal states, not queue artifacts. No Prometheus/OTel: ops is deterministic SQL over `river_job` + `reminder_deliveries`.

**Tech Stack:** Go 1.26.5, pgx 5.9.2, tern 2.4.1, `riverqueue/river` + `riverqueue/riverpgxv5` (the only new Go dependencies, sanctioned by ADR-0002), Node.js 24.18.0, pnpm 11.19.0, Next.js, Vitest, PostgreSQL 18.4, Docker Compose, GitHub Actions. Zero new web dependencies.

## Global Constraints

1. Governing docs: master design `docs/superpowers/specs/2026-08-13-ai-native-personal-workbench-mvp-design.md` (§4.4, §6.5, §6.6, §7.1, §7.4, §9.1, §12, §13, §14.3) and iteration design `docs/superpowers/specs/2026-08-19-iter-0003-reliable-reminder-delivery-design.md`. This iteration adds **no Portability, no open-ended chat, no MCP, no real-provider calls from CI** (CI/local deliver only through fake adapters; real adapters are unit-tested against local listeners/httptest fixtures).
2. **Schema version bumps 5 → 7** via two append-only tern migrations: `006_river_v1.sql` (River's official SQL inlined; header comment names the source River version) and `007_create_reminder_deliveries.sql`. `database.CurrentSchemaVersion = 7`. Only `backend/cmd/migrate` migrates; API/Worker keep the equality `RequireSchema` gate. `tests/smoke/migration_test.sh` version pin moves `5` → `7`. Migrations 001–005 are red-zone, untouched.
3. **Dependencies:** exactly two new Go modules — `github.com/riverqueue/river` and `github.com/riverqueue/riverpgxv5` at one pinned stable version each (record in go.mod and the 006 header). River imports are allowed only under `reminder/adapters/outbound/river`, `reminder/adapters/inbound/worker`, and `backend/cmd/**` (architecture policy already forbids River in application layers; no policy change planned). Zero new web dependencies.
4. **Envelope reuse:** every response carries `X-Correlation-ID`; errors use `server.ErrorResponse{code, message, correlationId}`. New stable code: `invalid_signature` (receipt webhook). Existing `validation_error`, `unauthenticated`, `not_found`, `internal_error` retained.
5. **Gating:** `REMINDER_DEV_OUTBOX_ENABLED` + `APP_ENV` double gate for `GET /api/v1/dev/reminder-outbox` (identical fail-closed pattern to `DEV_INBOX_ENABLED`: `config.Load` errors if enabled with `APP_ENV=production`; route absent otherwise). `config.Load` errors if `REMINDER_EMAIL_ADAPTER=fake` or `REMINDER_SMS_ADAPTER=fake` with `APP_ENV=production`. Secrets (`REMINDER_RECEIPT_SECRET`, SMTP password, Aliyun key secret) are never logged; config errors name keys only.
6. **Data isolation:** every delivery read/write is workspace-scoped. The single documented exception is the receipt lookup by `ProviderMessageID` (provider-keyed, analogous to session lookup by token hash — record as decision D6).
7. **Suppression correctness** rests on execution-time re-reads (spec §6.1); River `JobCancelByID` is best-effort and never load-bearing. Business idempotency key `workspaceId:todoId:todoReminderVersion:channel` is UNIQUE and enforced by the database.
8. **Clocks injected** (`now func() time.Time`) in domain/application; no real waits in unit tests. Bounded polling loops (≤ 30 s, `sleep 1` cadence) are allowed only in integration/smoke tests. Provider latency is measured `submitted_at - scheduled_at`.
9. **Dead letter expression:** final retry (worker passes `FinalAttempt=true` when `job.Attempt >= cfg.ReminderJobMaxAttempts`) converts a transient failure into the `failed` terminal state + structured log event `reminder delivery dead-lettered`. Dashboard/ops read delivery states, never River internals directly except the ops adapter.
10. **Repo hygiene:** every task red→green→refactor, updates `docs/iterations/ITER-0003/{progress,test-matrix}.md`, commits only its listed files, never stages `.env` or credentials; `make verify` stays read-only.

### Exact API route table (new and changed)

Existing routes 1–21 from ITER-0002 are unchanged except route 17 (dashboard summary gains fields). New routes:

| # | Method + Path | Auth | Request body / query | Success | Error codes |
|---|---|---|---|---|---|
| 22 | `GET /api/v1/dashboard/summary` | session | `timezone` (IANA, required) | 200 existing counters **plus** `reminderSucceeded`, `reminderSuppressed`; `reminderRetrying`/`reminderFailed` now real | 401; 422 |
| 23 | `GET /api/v1/reminders` | session | `status?` (scheduled\|sending\|retrying\|succeeded\|failed\|suppressed), `limit?` (1..200, default 50), `offset?` (≥0) | 200 `{deliveries:[DeliveryView]}` ordered `created_at desc` | 401; 422 |
| 24 | `GET /api/v1/ops/reminder` | session | — | 200 `ReminderOps` | 401 |
| 25 | `POST /api/v1/webhooks/receipts/sms` | none; HMAC | raw JSON body; header `X-Receipt-Signature` = hex HMAC-SHA256(body, `REMINDER_RECEIPT_SECRET`) | 200 `{}` (idempotent) | 401 `invalid_signature`; 422 `validation_error` |
| 26 | `GET /api/v1/dev/reminder-outbox` | none (double-gated; absent ⇒ 404) | `?address=` | 200 `{messages:[…]}` latest 5 | 422 |

`DeliveryView` JSON (camelCase, omitempty for optionals): `{id, todoId, todoTitle, channel, state, suppressionReason?, attemptCount, providerMessageId?, lastErrorCode?, scheduledAt, createdAt, submittedAt?, finalizedAt?, receiptState?, receiptAt?, receiptErrorCode?}`.
`ReminderOps` JSON: `{queues:[{queue, depth, oldestWaitSeconds}], deliveries:{scheduled, sending, retrying, succeeded, failed, suppressed}, retryRate, latencyP95Ms, checkedAt}`.

## Design decisions (record in `docs/iterations/ITER-0003/decisions.md`)

- **D1 — Pipeline approach A.** The River worker is Reminder's only inbound delivery entry point and calls the `SendReminder` application command; all suppression/idempotency/provider logic lives in the application layer. Rejected: orchestration inside the worker adapter (layering violation) and API-side queue consumption (MVP §6.6).
- **D2 — River behind the evolved `JobScheduler` port.** `Schedule(ctx, ReminderJob) ([]ScheduledChannel, error)` (fan-out per channel; job IDs returned for writeback) and `Cancel(ctx, jobID int64) error` (best-effort). The River scheduler joins the caller's ambient transaction via `database.ExecutorFromContext` + a `pgx.Tx` type assertion and `InsertTx`; without an ambient transaction it errors (no silent fallback). The no-op adapter is retired from `cmd/api` wiring (kept only as a test fake).
- **D3 — Deliveries are created at plan time.** One `scheduled` delivery per requested channel in the same transaction as the plan; UNIQUE `(todo_id, todo_reminder_version, channel)` ≡ idempotency key. "Retrying" is derived (`state=sending ∧ attempt_count>0`); `sending` with `attempt_count=0` is the first attempt in flight. Terminal states are immutable; receipts never flip them.
- **D4 — River SQL inlined into the tern sequence** as `006_river_v1.sql` with the source River version in the header; River upgrades append new migrations, never edit 006. Rejected: a parallel `rivermigrate` pipeline and worker-time self-migration (violates migration ownership).
- **D5 — Stdlib-only real adapters.** SMTP via `net/smtp` (PLAIN auth, dial+send timeouts, 4xx transient / 5xx permanent) and Aliyun SMS via standard-library HTTP + HMAC-SHA1 RPC signing (`Code=OK` ⇒ `BizId` as provider message ID; throttle/5xx/timeout transient; `isv.*`-class permanent). No provider SDKs.
- **D6 — Receipts are informational and provider-keyed.** `RecordReceipt` applies once per `ProviderMessageID` and only to `succeeded` deliveries; unknown IDs are safely ignored. Lookup by provider message ID is the one documented unscoped read (provider evidence key, cf. session token lookup).
- **D7 — Ops endpoint is deterministic SQL** over `river_job` (depth, oldest wait) and `reminder_deliveries` (counts, retry rate, latency P95 = percentile_cont(0.95) of `submitted_at - scheduled_at` over succeeded deliveries in the last 24 h). No metrics dependency.
- **D8 — Web stays on the dashboard page.** Four real reminder tiles plus a "提醒记录" list inside the existing dashboard panel; no new route page, no new web dependencies.

## File and Module Map

| Module / touchpoint | Interface (test surface) | Implementation files |
|---|---|---|
| Migrations (yellow) | tern 006–007; `CurrentSchemaVersion = 7` | `deploy/migrations/006_river_v1.sql`, `007_create_reminder_deliveries.sql`; `platform/database/schema.go` |
| Configuration (yellow) | `config.Load` + reminder fields (§ Config) | `backend/internal/platform/config/config.go` + `config_iter0003_test.go` |
| Reminder domain | `ReminderDelivery` aggregate + state machine; `IdempotencyKeyFor` | `…/reminder/domain/{delivery,errors}.go` + `delivery_test.go` |
| Reminder application | evolved `JobScheduler`; new ports `DeliveryStore, PlanStore.Get, OpsStore, TodoReader, ChannelResolver, EmailNotifier, SmsNotifier`; cmds `PlanReminder` (evolved), `RevokePlans` (evolved), `SendReminder`, `RecordReceipt`; queries `DeliveryStats, ListDeliveries, ReminderOps`; `dto.ReminderSendArgs` (kind `reminder_send`) | `…/reminder/application/{command,query,ports,dto}/*.go` (+tests) |
| Reminder postgres adapters | `DeliveryStore`, `OpsStore`, plan `Get`, fake-outbox reader | `…/reminder/adapters/outbound/postgres/{deliveries,ops,outbox_reader,integration_test}.go` |
| River adapters | `riversched.New(client)` (Schedule/Cancel); `reminderworker.SendWorker` | `…/reminder/adapters/outbound/river/scheduler.go` (+integration test); `…/reminder/adapters/inbound/worker/worker.go` (+test) |
| Provider adapters | `fake.NewEmail/NewSms` (+`Outbox`), `smtp.New(cfg)`, `aliyun.New(cfg)`, `aliyun.ParseSmsReport` | `…/reminder/adapters/outbound/{fake,smtp,aliyun}/*.go` (+tests) |
| Reminder HTTP inbound | `Handler{List, Ops, Receipt, Parse, Secret}`, `RegisterRoutes`, `NewDevOutboxHandler` | `…/reminder/adapters/inbound/http/{reminders,ops,receipts,dev_outbox,json,handler_test}.go` |
| Todo module (green) | `PlanReminderRequest` + `Title`; dashboard `ReminderStats` seam; `ReminderCounts` | `…/todo/application/{ports/reminder.go, dto/dashboard.go, command/{create,update}.go, query/dashboard.go}` (+tests) |
| Composition root (yellow) | API wiring (River insert-only client, channels provider, reminder routes, stats shim); Worker wiring (River worker start/stop + ready flag) | `backend/cmd/api/{wiring.go, composition_integration_test.go}`, `backend/cmd/worker/main.go` (+test) |
| Contracts (yellow) | `reminder.yaml` new; `dashboard.yaml` extended; traversal tests | `contracts/openapi/{reminder,dashboard}.yaml`; `tests/contract/{reminder,dashboard}_contract_test.go` |
| Web features (green) | 10-key summary validator; reminder records fetch + list | `apps/web/src/features/dashboard/{fetch-dashboard.ts, fetch-reminders.ts, dashboard-view.tsx, dashboard-panel.tsx}` (+tests) |
| Harness/deploy (yellow) | migration pin 7; compose env; `.env.example`; smoke static env + reminder E2E | `tests/smoke/{migration_test.sh, stack_test.sh}`, `compose.yaml`, `.env.example` |

### Config fields (added to `config.Config`, all env-parsed like ITER-0002 fields)

| Field | Env | Default | Validation |
|---|---|---|---|
| `ReminderEmailAdapter` | `REMINDER_EMAIL_ADAPTER` | `fake` | `fake`\|`smtp`; `smtp` requires host/port/from; fake forbidden in production |
| `ReminderSmsAdapter` | `REMINDER_SMS_ADAPTER` | `fake` | `fake`\|`aliyun`; `aliyun` requires endpoint/key-id/key-secret/sign-name/template-code; fake forbidden in production |
| `ReminderReceiptSecret` | `REMINDER_RECEIPT_SECRET` | — | required for `RoleAPI`; never echoed |
| `ReminderDevOutboxEnabled` | `REMINDER_DEV_OUTBOX_ENABLED` | `false` | bool; error if true with `APP_ENV=production` |
| `ReminderSmtpHost/Port/Username/Password/From` | `REMINDER_SMTP_*` | — | required when email adapter `smtp` |
| `ReminderSmtpTimeout` | `REMINDER_SMTP_TIMEOUT` | `10s` | duration |
| `ReminderAliyunEndpoint` | `REMINDER_ALIYUN_ENDPOINT` | `https://dysmsapi.aliyuncs.com` | URL when sms adapter `aliyun` |
| `ReminderAliyunAccessKeyID/Secret/SignName/TemplateCode` | `REMINDER_ALIYUN_*` | — | required when sms adapter `aliyun`; secrets never echoed |
| `ReminderQueueEmailConcurrency` | `REMINDER_QUEUE_EMAIL_CONCURRENCY` | `5` | int ≥ 1 |
| `ReminderQueueSmsConcurrency` | `REMINDER_QUEUE_SMS_CONCURRENCY` | `5` | int ≥ 1 |
| `ReminderJobMaxAttempts` | `REMINDER_JOB_MAX_ATTEMPTS` | `5` | int ≥ 1 |

---

### Task 1: Iteration Ledger, Policy Refresh, and Branch

**Files:**
- Create: `docs/iterations/ITER-0003/{brief,spec,plan,decisions,progress,test-matrix,handoff}.md`
- Create: `docs/superpowers/specs/2026-08-19-iter-0003-reliable-reminder-delivery-design.md` (exists already at branch time — commit carries it)
- Create: `docs/superpowers/plans/2026-08-19-iter-0003-reliable-reminder-delivery.md` (this plan)
- Modify (yellow): `AGENTS.md`, `backend/AGENTS.md`, `apps/web/AGENTS.md` (green zone now includes the reminder delivery modules; red zone: no Portability, no real-provider calls from CI, no lowering gates)

**Interfaces:** consumes master design §14.3 + iteration spec; produces `decisions.md` seeded with D1–D8 and `test-matrix.md` row IDs (RMD/DLV/RIV/PRV/HTP/TDO/CNT/WEB/MIG/OPS).

- [ ] **Step 1: Branch** `git checkout -b iter-0003-reliable-reminder-delivery` from `master`.
- [ ] **Step 2: Author the ledger** — `brief.md` carries the spec's 10 acceptance criteria verbatim; `spec.md` points at the approved design; `plan.md` points at this plan; `progress.md` table (Task | Status | Evidence) lists all 16 tasks Pending; `test-matrix.md` table (Requirement | Command and evidence | Evidence commit | Status) has one row per row ID.
- [ ] **Step 3: Refresh AGENTS.md zones** so the red zone forbids Portability and real-provider CI calls instead of "no delivery/River".
- [ ] **Step 4: Verify** `make harness-test` green; `git status --short` shows only intended files.
- [ ] **Step 5: Commit** `docs: open ITER-0003 iteration ledger` (only ledger + AGENTS files; spec + plan committed separately as listed).

### Task 2: River Dependency, Migrations 006–007, Schema 5→7 (yellow)

**Files:**
- Modify: `go.mod` / `go.sum`
- Create: `deploy/migrations/006_river_v1.sql`, `deploy/migrations/007_create_reminder_deliveries.sql`
- Modify: `backend/internal/platform/database/schema.go` (`CurrentSchemaVersion int32 = 7`)
- Modify: `tests/smoke/migration_test.sh` (version pin `5` → `7`, lines ~140–141)
- Modify: `backend/internal/platform/database/migrate_integration_test.go` (assert version 7 + new tables)

**Interfaces:** tern contract (`---- create above / drop below ----` marker); after this task every object of the pinned River schema (representative check: `river_job` in `public`) plus `reminder.reminder_deliveries` and `reminder.fake_outbox` exist after `RunMigrations`. Tern owns versioning, so River's own `river_migration` tracking table is neither created nor expected.

- [ ] **Step 1: Add dependencies.** `go get github.com/riverqueue/river@latest github.com/riverqueue/riverpgxv5@latest` (if the default module proxy is unreachable on this host, retry with `GOPROXY=https://goproxy.cn,direct`). Record the exact pinned versions.
- [ ] **Step 2 (red):** extend `migrate_integration_test.go`: version 7; `river_job` table exists in `public`; `reminder.reminder_deliveries` and `reminder.fake_outbox` exist. Run `TEST_DATABASE_URL=… go test ./backend/internal/platform/database -race -run TestMigrate -v` ⇒ FAIL.
- [ ] **Step 3 (green):** author migrations. 006: concatenate **all** `*.up.sql` files in order from the pinned module's embedded migrations (`$(go env GOMODCACHE)/github.com/riverqueue/river@<version>/rivermigrate/migration/main/`), preceded by a header comment `-- River <version> schema, inlined from rivermigrate migrations; upgrade by appending a new migration`; append the tern drop marker and drop every object the up section creates in reverse order (enumerate with `grep -iE 'create (table|index|function|trigger|type)' 006_river_v1.sql`). 007:

```sql
create table reminder.reminder_deliveries (
  id uuid primary key,
  workspace_id uuid not null,
  owner_user_id uuid not null,
  todo_id uuid not null,
  todo_reminder_version integer not null,
  plan_id uuid not null references reminder.reminder_plans (id),
  channel text not null,
  todo_title_snapshot text not null,
  idempotency_key text not null,
  state text not null default 'scheduled',
  suppression_reason text,
  attempt_count integer not null default 0,
  provider_job_id bigint,
  provider_message_id text,
  last_error_code text,
  scheduled_at timestamptz not null,
  created_at timestamptz not null default now(),
  submitted_at timestamptz,
  finalized_at timestamptz,
  receipt_state text,
  receipt_at timestamptz,
  receipt_error_code text,
  constraint reminder_deliveries_channel_check check (channel in ('email', 'sms')),
  constraint reminder_deliveries_state_check check (state in ('scheduled', 'sending', 'succeeded', 'failed', 'suppressed')),
  constraint reminder_deliveries_suppression_check check (suppression_reason in ('todo_completed', 'todo_deleted', 'version_stale', 'channel_unavailable', 'plan_revoked')),
  constraint reminder_deliveries_receipt_check check (receipt_state in ('received_ok', 'received_failed')),
  constraint reminder_deliveries_idempotency_unique unique (idempotency_key),
  constraint reminder_deliveries_todo_channel_unique unique (todo_id, todo_reminder_version, channel)
);

create index reminder_deliveries_workspace_state_idx on reminder.reminder_deliveries (workspace_id, state);
create index reminder_deliveries_provider_message_idx on reminder.reminder_deliveries (provider_message_id);
create index reminder_deliveries_plan_idx on reminder.reminder_deliveries (plan_id);

create table reminder.fake_outbox (
  id bigserial primary key,
  address text not null,
  channel text not null,
  todo_id uuid not null,
  body text not null,
  created_at timestamptz not null default now()
);

create index fake_outbox_address_created_at_idx on reminder.fake_outbox (address, created_at desc);
```

  Bump `schema.go` and the migration-test pin.
- [ ] **Step 4: Verify** `TEST_DATABASE_URL=… go test ./backend/internal/platform/database -race -v` ⇒ PASS; `go build ./backend/cmd/migrate` ⇒ PASS; down/up round-trip: run the migrate command twice against a scratch database URL and confirm idempotency the same way `migration_test.sh` does.
- [ ] **Step 5: Commit** `feat: add River schema and reminder delivery migrations`.

### Task 3: Platform Config Reminder Fields (yellow)

**Files:**
- Modify: `backend/internal/platform/config/config.go`
- Create: `backend/internal/platform/config/config_iter0003_test.go`

**Interfaces:** `Config` gains the fields in the Config table above; `Load` keeps its `(Role, LookupEnv)` signature and error style (`config: …`, keys named, values never echoed).

- [ ] **Step 1 (red):** `config_iter0003_test.go` using the existing `mapLookup` helper: defaults (`fake`/`fake`, dev outbox disabled, concurrencies 5, max attempts 5, smtp timeout 10s, aliyun endpoint default); unknown adapter values rejected; `smtp` without `REMINDER_SMTP_HOST/PORT/FROM` rejected; `aliyun` without its five settings rejected; fake adapter + `APP_ENV=production` rejected for both channels; `REMINDER_DEV_OUTBOX_ENABLED=true` + production rejected; `REMINDER_DEV_OUTBOX_ENABLED=true` + `APP_ENV=development` accepted; `ReminderReceiptSecret` required for `RoleAPI` but not `RoleWorker`/`RoleMigrate`; invalid integer concurrency (`0`, `x`) rejected; `TestLoadReminderErrorsNeverEchoSecretValues` (set secrets, assert their values appear in no error string). Run `go test ./backend/internal/platform/config -race` ⇒ FAIL.
- [ ] **Step 2 (green):** implement parsing/validation following the existing `durationEnv`/`boolEnv` helpers' style. `go test ./backend/internal/platform/config -race -v` ⇒ PASS.
- [ ] **Step 3: Commit** `feat: add reminder adapter configuration`.

### Task 4: Reminder Domain — Delivery Aggregate

**Files:**
- Create: `backend/internal/modules/reminder/domain/delivery.go`
- Modify: `backend/internal/modules/reminder/domain/errors.go`
- Create: `backend/internal/modules/reminder/domain/delivery_test.go`

**Interfaces:**

```go
type DeliveryState string      // scheduled | sending | succeeded | failed | suppressed
type SuppressionReason string  // todo_completed | todo_deleted | version_stale | channel_unavailable | plan_revoked
type ReceiptState string       // received_ok | received_failed

type ReminderDelivery struct {
    ID, WorkspaceID, TodoID  string
    OwnerUserID              string // denormalized from the todo at plan time; keeps every execution-time read workspace+user scoped
    TodoReminderVersion      int
    PlanID, Channel, TodoTitleSnapshot string
    IdempotencyKey           string
    State                    DeliveryState
    SuppressionReason        *SuppressionReason
    AttemptCount             int
    ProviderJobID            *int64
    ProviderMessageID        *string
    LastErrorCode            *string
    ScheduledAt, CreatedAt   time.Time
    SubmittedAt, FinalizedAt *time.Time
    ReceiptState             *ReceiptState
    ReceiptAt                *time.Time
    ReceiptErrorCode         *string
}

func IdempotencyKeyFor(workspaceID, todoID string, todoReminderVersion int, channel string) string // "ws:todo:version:channel"
func NewDelivery(id, workspaceID, ownerUserID, todoID string, todoReminderVersion int, planID, channel, titleSnapshot string, scheduledAt, now time.Time) (ReminderDelivery, error)
func (d *ReminderDelivery) IsFinal() bool
func (d *ReminderDelivery) MarkSending(now time.Time) error            // scheduled|sending only; AttemptCount++
func (d *ReminderDelivery) MarkSucceeded(providerMessageID string, now time.Time) error // sending only
func (d *ReminderDelivery) MarkFailed(errorCode string, now time.Time) error            // scheduled|sending
func (d *ReminderDelivery) MarkSuppressed(reason SuppressionReason, now time.Time) error // scheduled|sending
func (d *ReminderDelivery) ApplyReceipt(state ReceiptState, errorCode string, now time.Time) error
```

New errors: `ErrInvalidDeliveryChannel`, `ErrDeliveryFinal`, `ErrReceiptNotApplicable`, `ErrDeliveryExists`, `ErrDeliveryNotFound`.

- [ ] **Step 1 (red):** table-driven tests: `NewDelivery` builds the idempotency key and normalizes to `scheduled` with nil optionals; empty id/workspace/owner/todo/plan/title, zero scheduled-at, and channels other than `email`/`sms` rejected; `MarkSending` increments attempts from scheduled and again from sending; every transition from each terminal state returns `ErrDeliveryFinal`; `MarkSucceeded` sets SubmittedAt+FinalizedAt; `MarkSuppressed` records the reason + FinalizedAt; `ApplyReceipt` succeeds once on `succeeded` (idempotent second call is a no-op returning nil), fails with `ErrReceiptNotApplicable` on any other state. Run `go test ./backend/internal/modules/reminder/domain -race` ⇒ FAIL.
- [ ] **Step 2 (green):** implement; `go test ./backend/internal/modules/reminder/domain -race -v` ⇒ PASS; `make architecture-test` ⇒ PASS (domain imports stdlib only).
- [ ] **Step 3: Commit** `feat: add reminder delivery aggregate`.

### Task 5: Reminder Application — Port Evolution, Plan/Revoke With Deliveries

**Files:**
- Modify: `…/reminder/application/ports/scheduler.go`, `ports/store.go`, `…/reminder/application/dto/plan.go`, `…/reminder/application/command/plan.go`, `command/revoke.go`, `command/fakes_test.go`, `command/command_test.go`, `…/reminder/adapters/outbound/noopjob/scheduler.go` (+test)
- Create: `…/reminder/application/ports/delivery.go` (new ports), `dto/delivery.go`

**Interfaces:**

```go
// ports/scheduler.go (evolved)
type ReminderJob struct { …existing fields…; OwnerUserID string } // new field joins Title in the plan payload
type ScheduledChannel struct { Channel string; JobID int64 }
type JobScheduler interface {
    Schedule(ctx context.Context, job ReminderJob) ([]ScheduledChannel, error)
    Cancel(ctx context.Context, jobID int64) error
}

// ports/store.go (additions)
type PlanStore interface {
    Save(ctx context.Context, plan domain.ReminderPlan) error
    Get(ctx context.Context, workspaceID, planID string) (domain.ReminderPlan, error) // ErrPlanNotFound
    RevokePlanned(ctx context.Context, workspaceID, todoID string, upToReminderVersion int, now time.Time) error
}

// ports/delivery.go (new)
type DeliveryStore interface {
    Save(ctx context.Context, delivery domain.ReminderDelivery) error // domain.ErrDeliveryExists on unique dup
    Update(ctx context.Context, delivery domain.ReminderDelivery) error
    ByIdempotencyKey(ctx context.Context, workspaceID, key string) (domain.ReminderDelivery, error) // domain.ErrDeliveryNotFound
    ByProviderMessageID(ctx context.Context, providerMessageID string) (domain.ReminderDelivery, error) // D6 provider-keyed
    SetProviderJobID(ctx context.Context, workspaceID, deliveryID string, jobID int64) error
    PlannedJobIDs(ctx context.Context, workspaceID, todoID string, upToReminderVersion int) ([]int64, error)
    Stats(ctx context.Context, workspaceID string) (dto.DeliveryCounts, error)
    List(ctx context.Context, workspaceID string, filter dto.DeliveryFilter) ([]domain.ReminderDelivery, error)
}

// dto/plan.go gains: PlanRequest.Title string and PlanRequest.OwnerUserID string
// dto/delivery.go:
type DeliveryCounts struct { Scheduled, Sending, Retrying, Succeeded, Failed, Suppressed int } // Sending = sending∧attempt=0; Retrying = sending∧attempt>0
type DeliveryFilter struct { Status string; Limit, Offset int }
```

`PlanReminderHandler` gains a `Deliveries ports.DeliveryStore` field; `RevokePlansHandler` gains `Deliveries`, `Scheduler`, and `Log *slog.Logger` fields. Handler semantics per spec §6.2/§5.2: Plan = save plan (idempotent on `ErrPlanExists`) → save one delivery per channel carrying `OwnerUserID` + `Title` snapshots (`ErrDeliveryExists` tolerated) → `Schedule` (ReminderJob carries OwnerUserID) → `SetProviderJobID` per returned channel. Revoke = `RevokePlanned` → `PlannedJobIDs` → `Scheduler.Cancel` each; cancel errors are logged via `Log` and never fail the revoke. The noop scheduler returns an empty slice / nil cancel.

- [ ] **Step 1 (red):** adapt existing command tests to the new signatures; add cases with recording fakes: plan with two channels saves plan + 2 deliveries (deterministic order, correct idempotency keys, `Title` and `OwnerUserID` snapshots carried) then schedules once with identical fields (ReminderJob includes OwnerUserID) and writes returned JobIDs back per channel; scheduler error ⇒ handler error (caller tx rolls back); `ErrPlanExists` ⇒ early nil without touching deliveries/scheduler; empty channels ⇒ plan only. Revoke: `PlannedJobIDs` [10, 11] ⇒ `Cancel` called twice; cancel error ⇒ revoke still succeeds and error logged; zero jobs ⇒ no cancel. Noop adapter satisfies the new interface. Run `go test ./backend/internal/modules/reminder/... -race` ⇒ FAIL.
- [ ] **Step 2 (green):** implement; `go test ./backend/internal/modules/reminder/... -race -v` ⇒ PASS; `make architecture-test` ⇒ PASS.
- [ ] **Step 3: Commit** `feat: evolve reminder planning seam for delivery rows and River job IDs`.

### Task 6: Reminder Application — SendReminder, RecordReceipt, Queries

**Files:**
- Create: `…/reminder/application/command/send.go`, `command/receipt.go`
- Create: `…/reminder/application/query/{stats,list,ops}.go`
- Create/extend: `dto/delivery.go` (`TodoView, ChannelEndpoint, ReminderMessage, SendResult, ReceiptPayload, OpsView, QueueDepth`), `ports/readers.go` (`TodoReader, ChannelResolver`), `ports/notify.go` (`EmailNotifier, SmsNotifier, OpsStore`, `var ErrPermanent`)
- Modify: `command/command_test.go`, `command/fakes_test.go`

**Interfaces:**

```go
type TodoReader interface { Get(ctx context.Context, workspaceID, ownerUserID, todoID string) (dto.TodoView, error) } // dto.TodoView{Title, Status string; ReminderVersion int; OwnerUserID string}; ErrTodoNotFound; signature mirrors todo's scoped store Get so the cmd shim is direct
type ChannelResolver interface { Resolve(ctx context.Context, workspaceID, userID, channel string) (dto.ChannelEndpoint, error) } // dto.ChannelEndpoint{Address string; Usable bool}; not-found ⇒ zero endpoint, nil error
type EmailNotifier interface { Send(ctx context.Context, message dto.ReminderMessage) (dto.SendResult, error) }
type SmsNotifier interface { Send(ctx context.Context, message dto.ReminderMessage) (dto.SendResult, error) } // dto.ReminderMessage{To, Title string; ScheduledAtUTC time.Time}; dto.SendResult{ProviderMessageID string}
var ErrPermanent = errors.New("reminder: permanent provider failure") // adapters wrap; transient = any other error
type OpsStore interface { ReminderOps(ctx context.Context, now time.Time, window time.Duration) (dto.OpsView, error) }

type SendReminderHandler struct { Plans ports.PlanStore; Deliveries ports.DeliveryStore; Todos ports.TodoReader; Channels ports.ChannelResolver; Email ports.EmailNotifier; Sms ports.SmsNotifier; Log *slog.Logger; Now func() time.Time }
type SendRequest struct { WorkspaceID, OwnerUserID, PlanID, Channel string; FinalAttempt bool } // workspace and owner travel in the River job args so every read stays scoped
func (h *SendReminderHandler) Handle(ctx context.Context, request SendRequest) error

type RecordReceiptHandler struct { Deliveries ports.DeliveryStore; Now func() time.Time }
type ReceiptRequest struct { ProviderMessageID string; Delivered bool; ErrorCode string }
func (h *RecordReceiptHandler) Handle(ctx context.Context, request ReceiptRequest) error
// maps Delivered=true ⇒ domain received_ok, false ⇒ received_failed; delivery not found ⇒ nil (safe ignore, logged)

// queries
type DeliveryStatsHandler struct { Deliveries ports.DeliveryStore }       // Handle(ctx, workspaceID) (dto.DeliveryCounts, error)
type ListDeliveriesHandler struct { Deliveries ports.DeliveryStore }      // Handle(ctx, workspaceID, dto.DeliveryFilter) ([]domain.ReminderDelivery, error); default limit 50, max 200, clamp offset ≥ 0
type ReminderOpsHandler struct { Ops ports.OpsStore; Now func() time.Time } // Handle(ctx) (dto.OpsView, error) — window constant 24h
```

`SendReminder` algorithm (spec §6.2): load plan by `(request.WorkspaceID, request.PlanID)` ⇒ missing plan ⇒ nil + log; revoked plan ⇒ suppress(plan_revoked) via the idempotency-keyed delivery. Load todo via `Todos.Get(request.WorkspaceID, request.OwnerUserID, plan.TodoID)`: `ErrTodoNotFound` or Status `deleted` ⇒ suppress(todo_deleted); `completed` ⇒ suppress(todo_completed); `TodoView.ReminderVersion != plan.TodoReminderVersion` ⇒ suppress(version_stale). Delivery by idempotency key: final state ⇒ nil (idempotent); missing ⇒ defensive `NewDelivery`+`Save` using `TodoView.Title` and `request.OwnerUserID` + log. `MarkSending` + `Update`. `Channels.Resolve(request.WorkspaceID, request.OwnerUserID, request.Channel)`: not usable ⇒ suppress(channel_unavailable). Notify via the handler matching `request.Channel`; success ⇒ `MarkSucceeded`+`Update`; `errors.Is(err, ports.ErrPermanent)` ⇒ `MarkFailed(code)`+`Update`+dead-letter log+nil; other error ⇒ `FinalAttempt` ? `MarkFailed("retry_exhausted")`+`Update`+dead-letter log+nil : return err. Suppression of a delivery whose row is missing still creates the row (so the dashboard/audit shows the reason).

- [ ] **Step 1 (red):** fake-port tests with fixed clock: plan revoked ⇒ suppressed(plan_revoked), notifier never called; todo deleted/completed/stale-version/todo-not-found ⇒ the four suppression reasons; delivery already `succeeded` ⇒ nil, no notifier call, no update; delivery missing ⇒ defensive insert then normal flow; channel unusable ⇒ suppressed(channel_unavailable); notifier success ⇒ succeeded with provider message ID + submitted/finalized; permanent error ⇒ failed terminal with code + dead-letter logged; transient + `FinalAttempt=false` ⇒ error returned, state still `sending`, attempt incremented; transient + `FinalAttempt=true` ⇒ failed("retry_exhausted") + nil; second send after success is a no-op. Receipt: succeeded delivery ⇒ receipt applied once (second call no-op); non-succeeded ⇒ ignored; unknown provider ID (`ErrDeliveryNotFound`) ⇒ nil. Queries: limit clamping (0⇒50, 300⇒200), negative offset ⇒ 0. Run `go test ./backend/internal/modules/reminder/application/... -race` ⇒ FAIL.
- [ ] **Step 2 (green):** implement; `go test ./backend/internal/modules/reminder/application/... -race -v` ⇒ PASS; `make architecture-test` ⇒ PASS (application imports no river/smtp/net-http).
- [ ] **Step 3: Commit** `feat: add reminder delivery command and queries`.

### Task 7: Reminder Postgres Adapters — Deliveries, Ops, Fake-Outbox Reader

**Files:**
- Create: `…/reminder/adapters/outbound/postgres/{deliveries,ops,outbox_reader}.go`
- Modify: `…/reminder/adapters/outbound/postgres/{plans.go,integration_test.go}` (add `Get`)
- Create: `…/reminder/adapters/outbound/fake/outbox.go` (+test)

**Interfaces:** `postgres.NewDeliveryStore(pool) ports.DeliveryStore`, `postgres.NewOpsStore(pool) ports.OpsStore`, `postgres.NewOutboxReader(pool)` with `LatestByAddress(ctx, address string, limit int)` returning rows `{Address, Channel, TodoID, Body, CreatedAt}`; `plans.Get` maps no-rows to `domain.ErrPlanNotFound` (add the error to `domain/errors.go`). All methods resolve the executor via `database.ExecutorFromContextOr(ctx, s.pool)`; unique violation `23505` on deliveries maps to `domain.ErrDeliveryExists`. `fake.NewOutbox(pool)` with `Write(ctx, channel, address, todoID, body string) error` (joins ambient tx like identity's fake outbox).

`List` status filter values: the five states plus `retrying` (= `state='sending' and attempt_count > 0`); plain `sending` matches any sending row. Stats SQL: conditional aggregation over `reminder.reminder_deliveries` scoped by `workspace_id`; `sending = state='sending' and attempt_count = 0`; `retrying = state='sending' and attempt_count > 0`. Ops SQL: queue rows from `river_job` `where queue in ('reminder_email','reminder_sms') and state in ('available','scheduled','retryable','running')` (`depth = count(*)`, `oldest_wait_seconds = greatest(0, floor(extract(epoch from (now - min(scheduled_at)))))`, one row per queue even when empty); delivery counts as above over all workspaces? — no: ops is instance-wide (not workspace-scoped) because it is operational data; document that. `retry_rate = attempt_count>0 / total` over instance deliveries (0 when none); `latency_p95_ms = percentile_cont(0.95) within group (order by extract(epoch from (submitted_at - scheduled_at))*1000)` where `state='succeeded' and submitted_at >= now - $window` (0 when none).

- [ ] **Step 1 (red):** integration tests (skip without `TEST_DATABASE_URL`; migrations from `filepath.Join("..","..","..","..","..","deploy","migrations")`; unique UUIDs + `t.Cleanup` deletion per existing precedent): delivery save/get round-trip incl. nullable optionals; duplicate idempotency key ⇒ `ErrDeliveryExists`; update round-trips state transitions and receipt fields; `SetProviderJobID` + `PlannedJobIDs` returns only non-null jobs for plans ≤ version cutoff; stats counts against seeded fixtures covering all six buckets; list ordering `created_at desc`, status filter, limit/offset; `ByProviderMessageID` found/not-found; plan `Get` scoped (workspace B cannot read workspace A's plan ⇒ `ErrPlanNotFound`); ops against seeded `river_job` rows inserted directly (available/scheduled + one completed ignored) and seeded deliveries incl. one succeeded with known latency ⇒ p95 matches the seeded value; fake outbox write + `LatestByAddress` latest-5 ordering and address isolation. Run ⇒ FAIL.
- [ ] **Step 2 (green):** implement; `TEST_DATABASE_URL=… go test ./backend/internal/modules/reminder/adapters/outbound/... -race -v` ⇒ PASS.
- [ ] **Step 3: Commit** `feat: add reminder delivery postgres and fake outbox adapters`.

### Task 8: River Adapters — Scheduler and Worker (contract tests)

**Files:**
- Create: `…/reminder/application/dto/jobargs.go` — `ReminderSendArgs{PlanID, WorkspaceID, OwnerUserID, TodoID string; TodoReminderVersion int; Channel string}` with `func (ReminderSendArgs) Kind() string { return "reminder_send" }` (plain struct; application imports no River)
- Create: `…/reminder/adapters/outbound/river/scheduler.go` + `scheduler_integration_test.go`
- Create: `…/reminder/adapters/inbound/worker/worker.go` + `worker_test.go`

**Interfaces:**

```go
// outbound/river
func New(client *river.Client[pgx.Tx]) *Scheduler // implements ports.JobScheduler
// Schedule: ExecutorFromContext ⇒ pgx.Tx assertion (both failures ⇒ typed error, no fallback);
// per channel: client.InsertTx(ctx, tx, dto.ReminderSendArgs{PlanID, WorkspaceID, OwnerUserID, TodoID, TodoReminderVersion, Channel}, &river.InsertOpts{Queue: "reminder_" + channel, ScheduledAt: job.ScheduledAtUTC});
// returns []ports.ScheduledChannel{{Channel, JobID: rj.ID}}
// Cancel: client.JobCancelByID(ctx, jobID); error passed through (best-effort is the caller's policy)

// inbound/worker
type SendWorker struct {
    river.WorkerDefaults[dto.ReminderSendArgs]
    Handler *remindercommand.SendReminderHandler
    MaxAttempts int
}
func (w *SendWorker) Work(ctx context.Context, job *river.Job[dto.ReminderSendArgs]) error
// → Handler.Handle(ctx, SendRequest{WorkspaceID: job.Args.WorkspaceID, OwnerUserID: job.Args.OwnerUserID, PlanID: job.Args.PlanID, Channel: job.Args.Channel, FinalAttempt: job.Attempt >= w.MaxAttempts})
func (w *SendWorker) NextRetryDelay(attempt int) time.Duration // min(500ms * 2^(attempt-1), 60s)
```

- [ ] **Step 1 (red):** unit test for `SendWorker` with a recording `SendReminderHandler` fake: args mapped exactly; `Attempt == MaxAttempts` ⇒ `FinalAttempt=true`; `NextRetryDelay` series (1→500ms, 2→1s, 3→2s, capped at 60s). Integration tests (skip without `TEST_DATABASE_URL`; run migrations; build a real insert-only River client over the pool): (a) atomicity — inside a `TxRunner.Run`, schedule via the adapter, query `river_job` within the tx (row visible), roll back ⇒ row absent; commit ⇒ row present with `queue='reminder_email'` and `scheduled_at` matching; (b) cancel — committed scheduled job ⇒ `Cancel` ⇒ `river_job.state='cancelled'`; (c) end-to-end — schedule with `ScheduledAtUTC = now+1s`, start a River worker client (queues `reminder_email`/`reminder_sms`, 1 worker each) running a `SendWorker` wired to fakes that record calls; poll ≤ 15 s until the fake notifier records one call and the delivery row is `succeeded`; (d) duplicate execution — call the handler a second time for the same args ⇒ notifier count stays 1; (e) retry — notifier fake fails once with a transient error then succeeds ⇒ delivery `succeeded`, `attempt_count=2`; (f) crash recovery proxy — notifier fake sleeps 2 s; start client, stop it with a 500 ms budget mid-flight, restart the client ⇒ eventually exactly one outbox/success record (bounded 20 s). Run ⇒ FAIL.
- [ ] **Step 2 (green):** implement; `TEST_DATABASE_URL=… go test ./backend/internal/modules/reminder/adapters/... -race -v -p=1` ⇒ PASS; `make architecture-test` ⇒ PASS.
- [ ] **Step 3: Commit** `feat: add River scheduler and reminder worker adapters`.

### Task 9: Provider Adapters — Fake, SMTP, Aliyun

**Files:**
- Create: `…/reminder/adapters/outbound/fake/{notify.go,notify_test.go}`
- Create: `…/reminder/adapters/outbound/smtp/{notifier.go,notifier_test.go}`
- Create: `…/reminder/adapters/outbound/aliyun/{notifier.go,receipt.go,notifier_test.go,receipt_test.go}`

**Interfaces:**

```go
// fake: NewEmail(outbox *Outbox) ports.EmailNotifier; NewSms(outbox *Outbox) ports.SmsNotifier
// both render fixed templates (email subject "工作台提醒"; body/sms text includes 《title》 and UTC instant),
// write via outbox.Write, return SendResult{ProviderMessageID: "fake-" + 16-hex};
// test knobs are struct fields: FailError error, Delay time.Duration (zero values in wired defaults)
// smtp: type Config struct { Host string; Port int; Username, Password, From string; Timeout time.Duration }
// func New(cfg Config) *Notifier — implements ports.EmailNotifier
// aliyun: type Config struct { Endpoint, AccessKeyID, AccessKeySecret, SignName, TemplateCode string; Timeout time.Duration }
// func New(cfg Config) *Notifier (implements ports.SmsNotifier)
// func ParseSmsReport(body []byte) (dto.ReceiptPayload, error)
```

SMTP behavior: dial with timeout (injectable `dial func(network, addr string, timeout time.Duration) (net.Conn, error)` for tests), `smtp.NewClient`, `PlainAuth` when username set, `Mail`/`RCPT`/`Data` with headers `From/To/Subject/Date`; response 5xx ⇒ `fmt.Errorf("%w: code=%d", ports.ErrPermanent, code)`; 4xx / dial / timeout ⇒ transient error; success ⇒ generated provider message ID (`smtp-` + hex). Aliyun behavior: RPC `SendSms` (Version `2017-05-25`), params sorted + percent-encoded, `StringToSign = "POST&%2F&" + percentEncode(canonicalQuery)`, signature `base64(hmac-sha1(secret + "&", stringToSign))`; injectable `do func(*http.Request) (*http.Response, error)` + `nonce func() string` + `now` for tests. Response `{Code, Message, BizId}`: `OK` ⇒ `SendResult{BizId}`; `isThrottled` / HTTP 5xx / timeout ⇒ transient; other codes ⇒ `ErrPermanent` wrap carrying the code. `ParseSmsReport` parses the fixture shape `{"phone_number","send_time","report_time","success","err_code","err_msg","biz_id"}` ⇒ `dto.ReceiptPayload{ProviderMessageID: biz_id, Delivered: success, ErrorCode: err_code}`; malformed ⇒ error.

- [ ] **Step 1 (red):** fake: templates contain title + instant; outbox write captured; `FailError` surfaced; sms/email distinct channel values. SMTP: local `net.Listen` stub speaking minimal SMTP (scripted replies): happy path ⇒ success + message recorded body contains title; `550` on RCPT ⇒ `errors.Is(err, ports.ErrPermanent)`; `452` ⇒ non-permanent error; dial refusal ⇒ transient. Aliyun: `httptest.Server` capturing the request — assert signature present and stable for fixed nonce/timestamp, `Code=OK` ⇒ BizId returned; `isThrottled` ⇒ transient; `isv.MOBILE_NUMBER_ILLEGAL` ⇒ permanent; HTTP 500 ⇒ transient; timeout via context. Receipt parser: golden fixture (delivered true/false), missing fields ⇒ error. Run `go test ./backend/internal/modules/reminder/adapters/outbound/{fake,smtp,aliyun} -race` ⇒ FAIL.
- [ ] **Step 2 (green):** implement; tests PASS; `make architecture-test` ⇒ PASS.
- [ ] **Step 3: Commit** `feat: add fake, SMTP, and Aliyun reminder provider adapters`.

### Task 10: Reminder HTTP Inbound — List, Ops, Receipts, Dev Outbox

**Files:**
- Create: `…/reminder/adapters/inbound/http/{reminders,ops,receipts,dev_outbox,json,handler_test}.go`

**Interfaces:**

```go
type Handler struct {
    List    *reminderquery.ListDeliveriesHandler
    Ops     *reminderquery.ReminderOpsHandler
    Receipt *remindercommand.RecordReceiptHandler
    Parse   func(body []byte) (reminderdto.ReceiptPayload, error) // generic JSON parser lives here; cmd may swap in aliyun.ParseSmsReport
    Secret  string
}
func RegisterRoutes(mux *http.ServeMux, auth func(http.Handler) http.Handler, h *Handler) // routes 23–25
func NewDevOutboxHandler(store DevOutboxStore) http.HandlerFunc // route 26, latest 5, mirrors identity's dev inbox shape
type DevOutboxStore interface { LatestByAddress(ctx context.Context, address string, limit int) ([]DevOutboxMessage, error) }
type DevOutboxMessage struct { Address, Channel, TodoID, Body string; CreatedAt time.Time }
```

Generic receipt JSON: `{"providerMessageId": string, "delivered": bool, "errorCode"?: string}`. Webhook: read body (≤ 64 KiB), hex HMAC-SHA256 over raw body with `Secret`, constant-time compare against `X-Receipt-Signature`; missing/invalid ⇒ 401 `invalid_signature`; parse failure ⇒ 422 `validation_error`; success ⇒ 200 `{}`. List: `status` validated against six enum values (`retrying` allowed as filter alias for sending∧attempt>0 — map in query), `limit`/`offset` bounds ⇒ 422 otherwise. Reminder module gets its own `json.go` helpers following the identity/todo pattern.

- [ ] **Step 1 (red):** httptest + fake application services: list returns DeliveryView JSON (camelCase, omitempty), 401 without principal, 422 for bad status/limit; ops returns ReminderOps JSON, 401 unauthenticated; webhook — valid signature ⇒ 200 and `RecordReceipt` called with parsed payload; tampered body ⇒ 401; missing header ⇒ 401; malformed JSON with valid signature ⇒ 422; unknown provider ID ⇒ still 200 (safe ignore); dev outbox — latest 5 by address, missing address ⇒ 422, JSON shape `{messages:[…]}`. Run ⇒ FAIL.
- [ ] **Step 2 (green):** implement; `go test ./backend/internal/modules/reminder/adapters/inbound/http -race -v` ⇒ PASS; `make architecture-test` ⇒ PASS.
- [ ] **Step 3: Commit** `feat: add reminder HTTP list, ops, receipts webhook, and dev outbox`.

### Task 11: Todo Module — Title Seam and Real Dashboard Reminder Counters (green)

**Files:**
- Modify: `…/todo/application/ports/reminder.go` (`PlanReminderRequest` + `Title string`; new `ReminderCounts` + `ReminderStats`)
- Modify: `…/todo/application/command/{create,update}.go` (+tests)
- Modify: `…/todo/application/dto/dashboard.go`, `…/todo/application/query/dashboard.go` (+tests)

**Interfaces:**

```go
// ports/reminder.go
type PlanReminderRequest struct { …existing…; Title, OwnerUserID string }
type ReminderCounts struct { Succeeded, Retrying, Failed, Suppressed int }
type ReminderStats func(ctx context.Context, workspaceID string) (ReminderCounts, error)

// dto/dashboard.go gains:
ReminderSucceeded  int `json:"reminderSucceeded"`
ReminderSuppressed int `json:"reminderSuppressed"`
```

`CreateTodoHandler`/`UpdateTodoHandler` pass `Title: todo.Title, OwnerUserID: todo.OwnerUserID` / `Title: loaded.Title, OwnerUserID: loaded.OwnerUserID` in `PlanReminderRequest`. `DashboardSummaryHandler` gains `ReminderStats ports.ReminderStats`; nil ⇒ all reminder counters stay 0 (keeps existing tests green); non-nil error ⇒ propagated; counts map Retrying→`ReminderRetrying`, Failed→`ReminderFailed`, plus the two new fields.

- [ ] **Step 1 (red):** create/update tests assert the recorded `Plan` request now carries the todo title (existing cases extended, not replaced); dashboard tests: nil stats ⇒ four zero reminder counters; fake stats `{7,2,3,5}` ⇒ summary fields `reminderSucceeded=7, reminderRetrying=2, reminderFailed=3, reminderSuppressed=5`; stats error ⇒ handler error. Run `go test ./backend/internal/modules/todo/... -race` ⇒ FAIL.
- [ ] **Step 2 (green):** implement; `go test ./backend/internal/modules/todo/... -race -v` ⇒ PASS; `make architecture-test` ⇒ PASS.
- [ ] **Step 3: Commit** `feat: carry todo title into plans and real dashboard reminder counters`.

### Task 12: Composition Roots — API and Worker Wiring (yellow)

**Files:**
- Modify: `backend/cmd/api/wiring.go`, `backend/cmd/api/composition_integration_test.go`
- Modify: `backend/cmd/worker/main.go`, `backend/cmd/worker/main_test.go`

**Interfaces:** API `buildHandler` gains: River insert-only client `river.NewClient(riverpgxv5.New(pool), &river.Config{})` wrapped by `riversched.New` (replacing `noopjob.New()` in `buildTodoHandlers`); `ChannelsProvider` wired from identity's `ChannelStore.ListByUser` (cmd-local shim: usable ⇒ `string(kind)`, sorted, deterministic — replaces the ITER-0002 empty snapshot); reminder block — delivery store, `SendReminderHandler` deps (`TodoReader` as a cmd-local shim over todo's postgres store `Get(ctx, workspaceID, ownerUserID, todoID)` mapping to reminder's `dto.TodoView`, `ChannelResolver` shim over identity's `ChannelStore.ListByUser`, notifiers selected by `cfg.ReminderEmailAdapter`/`cfg.ReminderSmsAdapter`: `fake` ⇒ `fake.NewEmail/NewSms(postgres outbox)`, `smtp` ⇒ `smtp.New(…)`, `aliyun` ⇒ `aliyun.New(…)`), reminder routes behind auth, gated `NewDevOutboxHandler` (`cfg.ReminderDevOutboxEnabled && cfg.AppEnv != production`), receipt parser selection (`aliyun.ParseSmsReport` when sms adapter is `aliyun`, generic otherwise), dashboard `ReminderStats` shim over the delivery store's `Stats`. Worker `run()` gains a third goroutine: build `SendReminderHandler` (same deps minus HTTP), `river.NewWorkers` + `river.AddWorker(workers, &reminderworker.SendWorker{Handler, MaxAttempts: cfg.ReminderJobMaxAttempts})`, client with `Queues: {"reminder_email": {Workers: cfg.ReminderQueueEmailConcurrency}, "reminder_sms": {…}}`, `client.Start(ctx)`; an `atomic.Bool` set after successful start feeds `ready`; on ctx cancel `client.Stop`; `errs` channel widened to 3. The existing `failingScheduler` composition test is updated to the evolved `JobScheduler` signature (returns `([]ScheduledChannel, error)`).

- [ ] **Step 1 (red):** composition tests (skip without `TEST_DATABASE_URL`): (a) atomicity with a failing fake scheduler preserved (todo + plan + delivery rows all absent on failure; present with `provider_job_id` NULL replaced by real IDs when wired to the River scheduler — add a River-backed variant asserting `river_job` rows exist after commit); (b) channels snapshot — register + verify an email and an sms channel via the public identity handlers, create a due todo ⇒ plan `requested_channels = {email,sms}` and two delivery rows; (c) dashboard counters — seed deliveries via the store (one per state incl. sending∧attempt>0) ⇒ `GET /api/v1/dashboard/summary` returns the four real counters; (d) receipts route wired — valid HMAC callback flips `receiptState`. Worker: `main_test.go` config-validation cases for the new fields (existing style). Run ⇒ FAIL.
- [ ] **Step 2 (green):** implement wiring; `go build ./backend/cmd/api ./backend/cmd/worker` ⇒ PASS; `TEST_DATABASE_URL=… go test ./backend/cmd/... -race -v -p=1` ⇒ PASS.
- [ ] **Step 3: Commit** `feat: wire River scheduler, reminder routes, and reminder worker`.

### Task 13: OpenAPI Contracts + Contract Tests (yellow)

**Files:**
- Create: `contracts/openapi/reminder.yaml`
- Modify: `contracts/openapi/dashboard.yaml`
- Create: `tests/contract/reminder_contract_test.go`
- Modify: `tests/contract/dashboard_contract_test.go`

**Requirements:** OpenAPI 3.1.1, hand-rolled traversal style of the existing tests (helpers in `contract_types_test.go`), closed objects, exact required sets. `reminder.yaml` paths: `/api/v1/reminders` (get 200/401/422), `/api/v1/ops/reminder` (get 200/401), `/api/v1/webhooks/receipts/sms` (post 200/401/422, description states HMAC-SHA256 `X-Receipt-Signature`), `/api/v1/dev/reminder-outbox` (get 200/422, description states double gating). Schemas: `DeliveryView` (required: id, todoId, todoTitle, channel enum email|sms, state enum scheduled|sending|succeeded|failed|suppressed, attemptCount, scheduledAt, createdAt; optional: suppressionReason enum, providerMessageId, lastErrorCode, submittedAt, finalizedAt, receiptState enum received_ok|received_failed, receiptAt, receiptErrorCode), `ReminderOps` (queues array of {queue, depth, oldestWaitSeconds}; deliveries object with six integer buckets; retryRate number; latencyP95Ms integer; checkedAt date-time), `ErrorEnvelope`. `dashboard.yaml`: `DashboardSummary` required set gains `reminderSucceeded`, `reminderSuppressed` (both integer). Contract tests assert all of the above and include mutation-rejection cases (drop `reminderSuppressed` from required; widen the state enum).

- [ ] **Step 1 (red):** write/extend contract tests first; `go test ./tests/contract -v` ⇒ FAIL.
- [ ] **Step 2 (green):** author/extend YAML until tests pass; `go test ./tests/contract -race -v` ⇒ PASS including untouched identity/todo/conversation/system-health tests.
- [ ] **Step 3: Commit** `docs: add reminder contract and extend dashboard contract`.

### Task 14: Web — Real Reminder Tiles and Records (green)

**Files:**
- Modify: `apps/web/src/features/dashboard/{fetch-dashboard.ts, dashboard-view.tsx, dashboard-panel.tsx}` (+tests)
- Create: `apps/web/src/features/dashboard/fetch-reminders.ts` (+test)

**Interfaces:** `DashboardSummary` gains `reminderSucceeded: number; reminderSuppressed: number` and the fail-closed validator expects exactly 10 keys. `fetchReminderDeliveries(baseURL, fetcher, timeoutMs?)` → `ReminderDelivery[] | null`, validator exact keys mirroring `DeliveryView` (nullable fields accepted as absent). `dashboard-view.tsx` renders nine tiles (existing five + 提醒成功/重试中/失败/被抑制) and a "提醒记录" section: rows `《title》 · 通道 · 状态 · scheduledAt`, empty state "暂无提醒记录", receipt state shown when present. `dashboard-panel.tsx` fetches summary + records; records failure degrades to the summary-only view with an inline note (summary failure still fail-closed alert). Constraints: injected fetcher, no `process.env`, no new deps, plain `globals.css` classes.

- [ ] **Step 1 (red):** validator tests (10-key summary valid; 8-key legacy payload ⇒ null; deliveries valid/missing-required/non-2xx/timeout ⇒ null); view tests (nine tiles with values incl. zeros; records list renders title/channel/state; empty state); panel tests (both fetches succeed; records fetch fails ⇒ summary still rendered + note; summary fails ⇒ role=alert). Run `corepack pnpm --filter @artificial-brain/web test` ⇒ FAIL.
- [ ] **Step 2 (green):** implement; web `test` ⇒ PASS; `lint` (tsc strict + eslint) ⇒ PASS; `build` ⇒ PASS.
- [ ] **Step 3: Commit** `feat: show real reminder counters and records on the dashboard`.

### Task 15: Integration Gates — Migration Pin, Compose Env, Smoke E2E (yellow)

**Files (all yellow):**
- Modify: `tests/smoke/migration_test.sh` — schema pin `5` → `7` (done in Task 2; re-assert here against the full gate)
- Modify: `compose.yaml` — api environment gains `REMINDER_EMAIL_ADAPTER: ${REMINDER_EMAIL_ADAPTER:-fake}`, `REMINDER_SMS_ADAPTER: ${REMINDER_SMS_ADAPTER:-fake}`, `REMINDER_RECEIPT_SECRET: ${REMINDER_RECEIPT_SECRET:-local-development-only}`, `REMINDER_DEV_OUTBOX_ENABLED: ${REMINDER_DEV_OUTBOX_ENABLED:-true}`; worker environment gains the same four adapter/gate vars plus `REMINDER_QUEUE_EMAIL_CONCURRENCY: ${REMINDER_QUEUE_EMAIL_CONCURRENCY:-2}`, `REMINDER_QUEUE_SMS_CONCURRENCY: ${REMINDER_QUEUE_SMS_CONCURRENCY:-2}`, `REMINDER_JOB_MAX_ATTEMPTS: ${REMINDER_JOB_MAX_ATTEMPTS:-5}`
- Modify: `.env.example` — append exactly those seven keys with the compose defaults (order must match the static test)
- Modify: `tests/smoke/stack_test.sh` — `expected_env` gains the seven new pairs; `full_stack_test` gains a bounded reminder E2E block after the ITER-0002 block (all curls `--max-time`; polling loops `≤ 30` iterations of `sleep 1`):

```text
e2e_email="smoke@example.com"; e2e_sms="+8613800137002"
1  POST settings/contact-channels {kind:"email",address:$e2e_email} ⇒ 201; GET dev/sms-inbox?address=$e2e_email ⇒ code; POST …/verify ⇒ verified
2  same pair for {kind:"sms",address:$e2e_sms}
3  POST todos {"title":"冒烟提醒","dueAtUTC":<now+5s UTC>} ⇒ 201 todoId
4  poll GET dev/reminder-outbox?address=$e2e_email ⇒ message body contains 冒烟提醒 (≤30s)
5  GET reminders ⇒ two rows for todoId, both state=succeeded (email + sms)
6  POST todos {"title":"冒烟抑制","dueAtUTC":<now+10s UTC>} ⇒ 201 id2; POST todos/{id2}/complete {version:1} ⇒ completed
7  poll psql: select state, suppression_reason from reminder.reminder_deliveries where todo_id='<id2>' ⇒ every row state='suppressed' and suppression_reason='todo_completed' (≤30s); GET dev/reminder-outbox for both addresses ⇒ no body contains 冒烟抑制 (revoke finalizes scheduled deliveries at revoke time — decisions.md D9)
8  sms providerMessageId from step 5 ⇒ POST webhooks/receipts/sms with valid HMAC-SHA256 body {"providerMessageId":…,"delivered":true} ⇒ 200; GET reminders ⇒ that row receiptState=received_ok
9  GET ops/reminder ⇒ jq '.queues | length == 2' and '.deliveries.succeeded >= 2'
10 docker compose restart worker; POST todos {"title":"冒烟恢复","dueAtUTC":<now+5s UTC>}; poll reminder-outbox ⇒ 冒烟恢复 delivered (≤30s)
```

- [ ] **Step 1 (red):** `sh tests/smoke/stack_test.sh --static-only` ⇒ FAIL (env mismatch) before edits complete.
- [ ] **Step 2 (green):** apply compose/.env.example/static edits; `--static-only` and `--config-only` ⇒ PASS.
- [ ] **Step 3 (green):** `make migration-test` ⇒ PASS (schema 7 pin; adapter suite via `./backend/internal/modules/...` wildcard already includes the new packages).
- [ ] **Step 4 (green):** `make smoke-test` ⇒ PASS including the reminder E2E block and worker-restart recovery.
- [ ] **Step 5: Commit** `build: wire ITER-0003 into migration and smoke gates`.

### Task 16: Unified Verification, Docs, Handoff, and Clean-Context Regression Gate

**Files:**
- Modify: `README.md` (new routes 23–26 in the business route list; reminder semantics paragraph: plan-time deliveries, suppression-at-execution, dead letters, receipts informational; env table gains the seven new variables; dev outbox login section extended; verification section unchanged)
- Modify: `docs/iterations/ITER-0003/{progress,test-matrix,handoff,decisions}.md`
- Create (regression phase): `docs/iterations/ITER-0003/regression-report.md`

- [ ] **Step 1:** Run the exact CI sequence locally: `corepack pnpm install --frozen-lockfile` → `make verify` → `make migration-test` → `make smoke-test` → `git status --short` (only intended files). All exit 0.
- [ ] **Step 2:** Fill every `test-matrix.md` row with command + evidence commit; `progress.md` all tasks complete, regression pending; `handoff.md` with HEAD, URLs (dashboard reminder section, `/api/v1/reminders`, `/api/v1/ops/reminder`, webhook, dev outbox), environment prerequisites (incl. GOPROXY note), no unresolved gaps. Commit `ci: finalize ITER-0003 verification evidence`.
- [ ] **Step 3 (clean-context regression):** give the independent reviewer only the master design + ITER-0003 brief/spec/plan/test-matrix/handoff, the merge-base…HEAD diff, and README/Makefile commands; restrictions = no implementation chat, inspect/test before modifying. Reviewer runs `make verify`, `make migration-test`, `make smoke-test`, `git diff --check origin/master...HEAD`, maps all 10 acceptance criteria to evidence, checks yellow/red zone compliance (only the two River deps added; migrations 001–005 untouched; health contracts untouched; CI workflow unchanged; no real-provider egress in CI), scans logs/responses for credential leakage (receipt secret, SMTP password, Aliyun keys).
- [ ] **Step 4:** reviewer writes immutable `regression-report.md`. On FAIL: implementation agent fixes via fresh red→green loops (`fix:` commits), then a **new** clean reviewer supersedes; the old report is never edited.
- [ ] **Step 5 (PASS):** mark iteration complete in `progress.md`/`handoff.md`; commit `docs: record ITER-0003 regression approval`.

## Yellow-zone register (must be handled deliberately; all listed in this plan)

| # | Item | Task |
|---|---|---|
| 1 | go.mod: +`riverqueue/river`, +`riverqueue/riverpgxv5` (only new deps, ADR-0002) | T2 |
| 2 | `deploy/migrations/006–007` + `database.CurrentSchemaVersion` 5→7 + `migration_test.sh` pin | T2, T15 |
| 3 | `backend/internal/platform/config` reminder fields + production fail-closed rules | T3 |
| 4 | `backend/cmd/api` rewiring (River client, channels provider, reminder routes, stats shim) + composition tests | T12 |
| 5 | `backend/cmd/worker` River worker start/stop + ready flag | T12 |
| 6 | `compose.yaml` api/worker env additions + `.env.example` exact keys | T15 |
| 7 | `tests/smoke/stack_test.sh` static env + reminder E2E block | T15 |
| 8 | `contracts/openapi/reminder.yaml` new + `dashboard.yaml` extended + contract tests | T13 |
| 9 | Root/backend/web `AGENTS.md` scope refresh | T1 |
| 10 | `README.md` operator documentation | T16 |
| 11 | Architecture policy: **no changes planned**; `make architecture-test` must pass as-is (River imports stay in adapters/cmd, already forbidden in application) | T4–T12 |
| 12 | CI workflow: **unchanged** — `tests/harness/workflow_test.go` pins `make verify → migration-test → smoke-test` | — |

## Assumptions register (record accepted ones in `decisions.md`)

- **A1** The `ChannelsProvider` is wired for real in ITER-0003 (nil in ITER-0002); plans created afterwards carry actual enabled+verified channel snapshots.
- **A2** Fake adapters run with zero delay and no failure injection in wired defaults; knobs exist only for tests.
- **A3** SMTP has no receipt channel this iteration; SMS receipts arrive via the generic webhook with fixtures — no MNS/AES integration.
- **A4** The receipt webhook authenticates with a single shared HMAC-SHA256 secret; per-provider key management is deferred.
- **A5** `ReminderSendArgs` JSON shape is the River job payload contract; payload evolution rides on the `reminder_send` kind.
- **A6** Ops "oldest wait" = `now - min(scheduled_at)` across active River jobs (`available|scheduled|retryable|running`), clamped at 0; ops data is instance-wide, not workspace-scoped.
- **A7** Worker-restart smoke step stands in for crash recovery; kill-mid-flight semantics are covered by the River adapter's stop/restart integration case (Task 8f).
- **A8** Defaults: queue concurrency 5/5 (compose overrides 2/2), `MaxAttempts` 5, retry backoff `min(500ms·2^(n−1), 60s)`.
- **A9** `provider_job_id bigint` matches `river_job.id`; job IDs travel as int64 through the port.
- **A10** `reminder.fake_outbox` stores rendered message bodies in plaintext (dev-only table; same exception class as identity's A7).
- **A11** Receipt lookup by `provider_message_id` is the single documented unscoped read (D6).

## Plan Completion Evidence

Before implementation begins, validate this plan itself:

```bash
git diff --check -- docs/superpowers/plans/2026-08-19-iter-0003-reliable-reminder-delivery.md
```

Expected: `git diff --check` exits 0 and no placeholder text remains.
