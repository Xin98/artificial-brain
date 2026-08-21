# ITER-0004 Private Deployment and Data Portability Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Close the MVP: one image stack serves cloud and private deployments (fixed private admin, no open registration), and users can take their data with them — streaming Export Bundles, two-phase preview/confirm import with Source Identity idempotency (conflicts never overwrite), verifiable backup/restore commands, an offline bundle target, and a same-version upgrade drill.

**Architecture:** Portability is a new bounded context (`backend/internal/modules/portability`) that reads and writes only through the Todo, Identity, and Reminder modules' public application handlers, adapted in `cmd` via consumer-owned ports and shims (the ITER-0003 D1 pattern). Export streams a zip (per-file sha256 tee, manifest last); import stores the validated bundle as bytea, previews by deterministic decision, and confirm re-decides from the same bundle before executing channels → todos → delivery history in one transaction. Private mode is a config switch: `DEPLOYMENT_MODE=private` provisions a fixed admin from `PRIVATE_ADMIN_PHONE` and rejects every other phone with `registration_closed`.

**Tech Stack:** Go 1.26.5, pgx 5.9.2, tern 2.4.1, Node.js 24.18.0, pnpm 11.19.0, Next.js, Vitest, PostgreSQL 18.4, Docker Compose, GitHub Actions. Zero new Go dependencies (archive/zip, crypto/sha256, encoding/csv, encoding/json, mime/multipart are stdlib). Zero new web dependencies.

## Global Constraints

1. Governing docs: master design `docs/superpowers/specs/2026-08-13-ai-native-personal-workbench-mvp-design.md` (§2.1, §4.5, §8, §9.2, §15 scenarios 10–11) and iteration design `docs/superpowers/specs/2026-08-21-iter-0004-private-deployment-and-data-portability-design.md` (decisions P1–P10). This iteration adds **no reverse proxy in the box** (P3, documented instead), **no open-ended chat, no MCP, no real-provider calls from CI** (private-mode smoke runs `APP_ENV=development` with fake adapters; production gating is unit-tested).
2. **Schema version bumps 7 → 8** via one append-only tern migration `008_portability.sql`. `database.CurrentSchemaVersion = 8`. Only `backend/cmd/migrate` migrates; API/Worker keep the equality `RequireSchema` gate. `tests/smoke/migration_test.sh` version pin moves `7` → `8`. Migrations 001–007 are red-zone, untouched.
3. **Dependencies:** zero new Go modules; zero new web dependencies; no new container images. go.mod and both package.json files must diff clean except version-irrelevant churn.
4. **Envelope reuse:** every response carries `X-Correlation-ID`; errors use the module-local `writeError` envelope `{code, message, correlationId}`. New stable codes: `registration_closed` (identity HTTP, 403), `unsupported_schema_version`, `checksum_mismatch`, `bundle_invalid`, `bundle_too_large` (portability HTTP, 422), `import_conflict` (portability HTTP, 409 — confirm on a committed or expired import; message distinguishes).
5. **Cross-context seam (architecture):** Portability imports nothing from other contexts at compile time — consumer-owned ports in `portability/application/ports`, concrete modules' public handlers adapted by shims in `backend/cmd/api` (same shape as `reminderPlannerShim`). The existing `go-cross-context` policy already forbids other contexts' `domain`/`adapters` imports; Task 1 adds a portability fixture proving it.
6. **Data isolation:** every portability read/write is workspace-scoped through the session principal. The Source Identity UNIQUE key is deliberately **instance-global** (`source_instance_id, source_record_id`, design §7.3): a second workspace importing the same bundle sees skipped/conflict, never copied data.
7. **Import never schedules reminders:** `Todo.ImportTodo` creates no Reminder Plan and no River Job; imported channels are unverified; imported deliveries carry `origin='imported'`, a NULL `plan_id`, and trigger no queue work.
8. **Clocks injected** (`Now func() time.Time`) in every handler; no real waits in unit tests. Bounded polling (≤ 30 s, `sleep 1`) only in smoke.
9. **Gating unchanged:** `APP_ENV=production` still forbids fake reminder adapters and dev inboxes/outboxes. `DEPLOYMENT_MODE=private` additionally requires `PRIVATE_ADMIN_PHONE` (E.164); `config.Load` fails on a missing/invalid value for `RoleAPI`, and fails if `PRIVATE_ADMIN_PHONE` is set with cloud mode (misconfiguration trap).
10. **Repo hygiene:** every task red→green→refactor, updates `docs/iterations/ITER-0004/{progress,test-matrix}.md`, commits only its listed files, never stages `.env` or credentials; `make verify` stays read-only. Commit trailers follow the repo convention (`Co-Authored-By`, `AI-Model`, `AI-Contributed/Feature`, `AI-Contributed/UT` last).

### Exact API route table (new and changed)

Existing routes 1–26 from ITER-0002/0003 are unchanged (the two login routes change behavior only in private mode). New routes:

| # | Method + Path | Auth | Request | Success | Error codes |
|---|---|---|---|---|---|
| 27 | `POST /api/v1/portability/export` | session | — | 200 `application/zip` stream, `Content-Disposition: attachment; filename="artificial-brain-export-<YYYYMMDD>.zip"` | 401 |
| 28 | `POST /api/v1/portability/imports` | session | `multipart/form-data`, field `bundle` (zip ≤ `PORTABILITY_MAX_BUNDLE_BYTES`) | 201 `{importId, preview}` | 401; 422 `bundle_too_large`\|`bundle_invalid`\|`unsupported_schema_version`\|`checksum_mismatch` |
| 29 | `GET /api/v1/portability/imports/{importId}` | session | — | 200 `ImportView` | 401; 404 |
| 30 | `POST /api/v1/portability/imports/{importId}/confirm` | session | — | 200 `ImportReport` | 401; 404; 409 `import_conflict` |

`ImportView` JSON: `{importId, state(pending|committed|expired), sourceInstanceId, preview:{new, skipped, conflicts, invalid, details:[{kind, sourceRecordId, outcome, reason?}], truncated}, report?:{new, skipped, conflicts, invalid, details:[…], committedAt}, createdAt}`. Preview details cap at 100 per outcome with `truncated: true`.

## Design decisions (record in `docs/iterations/ITER-0004/decisions.md`)

- **D1 — Portability via consumer-owned ports + cmd shims.** Same seam shape as ITER-0003's `reminderPlannerShim`: portability defines narrow ports; `cmd/api` adapts Todo/Identity/Reminder public handlers. Rejected: direct cross-context application imports (allowed by policy but couples the context) and internal HTTP/events (MVP §6.1).
- **D2 — Bundle streams; manifest last.** `archive.Writer` tees each entry through sha256 while encoding; counts and hashes land in `manifest.json` written as the final zip entry. No disk staging, no temp files.
- **D3 — Import stores the validated bundle as bytea** in `portability_imports` (≤ 32 MB default) so any API replica can confirm; confirm re-parses and re-decides from the stored bytes — preview never goes stale, execution happens exactly once (`state=committed`; a second confirm returns 409 `import_conflict` carrying the stored report).
- **D4 — Delivery history imports with NULL plan_id.** Migration 008 drops the NOT NULL on `reminder.reminder_deliveries.plan_id` (FK kept, nullable); imported rows carry `origin='imported'` and idempotency key `import:<sourceInstanceId>:<sourceRecordId>`. The plan-time insert path is untouched.
- **D5 — Fixed admin is provisioned, not generated.** `PRIVATE_ADMIN_PHONE` is required in private mode; the API provisions the admin user + workspace idempotently at startup through Identity's public `ProvisionAdmin` command. Rejected: log-printed generated phones (losing the log loses the admin).
- **D6 — Login gate sits in the application layer.** Both login handlers carry `PrivateAdminPhone`; when non-empty, any other phone fails with `domain.ErrRegistrationClosed` → HTTP 403 `registration_closed`. The web login flow is untouched (P1).
- **D7 — Export seams are dedicated export handlers** (`ExportTodos`, `ExportChannels`, `ExportDeliveries`) returning export DTOs, so `TodoStore.List` keeps its "never deleted" contract and the reminder delivery scan gains `origin` only on the export path.
- **D8 — No reverse proxy in the box** (P3): deploy/private documents the enterprise-proxy hookup and the LAN exposure risk; `deploy/AGENTS.md` records the deviation from master design §9.2.
- **D9 — Backup/restore are operator scripts with a CONFIRM gate**, smoke-tested against the smoke compose project via env-parameterized project/credentials; `make backup`/`make restore` wrap them for the default project.
- **D10 — Upgrade drill is same-architecture**: smoke writes data, recreates the stack (migrate re-runs idempotently), and proves data + health intact. Cross-version drills await the first release.

## File and Module Map

| Module / touchpoint | Interface (test surface) | Implementation files |
|---|---|---|
| Migrations (yellow) | tern 008; `CurrentSchemaVersion = 8` | `deploy/migrations/008_portability.sql`; `backend/internal/platform/database/schema.go` |
| Configuration (yellow) | `config.Load` + 3 fields | `backend/internal/platform/config/config.go` + `config_iter0004_test.go` |
| Portability domain | manifest/record validation, decision engine, fingerprints | `backend/internal/modules/portability/domain/*.go` (+tests) |
| Portability application | `ExportBundle`, `UploadImport`, `ConfirmImport`, `GetImport` + consumer-owned ports | `backend/internal/modules/portability/application/{command,query,ports,dto}/*.go` (+tests) |
| Portability adapters | archive writer/parser; postgres import/source/meta stores; HTTP routes | `backend/internal/modules/portability/adapters/{outbound/archive,outbound/postgres,inbound/http}/*.go` (+tests) |
| Identity seams (green) | `ProvisionAdmin`, `ImportChannel`, `ExportChannels`, login gate | `…/identity/application/{command/{provision,import_channel}.go,query/export.go,dto/channel.go}`, `command/login.go` (+tests) |
| Todo seams (green) | `domain.Restore`, `ImportTodo`, `ExportTodos`, `TodoStore.ListAll` | `…/todo/domain/todo.go`, `…/todo/application/{command/import_todo.go,query/export.go,dto/todo.go,ports/store.go}`, `adapters/outbound/postgres/store.go` (+tests) |
| Reminder seams (green) | `domain.RestoreDelivery` + `Origin`, `SaveImported`/`Export` store methods, `ImportDeliveries`, `ExportDeliveries` | `…/reminder/domain/delivery.go`, `…/reminder/application/{command/import_deliveries.go,query/export.go,dto/delivery.go,ports/store.go}`, `adapters/outbound/postgres/deliveries.go` (+tests) |
| Composition root (yellow) | instance-id + admin provisioning, portability wiring + shims | `backend/cmd/api/{wiring.go, provision.go, composition_integration_test.go}` |
| Contracts (yellow) | `portability.yaml` new; export schemas new | `contracts/openapi/portability.yaml`; `contracts/export-schemas/*.json`; `tests/contract/{portability,export_bundle}_contract_test.go` |
| Web (green) | `/data` page + `features/data` | `apps/web/src/features/data/*.ts(x)` (+tests); `apps/web/src/app/(workbench)/data/page.tsx`; `workbench-shell.tsx` |
| Deploy/harness (yellow) | private assets, runbooks, Make targets, compose env, smoke blocks | `deploy/private/**`; `docs/runbooks/{backup-restore,upgrade}.md`; `Makefile`; `compose.yaml`; `.env.example`; `tests/smoke/{stack_test.sh,migration_test.sh}`; `README.md`; AGENTS.md files |

### Config fields (added to `config.Config`, all env-parsed like ITER-0002/0003 fields)

| Field | Env | Default | Validation |
|---|---|---|---|
| `DeploymentMode` | `DEPLOYMENT_MODE` | `cloud` | `cloud`\|`private`; unknown values rejected naming the key |
| `PrivateAdminPhone` | `PRIVATE_ADMIN_PHONE` | — | required for `RoleAPI` when mode `private`, validated as E.164 via the identity phone pattern `^\+?[1-9][0-9]{6,14}$` (re-implemented locally in config — platform may not import modules); forbidden (error) when mode `cloud` |
| `PortabilityMaxBundleBytes` | `PORTABILITY_MAX_BUNDLE_BYTES` | `33554432` | int ≥ `1048576` |

---

### Task 1: Iteration Ledger, Policy Refresh, and Branch

**Files:**
- Create: `docs/iterations/ITER-0004/{brief,spec,plan,decisions,progress,test-matrix,handoff}.md`
- Create: `docs/superpowers/specs/2026-08-21-iter-0004-private-deployment-and-data-portability-design.md` (already committed at `060ea7b` — nothing to do, listed for ledger completeness)
- Create: `docs/superpowers/plans/2026-08-21-iter-0004-private-deployment-and-data-portability.md` (this plan)
- Create: `architecture/tests/testdata/invalid-cross-context-portability/backend/internal/modules/portability/application/bad.go`
- Modify: `architecture/policy/policy_test.go` (add the fixture's table entry)
- Modify (yellow): `AGENTS.md`, `backend/AGENTS.md`, `apps/web/AGENTS.md`, `deploy/AGENTS.md`

**Interfaces:** consumes master design §14.4 + iteration spec; produces `decisions.md` seeded with D1–D10 and `test-matrix.md` row IDs (CFG/MIG/PDM/IDT/TDO/RMD/PTA/HTP/CNT/WEB/DEP/SMK).

- [ ] **Step 1: Verify branch.** `git branch --show-current` prints `iter-0004-private-deployment-and-data-portability` (already cut from master at branch time); otherwise `git checkout -b iter-0004-private-deployment-and-data-portability master`.
- [ ] **Step 2: Author the ledger.** `brief.md` carries the spec's 10 acceptance criteria verbatim; `spec.md` points at the approved design (same shape as ITER-0003's); `plan.md` points at this plan; `progress.md` table (Task | Status | Evidence) lists all 18 tasks Pending; `test-matrix.md` table (Requirement | Command and evidence | Evidence commit | Status) has one row per row ID; `decisions.md` seeds D1–D10 plus an empty Working assumptions section; `handoff.md` stub pointing at brief/progress/decisions.
- [ ] **Step 3: Add the portability cross-context fixture and its table entry.** `architecture/tests/testdata/invalid-cross-context-portability/backend/internal/modules/portability/application/bad.go`:

```go
package application

import "github.com/Xin98/artificial-brain/backend/internal/modules/todo/domain"

var Todo = domain.Todo{}
```

and append the matching case to `TestValidateFixtures` in `architecture/policy/policy_test.go` (fixtures are exercised only through this table):

```go
{
	name: "portability imports another context's domain",
	root: "testdata/invalid-cross-context-portability",
	want: []policy.Violation{{
		File:   "backend/internal/modules/portability/application/bad.go",
		Rule:   "go-cross-context",
		Import: "github.com/Xin98/artificial-brain/backend/internal/modules/todo/domain",
	}},
},
```

- [ ] **Step 4: Refresh AGENTS.md zones.** Root `AGENTS.md`: green adds the Portability module and the `/data` web feature; yellow unchanged in scope but names the ITER-0004 register; red replaces the ITER-0003 wording: "in ITER-0004 do not call real providers from CI (private-mode smoke runs development fakes), do not commit credentials, do not lower CI gates, and migrations 001–007 stay untouched." `backend/AGENTS.md`: green includes `portability` and the identity/todo/reminder import-export seams; red drops "no Portability behavior" and keeps "no real-provider calls from CI". `apps/web/AGENTS.md`: green adds the ITER-0004 `/data` feature; red drops "do not add portability UI". `deploy/AGENTS.md`: green adds `deploy/private/**` assets and runbooks; add a note that the private stack deliberately ships no reverse proxy (master design §9.2 deviation, iteration decision D8).
- [ ] **Step 5: Verify.** `make harness-test` ⇒ PASS; `make architecture-test` ⇒ PASS (the repo scan skips `testdata`; the new table entry in `TestValidateFixtures` asserts the fixture produces exactly the one `go-cross-context` violation).
- [ ] **Step 6: Commit** `docs: open ITER-0004 iteration ledger` (ledger + AGENTS + fixture only).

### Task 2: Migration 008 and Schema 7→8 (yellow)

**Files:**
- Create: `deploy/migrations/008_portability.sql`
- Modify: `backend/internal/platform/database/schema.go` (`CurrentSchemaVersion int32 = 8`)
- Modify: `tests/smoke/migration_test.sh` (version pin `7` → `8` at the `[ "$schema_version" = 7 ]` check, ~line 140)
- Modify: `backend/internal/platform/database/migrate_integration_test.go` (assert version 8 + the new tables, same style as the existing version/table assertions)

**Interfaces:** produces schema v8 for every later task; `portability_imports`, `portability_source_records`, `instance_meta` tables and the delivery `origin` column.

- [ ] **Step 1 (red):** update `migrate_integration_test.go` first: after migration the schema version is 8, `public.instance_meta`, `portability.portability_imports`, `portability.portability_source_records` exist, `reminder.reminder_deliveries.origin` exists with default `local`, and `plan_id` is nullable. Run `go test ./backend/internal/platform/database -run TestMigrations -race` (needs `TEST_DATABASE_URL`) ⇒ FAIL (version still 7).
- [ ] **Step 2: Write the migration.**

```sql
-- 008_portability.sql
create schema if not exists portability;

create table public.instance_meta (
  key text primary key,
  value text not null,
  created_at timestamptz not null default now()
);

create table portability.portability_imports (
  id uuid primary key,
  workspace_id uuid not null,
  state text not null default 'pending',
  source_instance_id text not null,
  bundle bytea not null,
  report jsonb,
  created_at timestamptz not null default now(),
  committed_at timestamptz,
  constraint portability_imports_state_check check (state in ('pending', 'committed', 'expired'))
);

create table portability.portability_source_records (
  workspace_id uuid not null,
  source_instance_id text not null,
  source_record_id text not null,
  target_kind text not null,
  target_id text not null,
  content_fingerprint text not null,
  created_at timestamptz not null default now(),
  constraint portability_source_records_target_check check (target_kind in ('todo', 'channel', 'delivery')),
  constraint portability_source_records_identity_unique unique (source_instance_id, source_record_id)
);

alter table reminder.reminder_deliveries alter column plan_id drop not null;
alter table reminder.reminder_deliveries add column origin text not null default 'local';
alter table reminder.reminder_deliveries add constraint reminder_deliveries_origin_check check (origin in ('local', 'imported'));
```

- [ ] **Step 3:** bump `CurrentSchemaVersion` to 8; move the smoke migration pin to 8.
- [ ] **Step 4 (green):** `go test ./backend/internal/platform/database -race` ⇒ PASS; `make migration-test` ⇒ PASS (fresh empty schema reaches version 8; adapter DB proof green).
- [ ] **Step 5: Commit** `feat: add portability schema and delivery origin column`.

### Task 3: Platform Config Fields (yellow)

**Files:**
- Modify: `backend/internal/platform/config/config.go`
- Create: `backend/internal/platform/config/config_iter0004_test.go`

**Interfaces:** produces `cfg.DeploymentMode` (`config.DeploymentModeCloud`/`config.DeploymentModePrivate`), `cfg.PrivateAdminPhone`, `cfg.PortabilityMaxBundleBytes` consumed by Tasks 5, 12, 13, 16.

- [ ] **Step 1 (red):** table-driven tests in the ITER-0003 style (`reminderDevEnv`-style map + `mapLookup`): defaults (`cloud`, empty phone, 33554432); `DEPLOYMENT_MODE=private` without `PRIVATE_ADMIN_PHONE` fails for `RoleAPI` naming the key; private with `+8613800138000` loads; private with `not-a-phone` fails; `DEPLOYMENT_MODE=carrier` fails naming the key; `PRIVATE_ADMIN_PHONE` set with cloud mode fails; `PORTABILITY_MAX_BUNDLE_BYTES=12` fails (below 1 MiB), `=1048576` loads; worker/migrate roles ignore the private-admin requirement (only `RoleAPI` enforced). Run `go test ./backend/internal/platform/config -race` ⇒ FAIL.
- [ ] **Step 2 (green):** implement following the existing `valueOrDefault` + explicit-validation style; parse the phone with a config-local `regexp.MustCompile(`^\+?[1-9][0-9]{6,14}$`)` (platform must not import modules — the identity pattern is duplicated deliberately, recorded as assumption A1). Constants: `DeploymentModeCloud = "cloud"`, `DeploymentModePrivate = "private"`, `defaultDeploymentMode = "cloud"`, `defaultPortabilityMaxBundleBytes = 33554432`, `minimumPortabilityBundleBytes = 1048576`. Run package tests ⇒ PASS; `make architecture-test` ⇒ PASS.
- [ ] **Step 3: Commit** `feat: add deployment mode and portability configuration`.

### Task 4: Portability Domain — Manifest, Records, Decision Engine

**Files:**
- Create: `backend/internal/modules/portability/domain/{manifest,records,decision,fingerprint,errors}.go`
- Create: `backend/internal/modules/portability/domain/{manifest,decision,fingerprint}_test.go`

**Interfaces:**

```go
const SchemaVersion = "1"

type Manifest struct {
	SchemaVersion    string
	SourceInstanceID string
	ExportedAt       time.Time
	Counts           ManifestCounts
	Files            map[string]string // filename -> sha256 hex of the entry
}
type ManifestCounts struct{ Todos, Deliveries, Channels int }

func ValidateManifest(m Manifest) error // ErrUnsupportedSchemaVersion / ErrManifestInvalid (missing ids, negative counts, empty files)

type TodoRecord struct {
	ID              string
	Title           string
	Description     *string
	DueAtUTC        *time.Time
	TimezoneAtInput *string
	Status          string // pending|completed|deleted
	ReminderVersion int
	CreatedAt       time.Time
	UpdatedAt       time.Time
	CompletedAt     *time.Time
	DeletedAt       *time.Time
}
type ChannelRecord struct{ ID, Kind, Address string; Enabled bool }
type DeliveryRecord struct {
	ID, SourceTodoRecordID, Channel, State string
	SuppressionReason, ProviderMessageID, LastErrorCode *string
	AttemptCount            int
	TodoTitleSnapshot       string
	ScheduledAt, CreatedAt  time.Time
	SubmittedAt, FinalizedAt *time.Time
	ReceiptState, ReceiptErrorCode *string
	ReceiptAt               *time.Time
	Origin                  string
}

func ValidateTodoRecord(r TodoRecord) error       // ErrRecordInvalid with field reason
func ValidateChannelRecord(r ChannelRecord) error // kind must be email|sms; address non-empty
func ValidateDeliveryRecord(r DeliveryRecord) error

type Outcome string
const (
	OutcomeNew      Outcome = "new"
	OutcomeSkipped  Outcome = "skipped"
	OutcomeConflict Outcome = "conflict"
	OutcomeInvalid  Outcome = "invalid"
)

type ImportEntry struct{ Kind, SourceRecordID, Fingerprint string } // Kind: todo|channel|delivery
type Decision struct{ Kind, SourceRecordID string; Outcome Outcome; Reason string }

// Decide classifies validated entries against existing source records
// (keyed sourceInstanceID:sourceRecordID). Equal fingerprint ⇒ skipped,
// different ⇒ conflict, unseen ⇒ new. Deterministic order: input order.
func Decide(entries []ImportEntry, existing map[string]string) []Decision

// Fingerprint returns the sha256 hex of the record's canonical JSON
// (map[string]any marshaling — Go sorts map keys).
func Fingerprint(record any) string
```

Errors: `ErrUnsupportedSchemaVersion`, `ErrManifestInvalid`, `ErrRecordInvalid`, `ErrBundleStructure`, `ErrChecksumMismatch`, `ErrImportNotFound`, `ErrImportConflict`, `ErrImportExpired`, `ErrSourceRecordExists`.

- [ ] **Step 1 (red):** table-driven tests: manifest validation (bad version, empty source id, zero exported-at, missing file entries); record validation (empty id/title, unknown status/kind/channel/state, negative attempt count); `Decide` — unseen ⇒ new; same fingerprint ⇒ skipped; changed fingerprint ⇒ conflict; mixed input keeps order; `Fingerprint` — stable across field order (build the same record two ways), changes when any field changes, pointer fields nil vs set differ. Run `go test ./backend/internal/modules/portability/domain -race` ⇒ FAIL.
- [ ] **Step 2 (green):** implement; run domain tests ⇒ PASS; `make architecture-test` ⇒ PASS (stdlib-only domain).
- [ ] **Step 3: Commit** `feat: add portability domain manifest and decision engine`.

### Task 5: Identity Seams — Provision Admin, Login Gate, Import/Export Channels (green)

**Files:**
- Modify: `backend/internal/modules/identity/domain/errors.go` (add `ErrRegistrationClosed`)
- Modify: `backend/internal/modules/identity/application/command/login.go` (both handlers gain `PrivateAdminPhone string`)
- Create: `backend/internal/modules/identity/application/command/provision.go`
- Create: `backend/internal/modules/identity/application/command/import_channel.go`
- Create: `backend/internal/modules/identity/application/query/export.go`
- Modify: `backend/internal/modules/identity/application/dto/channel.go` (`ChannelPreference`)
- Tests: `command/login_test.go` (extend), `command/provision_test.go`, `command/import_channel_test.go`, `query/export_test.go` (new)

**Interfaces:**

```go
// command/provision.go
type ProvisionAdminHandler struct {
	Users      ports.UserStore
	Workspaces ports.WorkspaceStore
	NewID      func() string
	Now        func() time.Time
}
// Handle idempotently provisions the fixed private admin: an existing user
// for the phone is a no-op; otherwise a fresh personal workspace + user.
func (h *ProvisionAdminHandler) Handle(ctx context.Context, phone string) error

// command/import_channel.go
type ImportChannelHandler struct {
	Channels ports.ChannelStore
	NewID    func() string
	Now      func() time.Time
}
// Handle creates an UNVERIFIED channel; a duplicate (user, kind, address)
// returns domain.ErrChannelExists for the caller to classify.
func (h *ImportChannelHandler) Handle(ctx context.Context, principal dto.Principal, kind, address string, enabled bool) (dto.ContactChannelView, error)

// query/export.go
type ChannelsExportQuery struct{ Channels ports.ChannelStore }
// GetChannelPreferences returns kind/address/enabled only — never codes or
// verification state.
func (q *ChannelsExportQuery) GetChannelPreferences(ctx context.Context, principal dto.Principal) ([]dto.ChannelPreference, error)

// dto/channel.go addition
type ChannelPreference struct {
	ID      string `json:"id"`
	Kind    string `json:"kind"`
	Address string `json:"address"`
	Enabled bool   `json:"enabled"`
}
```

Login gate: both `RequestLoginChallengeHandler` and `VerifyLoginChallengeHandler` gain `PrivateAdminPhone string`; at the top of each `Handle`, after phone parsing, `if h.PrivateAdminPhone != "" && p.String() != h.PrivateAdminPhone { return …, domain.ErrRegistrationClosed }` (zero-value return for the request handler).

- [ ] **Step 1 (red):** login tests: empty `PrivateAdminPhone` keeps ITER-0003 behavior untouched; with it set, the admin phone flows unchanged and any other phone fails `ErrRegistrationClosed` before any store/outbox interaction (recording fakes stay empty). Provision: fresh phone saves workspace then user (capture order); existing user (`ByPhone` hit) saves nothing; `ByPhone` error propagates. ImportChannel: creates unverified+enabled-as-requested channel with parsed kind; invalid kind ⇒ `ErrInvalidChannelKind`; duplicate address ⇒ `ErrChannelExists` passthrough from store fake. Export: returns preferences without Verified/CodeHash fields, empty list for no channels. Run `go test ./backend/internal/modules/identity/... -race` ⇒ FAIL.
- [ ] **Step 2 (green):** implement; identity tests ⇒ PASS; `make architecture-test` ⇒ PASS.
- [ ] **Step 3: Commit** `feat: add private admin provisioning, login gate, and channel portability seams`.

### Task 6: Todo Seams — Restore Constructor, Import/Export (green)

**Files:**
- Modify: `backend/internal/modules/todo/domain/todo.go` (add `Restore`)
- Modify: `backend/internal/modules/todo/application/ports/store.go` (add `ListAll`)
- Modify: `backend/internal/modules/todo/application/dto/todo.go` (add `ImportTodoRequest`, `TodoExportRecord`)
- Create: `backend/internal/modules/todo/application/command/import_todo.go`
- Create: `backend/internal/modules/todo/application/query/export.go`
- Modify: `backend/internal/modules/todo/adapters/outbound/postgres/store.go` (`ListAll`)
- Tests: `domain/todo_test.go` (extend), `command/import_todo_test.go`, `query/export_test.go`, `adapters/outbound/postgres/integration_test.go` (extend)

**Interfaces:**

```go
// domain/todo.go addition
// Restore rebuilds a todo in any status with its historical identity and
// timestamps; it validates the title and status only — reminder planning is
// the caller's concern and import never plans.
func Restore(id, workspaceID, ownerUserID, title string, description *string,
	dueAtUTC *time.Time, timezoneAtInput *string, status Status,
	reminderVersion, version int, createdAt, updatedAt time.Time,
	completedAt, deletedAt *time.Time) (Todo, error)

// ports/store.go addition
// ListAll returns every todo for the owner regardless of status, ordered by
// created_at, capped at limit from offset — the export seam.
ListAll(ctx context.Context, workspaceID, ownerUserID string, offset, limit int) ([]domain.Todo, error)

// dto/todo.go additions
type ImportTodoRequest struct {
	WorkspaceID, UserID  string
	Title                string
	Description          *string
	DueAtUTC             *time.Time
	TimezoneAtInput      *string
	Status               string
	ReminderVersion, Version int
	CreatedAt, UpdatedAt time.Time
	CompletedAt, DeletedAt *time.Time
}
type TodoExportRecord struct {
	ID              string     `json:"id"`
	Title           string     `json:"title"`
	Description     *string    `json:"description,omitempty"`
	DueAtUTC        *time.Time `json:"dueAtUtc,omitempty"`
	TimezoneAtInput *string    `json:"timezoneAtInput,omitempty"`
	Status          string     `json:"status"`
	ReminderVersion int        `json:"reminderVersion"`
	CreatedAt       time.Time  `json:"createdAt"`
	UpdatedAt       time.Time  `json:"updatedAt"`
	CompletedAt     *time.Time `json:"completedAt,omitempty"`
	DeletedAt       *time.Time `json:"deletedAt,omitempty"`
}

// command/import_todo.go
type ImportTodoHandler struct {
	Store ports.TodoStore
	NewID func() string
	Now   func() time.Time
}
// Handle restores the todo exactly as recorded (no UoW of its own — it joins
// the caller's ambient transaction; no Planner — import never schedules, D7).
func (h *ImportTodoHandler) Handle(ctx context.Context, request dto.ImportTodoRequest) (dto.Todo, error)

// query/export.go
type ExportTodosHandler struct {
	Store ports.TodoStore
	Now   func() time.Time
}
// Handle pages the owner's full-history todos (offset, limit capped at 200)
// as export records; deleted and completed todos included.
func (h *ExportTodosHandler) Handle(ctx context.Context, workspaceID, ownerUserID string, offset, limit int) ([]dto.TodoExportRecord, error)
```

- [ ] **Step 1 (red):** domain: `Restore` builds each status with timestamps preserved; empty title / unknown status rejected; `New` unchanged. Command: import pending todo with due date never touches any planner (handler has none — the test asserts the store receives the restored aggregate with original version/timestamps); completed/deleted variants carry their instants; invalid request (empty workspace/user/title) errors. Query: export maps all statuses and caps limit at 200. Store `ListAll`: integration cases — returns deleted rows too, ordered by `created_at`, offset/limit paging, workspace+owner scoping (another workspace sees nothing). Run `go test ./backend/internal/modules/todo/... -race` ⇒ FAIL.
- [ ] **Step 2 (green):** implement; todo tests ⇒ PASS; `make architecture-test` ⇒ PASS.
- [ ] **Step 3: Commit** `feat: add todo restore constructor and portability seams`.

### Task 7: Reminder Seams — Imported Deliveries and Export (green)

**Files:**
- Modify: `backend/internal/modules/reminder/domain/delivery.go` (add `Origin` field + `RestoreDelivery`)
- Modify: `backend/internal/modules/reminder/application/ports/store.go` (add `SaveImported`, `Export`)
- Modify: `backend/internal/modules/reminder/application/dto/delivery.go` (add `DeliveryExportRecord`, `ImportDeliveryRequest`)
- Create: `backend/internal/modules/reminder/application/command/import_deliveries.go`
- Create: `backend/internal/modules/reminder/application/query/export.go`
- Modify: `backend/internal/modules/reminder/adapters/outbound/postgres/deliveries.go`
- Tests: `domain/delivery_test.go` (extend), `command/import_deliveries_test.go`, `query/export_test.go`, `adapters/outbound/postgres/integration_test.go` (extend)

**Interfaces:**

```go
// domain/delivery.go additions
// Origin distinguishes plan-time deliveries from imported history.
type DeliveryOrigin string
const (
	OriginLocal    DeliveryOrigin = "local"
	OriginImported DeliveryOrigin = "imported"
)

// RestoreDelivery rebuilds a historical delivery without a plan; the
// idempotency key is the caller's import key, states must be terminal or
// scheduled-with-history (all five states allowed — history is history).
func RestoreDelivery(id, workspaceID, ownerUserID, todoID string, todoReminderVersion int,
	channel, titleSnapshot, idempotencyKey string, state DeliveryState,
	suppressionReason *SuppressionReason, attemptCount int,
	providerMessageID, lastErrorCode *string,
	scheduledAt, createdAt time.Time, submittedAt, finalizedAt *time.Time,
	receiptState *ReceiptState, receiptAt *time.Time, receiptErrorCode *string) (ReminderDelivery, error)
// ReminderDelivery gains: Origin DeliveryOrigin (zero value == OriginLocal everywhere existing code constructs deliveries)

// ports/store.go additions (DeliveryStore)
SaveImported(ctx context.Context, delivery domain.ReminderDelivery) error // plan_id NULL, origin='imported'; ErrDeliveryExists on unique dup
Export(ctx context.Context, workspaceID string, offset, limit int) ([]domain.ReminderDelivery, error) // all states, created_at order, includes origin

// dto/delivery.go additions
type ImportDeliveryRequest struct {
	WorkspaceID, OwnerUserID, TodoID string
	TodoReminderVersion              int
	Channel, State                   string
	SuppressionReason, ProviderMessageID, LastErrorCode *string
	AttemptCount                     int
	TodoTitleSnapshot                string
	ScheduledAt, CreatedAt           time.Time
	SubmittedAt, FinalizedAt         *time.Time
	ReceiptState, ReceiptErrorCode   *string
	ReceiptAt                        *time.Time
	SourceInstanceID, SourceRecordID string
}
type DeliveryExportRecord struct {
	ID, TodoID, Channel, State     string
	SuppressionReason              *string `json:",omitempty"`
	AttemptCount                   int
	ProviderMessageID, LastErrorCode *string
	TodoTitleSnapshot              string
	ScheduledAt, CreatedAt         time.Time
	SubmittedAt, FinalizedAt       *time.Time `json:",omitempty"`
	ReceiptState, ReceiptErrorCode *string    `json:",omitempty"`
	ReceiptAt                      *time.Time `json:",omitempty"`
	Origin                         string
}

// command/import_deliveries.go
type ImportDeliveriesHandler struct {
	Deliveries ports.DeliveryStore
	NewID      func() string
	Now        func() time.Time
}
// Handle writes one read-only history row; idempotency key is
// "import:<sourceInstanceID>:<sourceRecordID>". No scheduler, no provider,
// no state-machine transitions.
func (h *ImportDeliveriesHandler) Handle(ctx context.Context, request dto.ImportDeliveryRequest) error

// query/export.go
type ExportDeliveriesHandler struct{ Deliveries ports.DeliveryStore }
// Handle pages the workspace's delivery history (offset, limit capped at 200).
func (h *ExportDeliveriesHandler) Handle(ctx context.Context, workspaceID string, offset, limit int) ([]dto.DeliveryExportRecord, error)
```

- [ ] **Step 1 (red):** domain: `RestoreDelivery` validates channel/state like `NewDelivery` but accepts every state and requires the idempotency key; existing `NewDelivery` behavior unchanged and its Origin is `local`. Command: handle builds key `import:inst:rec`, calls `SaveImported` once, no other port touched; duplicate ⇒ `domain.ErrDeliveryExists` passthrough. Query: caps limit at 200, maps origin. Store: `SaveImported` integration — inserts with NULL `plan_id`, `origin='imported'`, unique dup on the import key ⇒ `ErrDeliveryExists`; `Export` returns imported + local rows ordered by `created_at` with origin populated; existing Save/Update/List/Stats behaviors re-run green (the 008 default keeps them untouched). Run `go test ./backend/internal/modules/reminder/... -race` ⇒ FAIL.
- [ ] **Step 2 (green):** implement; reminder tests ⇒ PASS; `make architecture-test` ⇒ PASS.
- [ ] **Step 3: Commit** `feat: add reminder delivery import and export seams`.

### Task 8: Portability Application — Ports, DTOs, and ExportBundle

**Files:**
- Create: `backend/internal/modules/portability/application/ports/ports.go`
- Create: `backend/internal/modules/portability/application/dto/{bundle,preview,export}.go`
- Create: `backend/internal/modules/portability/application/command/export.go`
- Create: `backend/internal/modules/portability/application/command/export_test.go`

**Interfaces:**

```go
// ports/ports.go — consumer-owned; cmd/api adapts the modules' public handlers.
type Principal struct{ UserID, WorkspaceID string } // mirrored, no identity import

type TodoExporter interface {
	ExportTodos(ctx context.Context, workspaceID, userID string, offset, limit int) ([]dto.TodoExportRecord, error)
}
type ChannelExporter interface {
	ExportChannels(ctx context.Context, principal Principal) ([]dto.ChannelExportRecord, error)
}
type DeliveryExporter interface {
	ExportDeliveries(ctx context.Context, workspaceID string, offset, limit int) ([]dto.DeliveryExportRecord, error)
}
type InstanceIdentityStore interface {
	InstanceID(ctx context.Context) (string, error) // get-or-create handled by the adapter
}
// ArchiveWriter streams zip entries; WriteEntry tees sha256 per file;
// WriteManifest appends manifest.json last; Close finalizes the archive.
type ArchiveWriter interface {
	WriteEntry(ctx context.Context, name string, encode func(context.Context, io.Writer) error) error
	WriteManifest(ctx context.Context, manifest domain.Manifest) error
	Close() error
}
type ArchiveFactory func(w io.Writer) ArchiveWriter

// dto/bundle.go — wire shapes shared with the archive adapter (JSON tags match
// contracts/export-schemas exactly).
type TodoExportRecord struct { /* same fields as todo dto, json-tagged */ }
type ChannelExportRecord struct{ ID, Kind, Address string; Enabled bool }
type DeliveryExportRecord struct { /* same fields as reminder dto + SourceTodoRecordID */ }
```

`ExportBundleHandler{Instance InstanceIdentityStore, Todos TodoExporter, Channels ChannelExporter, Deliveries DeliveryExporter, Archive ArchiveFactory, PageSize int, Now func() time.Time}`; `Handle(ctx, principal Principal, out io.Writer) (domain.Manifest, error)` streams: todos.json (paged, counting), reminder-deliveries.json (paged — each record carries `sourceTodoRecordId` = its todo's source id, resolved through the todo page order: deliveries reference the todo id the reminder module stored, which IS the source todo record id), preferences.json, todos.csv (same paged source, re-streamed), then the manifest with counts + hashes from the writer. `PageSize` defaults to 200.

- [ ] **Step 1 (red):** with recording fakes + an in-memory archive fake capturing entry names/order/bytes: entries appear in order `todos.json`, `reminder-deliveries.json`, `preferences.json`, `todos.csv`, `manifest.json`; paging loops until a short page (fake returns 2 pages); counts match streamed records; delivery records carry the todo id as `sourceTodoRecordId`; manifest hashes equal sha256 of captured entry bytes; exporter error aborts with no manifest written; empty workspace produces zero-count manifest with empty arrays (not nulls). Run `go test ./backend/internal/modules/portability/application -race` ⇒ FAIL.
- [ ] **Step 2 (green):** implement; application tests ⇒ PASS; `make architecture-test` ⇒ PASS.
- [ ] **Step 3: Commit** `feat: add portability export bundle command`.

### Task 9: Portability Application — Upload, Preview, Confirm

**Files:**
- Modify: `backend/internal/modules/portability/application/ports/ports.go` (import-side ports)
- Create: `backend/internal/modules/portability/application/command/upload.go`
- Create: `backend/internal/modules/portability/application/command/confirm.go`
- Create: `backend/internal/modules/portability/application/query/get_import.go`
- Tests: `command/upload_test.go`, `command/confirm_test.go`, `query/get_import_test.go`

**Interfaces:**

```go
// ports/ports.go additions
type BundleParser interface {
	// Parse validates structure, manifest, checksums, and records; it returns
	// typed domain errors (ErrBundleStructure, ErrChecksumMismatch,
	// ErrUnsupportedSchemaVersion, ErrRecordInvalid).
	Parse(data []byte) (ParsedBundle, error)
}
type ParsedBundle struct {
	Manifest   domain.Manifest
	Todos      []domain.TodoRecord
	Deliveries []domain.DeliveryRecord
	Channels   []domain.ChannelRecord
}
type ImportStore interface {
	Save(ctx context.Context, imp dto.ImportRecordRow) error
	Get(ctx context.Context, workspaceID, importID string) (dto.ImportRecordRow, error) // ErrImportNotFound
	Commit(ctx context.Context, workspaceID, importID string, report dto.ImportReport, now time.Time) error
}
type SourceRecordStore interface {
	// Fingerprints returns existing fingerprints keyed "sourceInstanceID:sourceRecordID".
	Fingerprints(ctx context.Context, sourceInstanceID string, ids []string) (map[string]string, error)
	Register(ctx context.Context, record dto.SourceRecord) error // unique dup ⇒ domain.ErrSourceRecordExists
}
type TodoImporter interface {
	ImportTodo(ctx context.Context, principal Principal, record dto.TodoImportRequest) (string, error) // returns new todo id
}
type ChannelImporter interface {
	ImportChannel(ctx context.Context, principal Principal, record dto.ChannelImportRequest) (string, error) // ErrChannelExists passthrough
}
type DeliveryImporter interface {
	ImportDelivery(ctx context.Context, principal Principal, record dto.DeliveryImportRequest) error
}
// UnitOfWork mirrors todo's port; cmd injects the joinableUoW so a confirm
// joins exactly one transaction (the handler never begins one itself).
type UnitOfWork interface {
	Run(ctx context.Context, work func(context.Context) error) error
}
// dto: ImportRecordRow{ID, WorkspaceID, State, SourceInstanceID, Bundle []byte, Report *ImportReport, CreatedAt, CommittedAt *time.Time}
// dto: SourceRecord{WorkspaceID, SourceInstanceID, SourceRecordID, TargetKind, TargetID, ContentFingerprint}
// dto: Preview{New, Skipped, Conflicts, Invalid int, Details []Decision, Truncated bool}; Decision{Kind, SourceRecordID, Outcome, Reason string}
// dto: ImportReport mirrors Preview + CommittedAt

// command/upload.go
type UploadImportHandler struct {
	Imports  ports.ImportStore
	Sources  ports.SourceRecordStore
	Parser   ports.BundleParser
	NewID    func() string
	Now      func() time.Time
	ImportTTL time.Duration // 24h
}
// Handle validates, stores state=pending, and returns (importID, preview, error).
func (h *UploadImportHandler) Handle(ctx context.Context, principal Principal, bundle []byte) (string, dto.Preview, error)

// command/confirm.go
type ConfirmImportHandler struct {
	Imports    ports.ImportStore
	Sources    ports.SourceRecordStore
	Parser     ports.BundleParser
	Todos      ports.TodoImporter
	Channels   ports.ChannelImporter
	Deliveries ports.DeliveryImporter
	UoW        UnitOfWork // same shape as todo's port
	Log        *slog.Logger
	NewID      func() string
	Now        func() time.Time
	ImportTTL  time.Duration
}
// Handle re-parses the stored bundle, re-decides, and inside ONE transaction
// executes channels → todos → deliveries (delivery todo references resolved
// through this run's source-record mapping), registers source records, and
// commits the import with the report. A committed import returns
// domain.ErrImportConflict; an expired one (created before now-TTL) returns
// domain.ErrImportExpired.
func (h *ConfirmImportHandler) Handle(ctx context.Context, principal Principal, importID string) (dto.ImportReport, error)

// query/get_import.go
type GetImportQuery struct{ Imports ports.ImportStore; Now func() time.Time; ImportTTL time.Duration }
func (q *GetImportQuery) Handle(ctx context.Context, workspaceID, importID string) (dto.ImportView, error)
```

Execution-time classification rules inside Confirm: decision `new` but the target importer reports an existing entity (`ErrChannelExists`) ⇒ downgrade to skipped (the source record is still registered pointing at the existing target where the importer returns its id — channels return the existing id). A delivery whose `sourceTodoRecordId` has no mapping in this run or in `Sources` ⇒ invalid (skipped with reason `todo_not_found`).

- [ ] **Step 1 (red):** Upload: valid bundle saves row `pending` + returns preview with per-kind decisions against faked existing fingerprints; parser errors propagate typed (assert each domain error); zero-record bundle ⇒ empty preview, still stored. Confirm: happy path executes channels then todos then deliveries (recording fakes assert order), registers one source record per new row with the record fingerprint, commits with state `committed` and report counts; re-confirm ⇒ `ErrImportConflict` without touching importers; expired row ⇒ `ErrImportExpired`; importer failure ⇒ whole UoW fails and state stays pending (UoW fake returns the error); channel exists ⇒ skipped + registered pointing at returned existing id; orphan delivery ⇒ invalid; decisions recomputed from stored bytes, not from preview state. GetImport: pending/committed views; past TTL renders `expired`. Run application tests ⇒ FAIL.
- [ ] **Step 2 (green):** implement; application tests ⇒ PASS; `make architecture-test` ⇒ PASS.
- [ ] **Step 3: Commit** `feat: add portability import preview and confirm commands`.

### Task 10: Portability Postgres Adapters — Imports, Source Records, Instance Meta

**Files:**
- Create: `backend/internal/modules/portability/adapters/outbound/postgres/{imports,sources,meta,integration_test}.go`

**Interfaces:** `NewImportStore(pool) *ImportStore` implements `ports.ImportStore` (bundle bytea round-trip, report jsonb, `Commit` sets state + `committed_at` only from `pending` — a second commit reports `domain.ErrImportConflict` via zero rows updated); `NewSourceRecordStore(pool)` implements `ports.SourceRecordStore` (`Fingerprints` via `ANY($2)` batch select; `Register` maps unique violation to `domain.ErrSourceRecordExists`); `NewMetaStore(pool)` with `InstanceID(ctx) (string, error)` — `insert … on conflict (key) do nothing` then select, UUID generated by `crypto/rand` (D2 instance identity). All reads/writes resolve `database.ExecutorFromContextOr(ctx, s.pool)` so confirm's UoW wraps them; `Get`/`Fingerprints`/`Register` on source records are workspace-scoped except `Fingerprints` (instance-global by design §7.3 — the documented exception, mirroring D6's provider-keyed read).

- [ ] **Step 1 (red):** integration cases (needs `TEST_DATABASE_URL`, follow the reminder `integration_test.go` setup): import save/get round-trip preserves bundle bytes and nil report; commit flips state and sets `committed_at`; double commit ⇒ `ErrImportConflict`; get for another workspace ⇒ not found; source record register + fingerprint lookup; duplicate register ⇒ `ErrSourceRecordExists`; fingerprints for unknown ids absent; meta `InstanceID` creates once and returns the same value on repeat calls, even concurrently (two goroutines). Run `go test ./backend/internal/modules/portability/adapters/... -race` ⇒ FAIL.
- [ ] **Step 2 (green):** implement; adapter tests ⇒ PASS; `make architecture-test` ⇒ PASS.
- [ ] **Step 3: Commit** `feat: add portability postgres adapters`.

### Task 11: Portability Archive Adapter — Zip Streaming, Parse, CSV

**Files:**
- Create: `backend/internal/modules/portability/adapters/outbound/archive/{writer,parser,csv}.go`
- Create: `backend/internal/modules/portability/adapters/outbound/archive/{writer,parser}_test.go`

**Interfaces:**

```go
// writer.go
type Writer struct{ … } // implements ports.ArchiveWriter over archive/zip
func NewWriter(w io.Writer) *Writer
// WriteEntry opens name in the zip, tees encode's output through sha256, records the hex.
// WriteManifest marshals manifest.json (its Files map is filled from the recorded hashes first).
// Close closes the zip writer.

// parser.go
func Parse(data []byte) (portabilityports.ParsedBundle, error)
// Read-all via archive/zip over bytes.Reader; missing/extra entries ⇒
// domain.ErrBundleStructure; per-file sha256 vs manifest.Files ⇒
// domain.ErrChecksumMismatch; schemaVersion ⇒ domain.ErrUnsupportedSchemaVersion;
// record unmarshal/validation ⇒ domain.ErrRecordInvalid (first offender named).

// csv.go
func WriteTodosCSV(w io.Writer, records []dto.TodoExportRecord) error
// columns: id,title,status,dueAtUtc,timezoneAtInput,createdAt,completedAt,deletedAt
```

- [ ] **Step 1 (red):** writer: build a bundle with two entries + manifest into a buffer, reopen with `archive/zip` and assert entry order, contents byte-equal, manifest hashes correct; parser: round-trip a writer-produced bundle parses back to equal records; corrupt one byte of `todos.json` in the produced zip ⇒ `ErrChecksumMismatch`; drop `preferences.json` ⇒ `ErrBundleStructure`; manifest `schemaVersion: "2"` ⇒ `ErrUnsupportedSchemaVersion`; a todo with empty title ⇒ `ErrRecordInvalid` naming the record id; CSV: golden rows including RFC3339 timestamps and empty optionals. Run archive tests ⇒ FAIL.
- [ ] **Step 2 (green):** implement; archive tests ⇒ PASS; `make architecture-test` ⇒ PASS.
- [ ] **Step 3: Commit** `feat: add portability archive writer and parser`.

### Task 12: Portability HTTP Inbound — Export, Upload, Confirm, Get

**Files:**
- Create: `backend/internal/modules/portability/adapters/inbound/http/{handler,export,imports,json}.go`
- Create: `backend/internal/modules/portability/adapters/inbound/http/handler_test.go`

**Interfaces:**

```go
type Handler struct {
	Export     *command.ExportBundleHandler
	Upload     *command.UploadImportHandler
	Confirm    *command.ConfirmImportHandler
	Get        *query.GetImportQuery
	MaxBundleBytes int64
}
func RegisterRoutes(mux *http.ServeMux, auth func(http.Handler) http.Handler, h *Handler)
```

Routes (all behind `auth`): `POST /api/v1/portability/export` streams the zip (`Content-Type: application/zip`, `Content-Disposition: attachment; filename="artificial-brain-export-<YYYYMMDD>.zip"` from the injected clock); `POST /api/v1/portability/imports` reads multipart field `bundle` through an `io.LimitReader` at `MaxBundleBytes+1` (over ⇒ 422 `bundle_too_large` without buffering the excess), returns 201 `{importId, preview}`; `GET /api/v1/portability/imports/{importId}` returns the `ImportView`; `POST /api/v1/portability/imports/{importId}/confirm` returns 200 with the report. Error mapping (module-local `writeError` envelope, same shape as identity's json.go): parser `ErrUnsupportedSchemaVersion` ⇒ 422 `unsupported_schema_version`; `ErrChecksumMismatch` ⇒ 422 `checksum_mismatch`; `ErrBundleStructure`/`ErrRecordInvalid`/bad multipart/non-zip bytes ⇒ 422 `bundle_invalid`; `ErrImportNotFound` ⇒ 404 `not_found`; `ErrImportConflict`/`ErrImportExpired` ⇒ 409 `import_conflict` (message names committed vs expired). Principal extraction follows the established cross-module pattern: import `identity/application/dto` (as `todo` and `conversation` HTTP adapters do) and call `identitydto.PrincipalFromContext`, mapping into the portability `ports.Principal`.

- [ ] **Step 1 (red):** `httptest` cases with command fakes: export streams a valid zip (open the response body with archive/zip) with correct headers; upload happy path ⇒ 201 importId + preview JSON; oversized body ⇒ 422 `bundle_too_large` and the handler never calls the parser; each typed parser error ⇒ its stable code; missing multipart field ⇒ 422 `bundle_invalid`; confirm happy ⇒ 200 report; committed/expired ⇒ 409 `import_conflict`; unknown id ⇒ 404; every error body carries `correlationId`. Run `go test ./backend/internal/modules/portability/... -race` ⇒ FAIL.
- [ ] **Step 2 (green):** implement; portability tests ⇒ PASS; `make architecture-test` ⇒ PASS (HTTP stays in adapters/inbound).
- [ ] **Step 3: Commit** `feat: add portability HTTP routes`.

### Task 13: Composition Root — Provisioning and Portability Wiring (yellow)

**Files:**
- Create: `backend/cmd/api/provision.go`
- Modify: `backend/cmd/api/wiring.go`
- Modify: `backend/cmd/api/composition_integration_test.go` (extend)

**Interfaces:** `provision.go` provides `provisionInstanceIdentity(ctx, pool) error` (portability `MetaStore.InstanceID` get-or-create; structured log `instance identity established` only on first create) and `provisionPrivateAdmin(ctx, cfg, pool) error` (no-op unless `cfg.DeploymentMode == config.DeploymentModePrivate`; runs identity's `ProvisionAdminHandler` with `cfg.PrivateAdminPhone`; log `private admin provisioned`). Both run in `main.go` after the schema gate and before serving (modify `backend/cmd/api/main.go` accordingly). `wiring.go` gains `buildPortabilityHandlers(cfg, pool, now)` returning the four handlers plus shims:

```go
// shims adapt the modules' public handlers to portability's consumer-owned ports:
type todoExportShim struct{ export *todoquery.ExportTodosHandler }          // ExportTodos
type todoImportShim struct{ imp *todocommand.ImportTodoHandler }           // ImportTodo
type channelExportShim struct{ q *identityquery.ChannelsExportQuery }      // ExportChannels (principal mapping)
type channelImportShim struct{ imp *identitycommand.ImportChannelHandler } // ImportChannel
type channelProvisionShim … // ProvisionAdmin wiring stays direct in provision.go
type deliveryExportShim struct{ q *reminderquery.ExportDeliveriesHandler } // ExportDeliveries
type deliveryImportShim struct{ imp *remindercommand.ImportDeliveriesHandler } // ImportDelivery
```

`registerPortabilityRoutes(cfg, mux, auth, handlers)` registers the three import routes + export; the `ConfirmImportHandler` gets the `joinableUoW` (already in wiring.go) so a portability confirm joins exactly one transaction; `ArchiveFactory` is `archive.NewWriter`; `BundleParser` is the archive package `Parse`. Dev-gate routes unchanged.

- [ ] **Step 1 (red):** composition integration cases (extend the existing file's style — real pool from `TEST_DATABASE_URL`, real handlers): private mode — `provisionPrivateAdmin` creates workspace+user once, second call no-op; login request for the admin phone succeeds end-to-end through the real handler chain (challenge lands in the fake outbox), another phone returns `ErrRegistrationClosed`; instance identity provisioning returns a stable UUID across calls; portability round trip in cloud mode with two sessions (two phone numbers ⇒ two auto-provisioned workspaces) — workspace A creates one todo with a due date (which plans) + one channel, export A into a buffer through the real handlers, upload the bytes in workspace B, confirm, assert B contains the copied todo **with no reminder plan** (`reminder.reminder_plans` count for B is zero) and the channel unverified; confirm twice ⇒ `ErrImportConflict`. Run `go test ./backend/cmd/api -race` ⇒ FAIL.
- [ ] **Step 2 (green):** implement; cmd tests ⇒ PASS; `go vet ./backend/...`; `make architecture-test` ⇒ PASS (shims live only in cmd).
- [ ] **Step 3: Commit** `feat: wire portability and private admin provisioning`.

### Task 14: Contracts — OpenAPI and Export Schemas (yellow)

**Files:**
- Create: `contracts/openapi/portability.yaml`
- Create: `contracts/export-schemas/{manifest,todos,reminder-deliveries,preferences}.schema.json`
- Create: `tests/contract/portability_contract_test.go`
- Create: `tests/contract/export_bundle_contract_test.go`

**Interfaces:** `portability.yaml` documents routes 27–30 exactly as the route table above (OpenAPI 3.1.1, same style as `reminder.yaml`: operationId, `>-` descriptions, `$ref` components). Schemas: `ErrorEnvelope`, `ImportDecision`, `ImportPreview`, `ImportReport`, `ImportView`, `ExportAccepted` (none needed — export is binary; document `200` with `application/zip` content and no schema), `UploadRequest` (multipart documentation). `export-schemas/*.schema.json` pin the bundle JSON shapes with `schemaVersion: {"const": "1"}`, closed property sets, and required arrays mirroring the Go DTO tags from Task 8 (the Go wire shape is authoritative; the schemas document it).

- [ ] **Step 1 (red):** `portability_contract_test.go` follows `reminder_contract_test.go`: `portabilityRoutes()` table, `assertDocRoutes`, exact schema-set assertion, closed-object assertions per schema (required + optional property lists), string enums for `state` (`pending|committed|expired`) and `outcome` (`new|skipped|conflict|invalid`), integer non-negative counts, `truncated` boolean; a mutation test flipping one property fails the validator. `export_bundle_contract_test.go`: each schema file parses as JSON, pins `schemaVersion` const `"1"`, required sets match the Go DTOs (todo record: id/title/status/reminderVersion/createdAt/updatedAt; channel: id/kind/address/enabled; delivery: id/sourceTodoRecordId/channel/state/attemptCount/todoTitleSnapshot/scheduledAt/createdAt/origin; manifest: schemaVersion/sourceInstanceId/exportedAt/counts/files), enums for statuses/kinds/channels/states/origin. Run `go test ./tests/contract -race` ⇒ FAIL.
- [ ] **Step 2 (green):** write YAML + schemas; contract tests ⇒ PASS.
- [ ] **Step 3: Commit** `docs: add portability contract and export bundle schemas`.

### Task 15: Web — /data Page With Export and Import Flow (green)

**Files:**
- Create: `apps/web/src/features/data/{fetch-export.ts,import-flow.ts,data-panel.tsx}`
- Create: `apps/web/src/features/data/{fetch-export.test.ts,import-flow.test.ts,data-panel.test.tsx}`
- Create: `apps/web/src/app/(workbench)/data/page.tsx`
- Modify: `apps/web/src/app/(workbench)/workbench-shell.tsx` (add `{ href: "/data", label: "Data" }` to `workbenchLinks`)
- Modify: `apps/web/src/app/(workbench)/workbench-shell.test.tsx` (assert the new link)

**Interfaces:**

```ts
// fetch-export.ts
export async function exportBundle(baseURL: string, fetcher: typeof fetch, timeoutMs = 30000): Promise<Blob | null>
// POSTs /api/v1/portability/export, returns the zip blob; null on non-2xx/network error.

// import-flow.ts — validators follow the fetch-dashboard.ts fail-closed style
export interface ImportDecision { kind: string; sourceRecordId: string; outcome: "new" | "skipped" | "conflict" | "invalid"; reason?: string }
export interface ImportPreview { importId: string; new: number; skipped: number; conflicts: number; invalid: number; details: ImportDecision[]; truncated: boolean }
export interface ImportReport { new: number; skipped: number; conflicts: number; invalid: number; committedAt: string }
export type UploadResult = { ok: true; preview: ImportPreview } | { ok: false; code: string }
export type ConfirmResult = { ok: true; report: ImportReport } | { ok: false; code: string }
export async function uploadImportBundle(baseURL: string, fetcher: typeof fetch, file: File, timeoutMs?): Promise<UploadResult>
export async function confirmImport(baseURL: string, fetcher: typeof fetch, importId: string, timeoutMs?): Promise<ConfirmResult>
// uploadImportBundle POSTs multipart field "bundle"; on 422/409 parses the error envelope and surfaces its code.
```

`DataPanel` (`"use client"`, injectable `fetcher` prop like `DashboardPanel`) is a three-step state machine: idle → previewing (upload in flight) → preview (table of the four counts + capped details + truncated note + confirm button) → confirming → report; export button triggers `exportBundle` and downloads via a transient object URL. Error copy maps codes to actionable Chinese messages: `bundle_too_large` → “导出包超过上限，请拆分后重试”, `checksum_mismatch` → “导出包已损坏，请重新导出”, `unsupported_schema_version` → “导出包版本不受支持”, `bundle_invalid` → “导出包内容无效”, `import_conflict` → “该导入已确认或已过期”.

- [ ] **Step 1 (red):** vitest cases in the dashboard style: `exportBundle` returns a Blob for 200 (assert `response.blob()` passthrough), null for non-2xx and network errors, POSTs to the export path; `uploadImportBundle` sends `FormData` with field `bundle` (assert fetcher called with a FormData body), parses preview for 201, returns `{ok:false, code}` for 422 envelopes; `confirmImport` 200 ⇒ report, 409 ⇒ `{ok:false, code:"import_conflict"}`; validators fail closed on missing keys. `DataPanel` render tests (testing-library): initial buttons present; mocked upload drives preview counts rendering; confirm drives the report; each error code renders its mapped message; truncated preview shows the note. Run `corepack pnpm --filter @artificial-brain/web test` ⇒ FAIL.
- [ ] **Step 2 (green):** implement (no new dependencies; `FormData`, `URL.createObjectURL` are platform APIs); web tests ⇒ PASS; `corepack pnpm --filter @artificial-brain/web lint` ⇒ PASS.
- [ ] **Step 3: Commit** `feat: add data portability page with export and import flow`.

### Task 16: Private Deployment Assets, Backup/Restore, Offline Bundle (yellow)

**Files:**
- Create: `deploy/private/{README.md,env.template,backup.sh,restore.sh}`
- Create: `docs/runbooks/{backup-restore.md,upgrade.md}`
- Modify: `Makefile` (add `backup`, `restore`, `offline-bundle` targets; extend `.PHONY`)
- Modify: `compose.yaml` (pass `DEPLOYMENT_MODE` and `PRIVATE_ADMIN_PHONE` into api and worker; api also gets `PORTABILITY_MAX_BUNDLE_BYTES`)
- Modify: `.env.example` (append `DEPLOYMENT_MODE=cloud`)
- Modify: `tests/smoke/stack_test.sh` `static_test` (extend `expected_env` with `DEPLOYMENT_MODE=cloud`; assert the new compose passthrough env keys)

**Interfaces:** `backup.sh` / `restore.sh` are POSIX sh, env-parameterized so smoke can point them at the smoke project: `COMPOSE_PROJECT_NAME` (required), `POSTGRES_USER`/`POSTGRES_DB` (default `artificial_brain`), `OUTPUT_DIR` (default `deploy/private/backups`, gitignored). `backup.sh`: `docker compose --project-name "$COMPOSE_PROJECT_NAME" exec -T postgres pg_dump --username … --format=custom --dbname …` → `${OUTPUT_DIR}/backup-<UTC timestamp>.dump` + `.sha256`; prints the archive path. `restore.sh`: requires `BACKUP` (path) and `CONFIRM=restore`; verifies the file exists and the sha256 sidecar matches when present; stops `api worker web` via compose, `pg_restore --clean --if-exists` into the db, restarts the stopped services. Make targets wrap the scripts for the default project name (`COMPOSE_PROJECT_NAME` defaults to the compose default) — `make restore` additionally requires `BACKUP` and `CONFIRM=restore` and refuses otherwise (same guard style as `clean-local-data`). `offline-bundle`: `docker compose --profile test images`-driven `docker save` of `postgres:18.4-alpine` + the five built service images into `.artifacts/offline/artificial-brain-images.tar` with a `README` load recipe (artifacts never committed).

`deploy/private/README.md`: single-host quick start (copy `env.template` to `.env`, set `DEPLOYMENT_MODE=private` + `PRIVATE_ADMIN_PHONE` + real SMTP/SMS/model config, `docker compose up -d --build`), **no reverse proxy in the box** with the LAN exposure risk note and the enterprise-proxy hookup (forward to web:3000 only; health paths; the web `/api` rewrite means one origin suffices), backup/restore and upgrade pointers. `upgrade.md`: checklist (verify version → `make backup` → replace images/refs → `docker compose up -d --build` letting the one-shot migrate run → verify health + spot-check data), append-only migrations reminder, restore path on failure.

- [ ] **Step 1 (red):** `make harness-test` first if the Make targets reference scripts (repository policy test scans Make/script conventions); shellcheck-style dry runs: `sh -n deploy/private/backup.sh deploy/private/restore.sh`; run `restore.sh` without `CONFIRM` ⇒ exits non-zero with the guard message; run `backup.sh` with a fake `docker` shim on PATH that records arguments (temp dir) ⇒ produces a dump file named by timestamp and a `.sha256`; smoke `static_test` currently FAILS on the extended `expected_env` until `.env.example` and compose.yaml change — make those edits in this task and re-run `sh tests/smoke/stack_test.sh --static-only` ⇒ PASS.
- [ ] **Step 2 (green):** implement all assets; `make harness-test` ⇒ PASS; `--static-only` ⇒ PASS; `docker compose config --quiet` ⇒ PASS.
- [ ] **Step 3: Commit** `feat: add private deployment assets, backup/restore, and offline bundle targets`.

### Task 17: Smoke Gates — Portability, Private Mode, Backup/Restore, Upgrade Drill (yellow)

**Files:**
- Modify: `tests/smoke/stack_test.sh` (new blocks inside `full_stack_test`)
- Modify: `README.md` (portability routes, new env vars, backup/restore/offline-bundle commands, private deployment pointer)

**Interfaces:** four appended blocks reusing `e2e_auth`, `WEB_PORT`, and the `verify_contact_channel` helper:

- [ ] **Step 1: Portability block.** After the ITER-0003 reminder block. Semantics note (assumption A3): locally created rows carry no source identity, so a **self-import creates copies** on the first run and is idempotent from the second run on. Steps: (a) `POST /api/v1/portability/export` with the session ⇒ 200 zip saved to a temp file; `unzip -l` lists the five entries; `unzip -p … manifest.json | jq` asserts `schemaVersion == "1"` and `counts.todos >= 1`; (b) upload the same bundle ⇒ 201, preview reports every record `new` (no source records exist yet); (c) confirm ⇒ 200 report, `new` equals the preview counts; assert via API/psql: todo list count increased by the copied todos, **`reminder.reminder_plans` count unchanged** (imports never plan), the copied channels appear unverified in the settings listing, and `/api/v1/reminders` shows the imported history rows (`state` preserved); (d) re-upload + re-confirm the same bundle ⇒ every record `skipped`, todo count unchanged (acceptance scenario 10 idempotency); (e) confirm the first importId again ⇒ 409 `import_conflict`; (f) upload a non-zip file (`printf 'not a bundle'`) ⇒ 422 `bundle_invalid` (checksum-mismatch and record-invalid rejections are covered by Task 11/12 unit tests); (g) oversized upload (the bundle padded past `PORTABILITY_MAX_BUNDLE_BYTES` with `dd`) ⇒ 422 `bundle_too_large`.
- [ ] **Step 2: Private-mode block.** Second compose project `${project}-private` run with `docker compose --project-name ${project}-private up --build --detach --wait` and env `DEPLOYMENT_MODE=private`, `PRIVATE_ADMIN_PHONE=+8613800137999`, `APP_ENV=development`, `DEV_INBOX_ENABLED=true` (fake adapters, so the red line "CI never calls real providers" holds — assumption A7), `API_PORT=0`, `WEB_PORT=0`: admin phone login through the dev inbox succeeds (202 → code → verify sets the cookie, home renders the dashboard); a second phone `+8613800137998` login request returns 403 with body code `registration_closed`; `docker compose --project-name ${project}-private down --volumes --remove-orphans` in the same block.
- [ ] **Step 3: Backup/restore block.** With the main project still up: `COMPOSE_PROJECT_NAME=$project OUTPUT_DIR=<tmp> sh deploy/private/backup.sh` produces a dump; delete the portability-imported copy todo through the API (confirmation flow) or directly via psql to mutate state; `COMPOSE_PROJECT_NAME=$project BACKUP=<dump> CONFIRM=restore sh deploy/private/restore.sh`; wait healthy; assert the deleted row is back (psql count) and the dashboard still answers; `CONFIRM=wrong` run exits non-zero without touching the database.
- [ ] **Step 4: Upgrade drill block.** Record todo + delivery counts via psql; `compose up --build --detach --wait` again (recreate; the one-shot migrate re-runs idempotently at version 8); wait healthy; counts unchanged; `assert_page healthy /status`.
- [ ] **Step 5: README.** Add the four portability routes to the business route table, the three new env vars to the table, `make backup` / `make restore` / `make offline-bundle` under verification/local ops, and a "Private deployment" pointer to `deploy/private/README.md`.
- [ ] **Step 6: Commit** `ci: extend smoke with portability, private mode, backup/restore, and upgrade drill`.

### Task 18: Unified Verification, Docs, Handoff, and Clean-Context Regression Gate

**Files:**
- Modify: `docs/iterations/ITER-0004/{progress,decisions,test-matrix,handoff}.md`
- Read-only gates: everything above

**Interfaces:** the iteration's closing evidence, mirroring ITER-0003 Task 16.

- [ ] **Step 1:** full acceptance sequence exactly as CI: `corepack pnpm install --frozen-lockfile`, `make verify`, `make migration-test`, `make smoke-test` — all green on the branch tip; record the commit hashes in `progress.md`.
- [ ] **Step 2:** finalize the ledger: `progress.md` all tasks Done with evidence commits; `test-matrix.md` rows green with evidence; `decisions.md` outcomes section — record any adaptations as O-items and accepted assumptions (A1 config-local phone regex; A2 delivery export includes `origin`; A3 self-import copies records because locally created rows carry no source identity; A4 409 `import_conflict` covers both committed and expired; A5 offline-bundle artifacts live in gitignored `.artifacts/`); `handoff.md` rewritten for ITER-0004 (what/where/how-to-continue/environment prerequisites, same shape as ITER-0003's).
- [ ] **Step 3:** `git status --short` clean except intended files; commit `docs: finalize ITER-0004 ledger` (marker file + AI trailers per repo convention).
- [ ] **Step 4:** dispatch the independent clean-context regression agent (per master design §11.4): only the approved spec + brief/plan/test-matrix/handoff + merge-base…HEAD diff + README/Makefile commands; it re-runs the gates, maps acceptance criteria to evidence, checks yellow/red compliance (zero new dependencies; migrations 001–007 untouched; no real-provider egress in CI; no credentials), and writes the immutable `regression-report.md`. On FAIL: fix as fresh red→green `fix:` commits, then a NEW regression agent supersedes.
- [ ] **Step 5:** record the regression approval in the ledger (`docs: record ITER-0004 regression approval`) and open the merge request.

## Yellow-zone register (must be handled deliberately; all listed in this plan)

| Item | Tasks |
|---|---|
| Migration 008 + `CurrentSchemaVersion` 7→8 + smoke pin | 2 |
| Platform config fields | 3 |
| `backend/cmd/api` wiring + `main.go` provisioning + `cmd` shims | 13 |
| `contracts/openapi/portability.yaml` + `contracts/export-schemas/**` + contract tests | 14 |
| Architecture testdata fixture (policy unchanged) | 1 |
| `Makefile` backup/restore/offline-bundle targets | 16 |
| `compose.yaml` env passthrough + `.env.example` + smoke static env | 16 |
| `tests/smoke/stack_test.sh` blocks + `migration_test.sh` pin | 2, 17 |
| `deploy/private/**` + `docs/runbooks/**` | 16 |
| `README.md` + root/backend/apps/web/deploy `AGENTS.md` | 1, 17 |

## Assumptions register (record accepted ones in `decisions.md`)

- **A1** `config` re-implements the E.164 regex locally (platform must not import modules).
- **A2** Delivery export records carry `origin`; the plan-time delivery path is otherwise untouched.
- **A3** Self-import (bundle re-imported into its source instance) creates copies because locally created rows carry no source identity; dedupe still holds from the second import on.
- **A4** 409 `import_conflict` distinguishes committed vs expired by message, keeping the stable-code surface at six new codes.
- **A5** Offline-bundle artifacts live under gitignored `.artifacts/offline/`; the target builds them on demand.
- **A6** `reminder_deliveries.plan_id` becomes nullable (FK kept); imported history never joins a plan.
- **A7** Private-mode smoke runs `APP_ENV=development` (fake adapters + dev inbox); production gating is covered by config unit tests, mirroring ITER-0003's real-adapter strategy.
- **A8** Preview/confirm detail lists cap at 100 per outcome with `truncated`.
- **A9** Import TTL is 24 h, evaluated lazily at GET/confirm.
- **A10** The upgrade drill is same-architecture (recreate stack, migrate idempotent) — cross-version drills await the first release.

## Plan Completion Evidence

All 18 tasks Done in `progress.md`; unified sequence green at the branch tip; zero new dependencies in `go.mod`/`package.json` diffs; independent clean-context regression PASS recorded in `docs/iterations/ITER-0004/regression-report.md`.
