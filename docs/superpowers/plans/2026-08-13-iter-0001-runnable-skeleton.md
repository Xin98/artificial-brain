# ITER-0001 Runnable Skeleton Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build a one-command, vertically runnable Next.js + Go API + Go Worker + PostgreSQL skeleton with independent migrations, health aggregation, architecture enforcement, CI, and reproducible iteration evidence.

**Architecture:** A Next.js server-rendered status page calls one Go system-health endpoint. The Go API checks PostgreSQL and reads a database-backed Worker heartbeat lease; a separate Go Worker maintains that lease and exposes internal liveness/readiness probes. A one-shot migrate binary owns all schema changes, while Make and Docker Compose provide the same deterministic entry points used by CI.

**Tech Stack:** Go 1.26.5, pgx 5.9.2, tern 2.4.1, Node.js 24.18.0 LTS, pnpm 11.19.0, Next.js 16.2.12, React 19.2.8, TypeScript 6.0.3, Vitest 4.1.10, PostgreSQL 18.4, Docker Compose, GitHub Actions.

## Global Constraints

- The approved design is `docs/superpowers/specs/2026-08-13-iter-0001-runnable-skeleton-design.md`; implementation must not add Identity, Todo, Conversation, Reminder, or Portability behavior.
- API routes are exactly `GET /health/live`, `GET /health/ready`, and `GET /api/v1/system/health`.
- Worker routes are exactly `GET /health/live` and `GET /health/ready` on an internal-only port.
- API and Worker may check schema compatibility but must never run migrations; only `backend/cmd/migrate` may change schema.
- Environment defaults are: API `:8080`, Worker health `:8081`, shutdown timeout `10s`, heartbeat interval `2s`, Worker lease TTL `6s`, migrations directory `/migrations`, service version `dev`.
- `WORKER_LEASE_TTL` must be at least twice `WORKER_HEARTBEAT_INTERVAL`; configuration errors name keys but never echo values.
- Status values are exactly `healthy`, `degraded`, and `unavailable`; timestamps are UTC RFC3339.
- Correlation IDs use header `X-Correlation-ID`, accept only `[A-Za-z0-9._-]{1,128}`, and are generated when absent or invalid.
- Logs are JSON and include `time`, `level`, `service`, `version`, `msg`, and `correlation_id` when available; credentials and connection strings are never logged.
- Go domain/application dependency rules from the approved design are enforced even though ITER-0001 does not create empty business packages.
- Tests use injected clocks/tickers and bounded polling; no test uses a fixed long sleep.
- `make down` preserves volumes. The destructive data-cleaning target must be named `clean-local-data` and require `CONFIRM=delete`.
- Existing untracked `.DS_Store` files belong to the user and must not be staged, modified, or deleted.
- Every task follows red → green → refactor, updates `docs/iterations/ITER-0001/progress.md`, and commits only its listed files.

## File and Module Map

| Module | Interface | Implementation files |
|---|---|---|
| Configuration | `config.Load(role, lookup) (Config, error)` | `backend/internal/platform/config/config.go` |
| Observability | `observability.NewLogger`, context Correlation ID helpers | `backend/internal/platform/observability/*.go` |
| Database | `database.OpenPool`, `database.RequireSchema`, `database.RunMigrations` | `backend/internal/platform/database/*.go` |
| Worker status | `workerstatus.Registry.Record/Remove/Latest`, `workerstatus.Heartbeat.Run` | `backend/internal/platform/workerstatus/*.go` |
| System health | `systemhealth.Checker.Check(ctx) Report` | `backend/internal/platform/systemhealth/*.go` |
| HTTP delivery | `server.NewAPIHandler`, `server.NewWorkerHealthHandler`, `server.Serve` | `backend/internal/platform/server/*.go` |
| Web system health | `fetchSystemHealth(baseURL, fetcher)`, `SystemHealthView` | `apps/web/src/features/system-health/*` |
| Architecture policy | `architecture.Validate(root) ([]Violation, error)` | `architecture/policy/*.go` |

The public interfaces above are the test surfaces. pgx queries, clock functions, tickers, JSON encoding, and HTTP details remain behind those interfaces or at internal test seams.

---

### Task 1: Repository Policy, Toolchain Pins, and Iteration Ledger

**Files:**
- Create: `.gitignore`
- Create: `.node-version`
- Create: `go.mod`
- Create: `package.json`
- Create: `pnpm-workspace.yaml`
- Create: `Makefile`
- Create: `scripts/check-toolchain.sh`
- Create: `tests/harness/repository_policy_test.sh`
- Create: `tests/harness/fixtures/go`
- Create: `tests/harness/fixtures/node`
- Create: `tests/harness/fixtures/pnpm`
- Create: `AGENTS.md`
- Create: `apps/web/AGENTS.md`
- Create: `backend/AGENTS.md`
- Create: `architecture/AGENTS.md`
- Create: `deploy/AGENTS.md`
- Create: `docs/iterations/ITER-0001/brief.md`
- Create: `docs/iterations/ITER-0001/spec.md`
- Create: `docs/iterations/ITER-0001/plan.md`
- Create: `docs/iterations/ITER-0001/progress.md`
- Create: `docs/iterations/ITER-0001/decisions.md`
- Create: `docs/iterations/ITER-0001/test-matrix.md`
- Create: `docs/iterations/ITER-0001/handoff.md`

**Interfaces:**
- Consumes: approved design and this plan.
- Produces: `make toolchain-check`, `make harness-test`, pinned language/package-manager metadata, and the iteration ledger every later task updates.

- [ ] **Step 1: Write the failing repository policy test**

Create `tests/harness/repository_policy_test.sh` with assertions that fail because the files do not exist yet:

```sh
#!/bin/sh
set -eu

fixture_dir=$(mktemp -d)
trap 'rm -rf "$fixture_dir"' EXIT

mkdir -p "$fixture_dir/bin"
for tool in go node pnpm; do
  cp "tests/harness/fixtures/$tool" "$fixture_dir/bin/$tool"
  chmod +x "$fixture_dir/bin/$tool"
done

PATH="$fixture_dir/bin:$PATH" sh scripts/check-toolchain.sh

TOOLCHAIN_FAKE_GO_VERSION=go1.25.0 \
  PATH="$fixture_dir/bin:$PATH" \
  sh -c 'if sh scripts/check-toolchain.sh; then exit 1; fi'

node -e '
  const fs = require("node:fs");
  const pkg = JSON.parse(fs.readFileSync("package.json", "utf8"));
  if (pkg.packageManager !== "pnpm@11.19.0") process.exit(1);
  if (pkg.engines.node !== "24.18.0") process.exit(1);
'

corepack pnpm --silent list --depth=-1 >/dev/null

if git ls-files | grep -E '(^|/)\.DS_Store$|(^|/)\.env$'; then
  echo 'tracked local or secret file detected' >&2
  exit 1
fi
```

- [ ] **Step 2: Run the policy test and verify red**

Run: `sh tests/harness/repository_policy_test.sh`

Expected: FAIL at `.node-version` or another missing required file.

- [ ] **Step 3: Add exact toolchain and repository policy files**

Use these exact version declarations:

```text
# .node-version
24.18.0
```

```go
module github.com/Xin98/artificial-brain

go 1.26.0

toolchain go1.26.5
```

```json
{
  "name": "artificial-brain",
  "private": true,
  "packageManager": "pnpm@11.19.0",
  "engines": { "node": "24.18.0", "pnpm": "11.19.0" },
  "scripts": {
    "format": "prettier --write package.json pnpm-workspace.yaml compose.yaml '.github/**/*.yml' && pnpm --filter @artificial-brain/web format",
    "format:check": "prettier --check package.json pnpm-workspace.yaml compose.yaml '.github/**/*.yml' && pnpm --filter @artificial-brain/web format:check",
    "lint": "pnpm --filter @artificial-brain/web lint",
    "test": "pnpm --filter @artificial-brain/web test",
    "build": "pnpm --filter @artificial-brain/web build"
  },
  "devDependencies": {
    "prettier": "3.9.6"
  }
}
```

```yaml
packages:
  - apps/*
```

`.gitignore` must cover `.DS_Store`, `.env`, `.env.*` with `!.env.example`, `.next/`, `node_modules/`, coverage, build output, and local Compose overrides. `AGENTS.md` files must restate the green/yellow/red modification zones, local dependency direction, and the exact Make verification targets relevant to that directory. Do not add empty business module directories.

Create the first `Makefile` targets without shell-specific GNU extensions:

```make
.PHONY: toolchain-check harness-test

toolchain-check:
	@sh scripts/check-toolchain.sh

harness-test:
	@sh tests/harness/repository_policy_test.sh
```

Add executable fixtures `tests/harness/fixtures/go`, `tests/harness/fixtures/node`, and `tests/harness/fixtures/pnpm`. Each prints the real command's version shape and reads only its matching `TOOLCHAIN_FAKE_*_VERSION` variable, defaulting to the approved version. `scripts/check-toolchain.sh` compares those observable command outputs to the pinned policy, accepts patch-newer Go 1.26 and exact Node/pnpm versions, and prints actionable install messages without installing anything. The test must prove the valid fixture set succeeds and an older Go fixture fails; it must not assert configuration by grepping source text.

Iteration docs must copy the approved scope and ten acceptance criteria, link the design and this plan, mark Task 1 in progress, and state that `regression-report.md` is intentionally absent until clean-context regression.

- [ ] **Step 4: Run policy checks and verify green**

Run: `make harness-test`

Expected: PASS with exit code 0.

Run: `make toolchain-check`

Expected in an equipped execution environment: PASS and print the pinned versions. If a local tool is absent, record the exact missing prerequisite in `progress.md`; do not weaken the check.

- [ ] **Step 5: Commit the baseline**

```bash
git add .gitignore .node-version go.mod package.json pnpm-workspace.yaml Makefile scripts/check-toolchain.sh tests/harness/repository_policy_test.sh tests/harness/fixtures AGENTS.md apps/web/AGENTS.md backend/AGENTS.md architecture/AGENTS.md deploy/AGENTS.md docs/iterations/ITER-0001
git commit -m "chore: establish ITER-0001 harness"
```

### Task 2: Typed Configuration and Structured Observability

**Files:**
- Create: `backend/internal/platform/config/config.go`
- Create: `backend/internal/platform/config/config_test.go`
- Create: `backend/internal/platform/observability/context.go`
- Create: `backend/internal/platform/observability/context_test.go`
- Create: `backend/internal/platform/observability/logger.go`
- Create: `backend/internal/platform/observability/logger_test.go`
- Modify: `docs/iterations/ITER-0001/progress.md`
- Modify: `docs/iterations/ITER-0001/test-matrix.md`

**Interfaces:**
- Consumes: Go module from Task 1.
- Produces:
  - `type config.Role string` with `RoleAPI`, `RoleWorker`, `RoleMigrate`.
  - `type config.LookupEnv func(string) (string, bool)`.
  - `func config.Load(role Role, lookup LookupEnv) (Config, error)`.
  - `func observability.NewLogger(w io.Writer, service, version string) *slog.Logger`.
  - `func observability.WithCorrelationID(ctx context.Context, id string) context.Context` and `CorrelationID(ctx) string`.

- [ ] **Step 1: Write failing configuration tests**

Create table-driven tests for valid API, Worker, and migrate roles plus missing database URL, invalid durations, and `leaseTTL < 2 * heartbeatInterval`:

```go
func TestLoadWorkerConfig(t *testing.T) {
	env := map[string]string{
		"DATABASE_URL": "postgres://user:secret@db/workbench",
		"SERVICE_VERSION": "abc123",
		"WORKER_HEARTBEAT_INTERVAL": "3s",
		"WORKER_LEASE_TTL": "9s",
	}
	cfg, err := Load(RoleWorker, mapLookup(env))
	if err != nil { t.Fatal(err) }
	if cfg.HTTPAddress != ":8081" { t.Fatalf("address = %q", cfg.HTTPAddress) }
	if cfg.HeartbeatInterval != 3*time.Second { t.Fatalf("interval = %s", cfg.HeartbeatInterval) }
	if cfg.WorkerLeaseTTL != 9*time.Second { t.Fatalf("ttl = %s", cfg.WorkerLeaseTTL) }
}

func TestLoadNeverEchoesSecretValue(t *testing.T) {
	_, err := Load(RoleAPI, mapLookup(map[string]string{
		"DATABASE_URL": "postgres://user:TOP-SECRET@db/workbench",
		"SHUTDOWN_TIMEOUT": "not-a-duration",
	}))
	if err == nil { t.Fatal("expected error") }
	if strings.Contains(err.Error(), "TOP-SECRET") { t.Fatalf("secret leaked: %v", err) }
}
```

- [ ] **Step 2: Run configuration tests and verify red**

Run: `go test ./backend/internal/platform/config -run TestLoad -v`

Expected: FAIL because `Load`, roles, and `Config` do not exist.

- [ ] **Step 3: Implement the minimal deep configuration module**

Define exactly:

```go
type Config struct {
	Role              Role
	ServiceName       string
	ServiceVersion    string
	DatabaseURL       string
	HTTPAddress       string
	MigrationsDir     string
	ShutdownTimeout   time.Duration
	HeartbeatInterval time.Duration
	WorkerLeaseTTL    time.Duration
}
```

`Load` chooses role-specific addresses/names, parses durations once, applies the Global Constraints defaults, rejects empty `DATABASE_URL`, rejects unknown roles, and returns key-oriented errors such as `config: invalid WORKER_LEASE_TTL`. Do not expose a generic `GetString`/`GetDuration` bag to callers.

- [ ] **Step 4: Write failing observability tests**

Tests must prove context round-trip and required JSON fields:

```go
func TestLoggerIncludesServiceFieldsAndCorrelationID(t *testing.T) {
	var out bytes.Buffer
	logger := NewLogger(&out, "api", "abc123")
	ctx := WithCorrelationID(context.Background(), "req-123")
	logger.InfoContext(ctx, "started")

	var got map[string]any
	if err := json.Unmarshal(out.Bytes(), &got); err != nil { t.Fatal(err) }
	for key, want := range map[string]string{
		"service": "api", "version": "abc123", "msg": "started", "correlation_id": "req-123",
	} {
		if got[key] != want { t.Fatalf("%s = %#v", key, got[key]) }
	}
}
```

- [ ] **Step 5: Implement observability helpers and verify green**

Use `log/slog` JSON handler. Add a handler wrapper that reads Correlation ID from context and appends it only when non-empty. Do not add OpenTelemetry SDK dependencies in ITER-0001; this context seam is the later trace bridge.

Run: `go test ./backend/internal/platform/config ./backend/internal/platform/observability -v`

Expected: PASS.

- [ ] **Step 6: Update iteration evidence and commit**

Add CFG-01 through CFG-06 and OBS-01 through OBS-03 to `test-matrix.md`, record the exact passing command in `progress.md`, then:

```bash
git add backend/internal/platform/config backend/internal/platform/observability docs/iterations/ITER-0001/progress.md docs/iterations/ITER-0001/test-matrix.md
git commit -m "feat: add runtime configuration and logging"
```

### Task 3: PostgreSQL Connection, One-Shot Migration, and Schema Compatibility

**Files:**
- Create: `backend/internal/platform/database/pool.go`
- Create: `backend/internal/platform/database/schema.go`
- Create: `backend/internal/platform/database/migrate.go`
- Create: `backend/internal/platform/database/schema_test.go`
- Create: `backend/internal/platform/database/migrate_integration_test.go`
- Create: `backend/cmd/migrate/main.go`
- Create: `deploy/migrations/001_create_runtime_health.sql`
- Modify: `go.mod`
- Create: `go.sum`
- Modify: `docs/iterations/ITER-0001/progress.md`
- Modify: `docs/iterations/ITER-0001/test-matrix.md`

**Interfaces:**
- Consumes: `config.Load(RoleMigrate, os.LookupEnv)` and `observability.NewLogger` from Task 2.
- Produces:
  - `func database.OpenPool(ctx context.Context, url string) (*pgxpool.Pool, error)`.
  - `func database.RequireSchema(ctx context.Context, q Queryer, expected int32) error`.
  - `func database.RunMigrations(ctx context.Context, url, directory string) error`.
  - Schema version constant `database.CurrentSchemaVersion int32 = 1`.

- [ ] **Step 1: Write failing schema compatibility tests**

Use a small fake implementing `QueryRow` and assert compatible, absent, and mismatched versions:

```go
func TestRequireSchemaRejectsMismatch(t *testing.T) {
	q := fakeQueryer{version: 0}
	err := RequireSchema(context.Background(), q, 1)
	if !errors.Is(err, ErrSchemaIncompatible) { t.Fatalf("error = %v", err) }
	if q.execCalled { t.Fatal("schema check attempted a write") }
}
```

The consumer-owned `Queryer` interface is exactly:

```go
type Queryer interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}
```

- [ ] **Step 2: Run schema tests and verify red**

Run: `go test ./backend/internal/platform/database -run TestRequireSchema -v`

Expected: FAIL because the database package is missing.

- [ ] **Step 3: Implement connection and read-only compatibility checks**

`OpenPool` parses pgx pool config, sets a five-second connection timeout if not supplied, opens the pool, pings once, and closes on ping failure. `RequireSchema` executes only:

```sql
select version from public.schema_version limit 1
```

It wraps missing-table and version mismatch as `ErrSchemaIncompatible` without exposing the connection URL.

- [ ] **Step 4: Add the first migration and failing integration test**

Use tern format in `001_create_runtime_health.sql`:

```sql
create schema if not exists runtime;

create table runtime.worker_heartbeats (
  instance_id text primary key,
  service_version text not null,
  started_at timestamptz not null,
  last_heartbeat_at timestamptz not null
);

---- create above / drop below ----

drop table if exists runtime.worker_heartbeats;
drop schema if exists runtime;
```

The integration test reads `TEST_DATABASE_URL`, skips only when absent, calls `RunMigrations` twice, asserts `CurrentSchemaVersion`, and verifies the Worker table exists exactly once.

- [ ] **Step 5: Implement the migration runner and command**

Pin dependencies:

```text
github.com/jackc/pgx/v5 v5.9.2
github.com/jackc/tern/v2 v2.4.1
```

`RunMigrations` connects with `pgx.Connect`, constructs `migrate.NewMigrator(ctx, conn, "public.schema_version")`, calls `LoadMigrations(directory)`, and calls `Migrate(ctx)`. `cmd/migrate` loads config, logs only migration directory/version, exits non-zero on failure, and never prints `DATABASE_URL`.

- [ ] **Step 6: Run database tests**

Run: `go test ./backend/internal/platform/database -run TestRequireSchema -v`

Expected: PASS.

Run with PostgreSQL available: `TEST_DATABASE_URL=postgres://... go test ./backend/internal/platform/database -run TestRunMigrationsTwice -v`

Expected: PASS; second run reports no pending migration and makes no schema change.

- [ ] **Step 7: Update evidence and commit**

Record DB-01 through DB-05 and MIG-01 through MIG-04 in the test matrix. Commit:

```bash
git add backend/internal/platform/database backend/cmd/migrate deploy/migrations go.mod go.sum docs/iterations/ITER-0001/progress.md docs/iterations/ITER-0001/test-matrix.md
git commit -m "feat: add independent database migrations"
```

### Task 4: Worker Lease Registry and Deterministic Heartbeat Loop

**Files:**
- Create: `backend/internal/platform/workerstatus/registry.go`
- Create: `backend/internal/platform/workerstatus/registry_integration_test.go`
- Create: `backend/internal/platform/workerstatus/heartbeat.go`
- Create: `backend/internal/platform/workerstatus/heartbeat_test.go`
- Modify: `docs/iterations/ITER-0001/progress.md`
- Modify: `docs/iterations/ITER-0001/test-matrix.md`

**Interfaces:**
- Consumes: `runtime.worker_heartbeats` from Task 3 and `*pgxpool.Pool`.
- Produces:
  - `type workerstatus.Instance { ID, Version string; StartedAt time.Time }`.
  - `type workerstatus.Lease { InstanceID, Version string; StartedAt, LastHeartbeatAt time.Time }`.
  - `func workerstatus.NewRegistry(pool *pgxpool.Pool, now func() time.Time) *Registry`.
  - `Registry.Record(ctx, Instance) error`, `Registry.Remove(ctx, id) error`, `Registry.Latest(ctx) (Lease, error)`.
  - `func workerstatus.NewHeartbeat(recorder Recorder, instance Instance, ticks TickSource) *Heartbeat` and `Heartbeat.Run(ctx) error`.
  - `type workerstatus.Recorder interface { Record(context.Context, Instance) error; Remove(context.Context, string) error }`.
  - `type workerstatus.TickSource interface { C() <-chan time.Time; Stop() }`.
  - `func workerstatus.NewTimeTickSource(interval time.Duration) (TickSource, error)` for the production adapter.

- [ ] **Step 1: Write failing registry integration tests**

The tests migrate a disposable database, inject a fixed UTC clock, record two instances, and assert `Latest` chooses the greatest heartbeat timestamp. They also assert upsert preserves `started_at`, updates `service_version`, and `Remove` makes `Latest` return `ErrNoLease`.

```go
func TestRegistryRecordAndLatest(t *testing.T) {
	now := time.Date(2026, 8, 13, 3, 0, 0, 0, time.UTC)
	r := NewRegistry(pool, func() time.Time { return now })
	if err := r.Record(ctx, Instance{ID: "worker-1", Version: "abc", StartedAt: now.Add(-time.Minute)}); err != nil { t.Fatal(err) }
	got, err := r.Latest(ctx)
	if err != nil { t.Fatal(err) }
	if got.InstanceID != "worker-1" || !got.LastHeartbeatAt.Equal(now) { t.Fatalf("lease = %#v", got) }
}
```

- [ ] **Step 2: Implement the registry behind its small interface**

Use parameterized SQL only. `Record` is one `insert ... on conflict (instance_id) do update` statement; `Latest` orders by `last_heartbeat_at desc, instance_id asc limit 1`; `Remove` deletes only the exact instance ID. Convert `pgx.ErrNoRows` to `ErrNoLease`.

Run with PostgreSQL: `TEST_DATABASE_URL=postgres://... go test ./backend/internal/platform/workerstatus -run TestRegistry -v`

Expected: PASS.

- [ ] **Step 3: Write the failing heartbeat lifecycle tests**

Use a channel-backed fake tick source; do not use `time.Sleep`:

```go
func TestHeartbeatRecordsImmediatelyOnEachTickAndRemovesOnCancel(t *testing.T) {
	recorder := &fakeRecorder{}
	ticks := newFakeTicks()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- NewHeartbeat(recorder, instance, ticks).Run(ctx) }()

	recorder.waitForRecords(t, 1)
	ticks.Send(time.Now())
	recorder.waitForRecords(t, 2)
	cancel()
	if err := <-done; err != nil { t.Fatal(err) }
	if recorder.removedID != instance.ID { t.Fatalf("removed = %q", recorder.removedID) }
}
```

- [ ] **Step 4: Implement Heartbeat and verify deterministic lifecycle**

`Run` records immediately, then on every tick. On context cancellation it attempts `Remove` with a fresh cleanup context capped at two seconds. A record failure returns immediately; a cleanup failure is logged by the caller but does not replace an earlier run failure. `NewTimeTickSource` rejects non-positive intervals before wrapping `time.NewTicker`, and `Heartbeat.Run` always stops its TickSource.

Run: `go test ./backend/internal/platform/workerstatus -run TestHeartbeat -race -v`

Expected: PASS without real-time waits.

- [ ] **Step 5: Update evidence and commit**

```bash
git add backend/internal/platform/workerstatus docs/iterations/ITER-0001/progress.md docs/iterations/ITER-0001/test-matrix.md
git commit -m "feat: track worker heartbeat leases"
```

### Task 5: System Health Module and Versioned HTTP Contract

**Files:**
- Create: `backend/internal/platform/systemhealth/report.go`
- Create: `backend/internal/platform/systemhealth/checker.go`
- Create: `backend/internal/platform/systemhealth/checker_test.go`
- Create: `contracts/openapi/system-health.yaml`
- Create: `tests/contract/system_health_contract_test.go`
- Modify: `go.mod`
- Modify: `go.sum`
- Modify: `docs/iterations/ITER-0001/progress.md`
- Modify: `docs/iterations/ITER-0001/test-matrix.md`

**Interfaces:**
- Consumes: `workerstatus.Lease` and `workerstatus.ErrNoLease` from Task 4.
- Produces:
  - `type systemhealth.Status string` with exact constants `StatusHealthy`, `StatusDegraded`, `StatusUnavailable`.
  - `type systemhealth.Component { Status Status; CheckedAt time.Time; Detail string }`.
  - `type systemhealth.Report { Status Status; CheckedAt time.Time; CorrelationID string; Components map[string]Component }`.
  - `type systemhealth.DatabaseProbe interface { Ping(context.Context) error }`.
  - `type systemhealth.WorkerLeaseReader interface { Latest(context.Context) (workerstatus.Lease, error) }`.
  - `func systemhealth.NewChecker(db DatabaseProbe, workers WorkerLeaseReader, now func() time.Time, leaseTTL time.Duration) *Checker`.
  - `func (*Checker) Check(context.Context) Report`.

- [ ] **Step 1: Write failing status-mapping tests**

Cover all healthy, database failure, missing Worker, lease exactly at TTL, lease one nanosecond beyond TTL, and raw-error redaction:

```go
func TestCheckerMarksExpiredWorkerUnavailable(t *testing.T) {
	now := time.Date(2026, 8, 13, 4, 0, 0, 0, time.UTC)
	checker := NewChecker(
		fakeDB{},
		fakeWorkers{lease: workerstatus.Lease{LastHeartbeatAt: now.Add(-6*time.Second - time.Nanosecond)}},
		func() time.Time { return now },
		6*time.Second,
	)
	report := checker.Check(observability.WithCorrelationID(context.Background(), "req-1"))
	if report.Status != StatusDegraded { t.Fatalf("status = %q", report.Status) }
	if report.Components["worker"].Status != StatusUnavailable { t.Fatalf("worker = %#v", report.Components["worker"]) }
}

func TestCheckerRedactsDependencyErrors(t *testing.T) {
	checker := NewChecker(fakeDB{err: errors.New("postgres://user:secret@db")}, fakeWorkers{}, fixedClock, 6*time.Second)
	report := checker.Check(context.Background())
	encoded, _ := json.Marshal(report)
	if bytes.Contains(encoded, []byte("secret")) { t.Fatalf("leaked: %s", encoded) }
}
```

- [ ] **Step 2: Run checker tests and verify red**

Run: `go test ./backend/internal/platform/systemhealth -v`

Expected: FAIL because the module does not exist.

- [ ] **Step 3: Implement deterministic aggregation**

`Checker.Check` always emits component keys `api`, `database`, and `worker`. API is healthy when the check executes. Database errors map to `unavailable`/`database unavailable`; missing or expired Worker maps to `unavailable`/`worker heartbeat unavailable`. Overall status is `healthy` only when all three components are healthy, otherwise `degraded`. Dependency error strings never enter `Report`.

Lease age exactly equal to TTL remains healthy; only `age > TTL` expires it. Clamp a negative age to zero to tolerate small clock skew.

- [ ] **Step 4: Write the OpenAPI contract and contract test**

`contracts/openapi/system-health.yaml` is OpenAPI 3.1.1 and fixes this response shape:

```json
{
  "status": "healthy",
  "checkedAt": "2026-08-13T04:00:00Z",
  "correlationId": "req-1",
  "components": {
    "api": { "status": "healthy", "checkedAt": "2026-08-13T04:00:00Z" },
    "database": { "status": "healthy", "checkedAt": "2026-08-13T04:00:00Z" },
    "worker": { "status": "healthy", "checkedAt": "2026-08-13T04:00:00Z" }
  }
}
```

The schema requires all top-level properties and component `status`/`checkedAt`, makes component `detail` optional, forbids additional properties, constrains `detail` to 200 characters, and documents 200 for system health, 200/503 for readiness, and 200 for liveness. Pin `gopkg.in/yaml.v3 v3.0.1`. `tests/contract/system_health_contract_test.go` parses the YAML into typed test structs, asserts `openapi: 3.1.1`, traverses the parsed `paths`/response maps for every route and status code, and compares the parsed status enum with the JSON produced by a representative `Report`. Malformed YAML, comment-only route names, misplaced enums, or missing response schemas must fail. This is a lightweight structured source-contract test; do not add a code generator in ITER-0001.

- [ ] **Step 5: Run health and contract tests**

Run: `go test ./backend/internal/platform/systemhealth ./tests/contract -v`

Expected: PASS.

- [ ] **Step 6: Update evidence and commit**

```bash
git add backend/internal/platform/systemhealth contracts/openapi/system-health.yaml tests/contract go.mod go.sum docs/iterations/ITER-0001/progress.md docs/iterations/ITER-0001/test-matrix.md
git commit -m "feat: define system health contract"
```

### Task 6: Correlation Middleware, Stable Errors, and Go API Process

**Files:**
- Create: `backend/internal/platform/server/correlation.go`
- Create: `backend/internal/platform/server/correlation_test.go`
- Create: `backend/internal/platform/server/response.go`
- Create: `backend/internal/platform/server/api.go`
- Create: `backend/internal/platform/server/api_test.go`
- Create: `backend/internal/platform/server/serve.go`
- Create: `backend/internal/platform/server/serve_test.go`
- Create: `backend/cmd/api/main.go`
- Modify: `docs/iterations/ITER-0001/progress.md`
- Modify: `docs/iterations/ITER-0001/test-matrix.md`

**Interfaces:**
- Consumes: Task 2 config/logging, Task 3 pool/schema check, Task 4 registry, and Task 5 checker.
- Produces:
  - `func server.Correlation(next http.Handler) http.Handler`.
  - `type server.Readiness func(context.Context) error`.
  - `type server.ErrorResponse struct` with string fields `Code`, `Message`, and `CorrelationID`, encoded as JSON keys `code`, `message`, and `correlationId`.
  - `func server.NewAPIHandler(ready Readiness, checker *systemhealth.Checker) http.Handler`.
  - `func server.Serve(ctx context.Context, srv *http.Server, shutdownTimeout time.Duration) error`.

- [ ] **Step 1: Write failing Correlation ID middleware tests**

Cover valid passthrough, missing generation, invalid replacement, response header, and context propagation:

```go
func TestCorrelationReplacesInvalidHeader(t *testing.T) {
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, observability.CorrelationID(r.Context()))
	})
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("X-Correlation-ID", "bad value with spaces")
	rr := httptest.NewRecorder()
	Correlation(next).ServeHTTP(rr, req)
	got := rr.Header().Get("X-Correlation-ID")
	if got == "" || got == "bad value with spaces" || rr.Body.String() != got { t.Fatalf("id = %q body = %q", got, rr.Body.String()) }
}
```

- [ ] **Step 2: Implement middleware with one validation rule**

Compile `^[A-Za-z0-9._-]{1,128}$` once. Generate 16 random bytes using `crypto/rand` and encode lowercase hex. If secure randomness fails, return a 500 stable error instead of falling back to timestamps or predictable IDs.

Run: `go test ./backend/internal/platform/server -run TestCorrelation -v`

Expected: PASS.

- [ ] **Step 3: Write failing API handler tests**

Use `httptest` to assert method handling, content type, correlation IDs, exact stable error envelope, and health semantics:

```go
func TestReadyReturnsStable503(t *testing.T) {
	h := NewAPIHandler(func(context.Context) error { return errors.New("postgres://secret") }, healthyChecker())
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/health/ready", nil))
	if rr.Code != http.StatusServiceUnavailable { t.Fatalf("status = %d", rr.Code) }
	var got ErrorResponse
	if err := json.NewDecoder(rr.Body).Decode(&got); err != nil { t.Fatal(err) }
	if got.Code != "not_ready" || got.Message != "service is not ready" || got.CorrelationID == "" { t.Fatalf("body = %#v", got) }
	if strings.Contains(rr.Body.String(), "secret") { t.Fatal("dependency error leaked") }
}
```

In the same test file, `healthyChecker` constructs `systemhealth.NewChecker` with a no-error database fake, a Worker lease at the injected current time, a fixed UTC clock, and a six-second TTL; it is a test helper, not a production interface.

Expected endpoint behavior:

| Route | Healthy | Dependency failure |
|---|---|---|
| `/health/live` | 200 `{"status":"healthy"}` | still 200 while process loop lives |
| `/health/ready` | 200 `{"status":"healthy"}` | 503 stable `not_ready` envelope |
| `/api/v1/system/health` | 200 health report | 200 degraded health report |

Any non-GET method returns 405 and `Allow: GET`; an unknown path returns JSON 404 `not_found`.

- [ ] **Step 4: Implement API delivery and graceful lifecycle**

`NewAPIHandler` owns routing and JSON transport only. `Serve` starts `ListenAndServe` in a goroutine, treats `http.ErrServerClosed` as normal, and on context cancellation calls `Shutdown` with the configured timeout before returning. Its test uses a listener on `127.0.0.1:0`, cancels context, and asserts the function exits within a one-second test timeout without sleeping.

`cmd/api/main.go` performs only composition:

```text
load RoleAPI config
-> create logger
-> open pgx pool
-> define readiness as pool.Ping + RequireSchema(CurrentSchemaVersion)
-> create workerstatus.Registry and systemhealth.Checker
-> create HTTP server with NewAPIHandler
-> run under signal.NotifyContext(SIGINT, SIGTERM)
```

Log stable error categories only. Exit non-zero on startup or serve failure.

- [ ] **Step 5: Run server tests and build API**

Run: `go test ./backend/internal/platform/server -race -v`

Expected: PASS.

Run: `go build ./backend/cmd/api`

Expected: PASS.

- [ ] **Step 6: Update evidence and commit**

```bash
git add backend/internal/platform/server backend/cmd/api docs/iterations/ITER-0001/progress.md docs/iterations/ITER-0001/test-matrix.md
git commit -m "feat: expose API health endpoints"
```

### Task 7: Worker Health Probes and Worker Process Composition

**Files:**
- Create: `backend/internal/platform/server/worker.go`
- Create: `backend/internal/platform/server/worker_test.go`
- Create: `backend/internal/platform/workerstatus/state.go`
- Create: `backend/internal/platform/workerstatus/state_test.go`
- Create: `backend/cmd/worker/main.go`
- Modify: `docs/iterations/ITER-0001/progress.md`
- Modify: `docs/iterations/ITER-0001/test-matrix.md`

**Interfaces:**
- Consumes: Task 2 config/logging, Task 3 pool/schema check, Task 4 Heartbeat, and Task 6 `server.Serve`/Correlation.
- Produces:
  - `type workerstatus.State` with `MarkHeartbeatSuccess`, `MarkHeartbeatFailure`, and `Ready() bool`.
  - `func server.NewWorkerHealthHandler(ready Readiness, heartbeatReady func() bool) http.Handler`.

- [ ] **Step 1: Write failing Worker state tests**

State begins not ready, becomes ready after the first successful heartbeat, becomes not ready after a heartbeat error, and is safe under `go test -race`:

```go
func TestStateRequiresSuccessfulHeartbeat(t *testing.T) {
	var state State
	if state.Ready() { t.Fatal("new state must not be ready") }
	state.MarkHeartbeatSuccess()
	if !state.Ready() { t.Fatal("success must make state ready") }
	state.MarkHeartbeatFailure()
	if state.Ready() { t.Fatal("failure must make state not ready") }
}
```

- [ ] **Step 2: Implement State and Worker probe handler**

Use `atomic.Bool`; do not expose the atomics. Worker `/health/live` always returns 200 while its health server loop is alive. Worker `/health/ready` calls database/schema readiness and requires `heartbeatReady()`. It returns the same stable 503 envelope as API.

- [ ] **Step 3: Write failing Worker handler tests**

Test database unavailable, heartbeat not yet recorded, both ready, invalid method, correlation header, and redaction. Use the same response assertions as Task 6.

- [ ] **Step 4: Compose the Worker process**

`cmd/worker/main.go` must:

```text
load RoleWorker config
-> create logger and pool
-> verify schema before starting
-> choose instance ID from WORKER_INSTANCE_ID or crypto-random 16-byte hex
-> create Registry, State, NewTimeTickSource(config.HeartbeatInterval), and Heartbeat
-> run Heartbeat and internal health HTTP server under one cancellation context
-> on first non-cancellation error cancel the sibling
-> wait for both, then close pool and exit
```

Wrap the Recorder passed to Heartbeat so each successful/failed `Record` updates `State`. Normal SIGTERM removes the lease through Heartbeat cleanup. The health port binds `WORKER_HEALTH_ADDRESS` but Compose will not publish it to the host.

- [ ] **Step 5: Run Worker tests and build**

Run: `go test ./backend/internal/platform/workerstatus ./backend/internal/platform/server -race -v`

Expected: PASS.

Run: `go build ./backend/cmd/worker`

Expected: PASS.

- [ ] **Step 6: Update evidence and commit**

```bash
git add backend/internal/platform/workerstatus backend/internal/platform/server/worker.go backend/internal/platform/server/worker_test.go backend/cmd/worker docs/iterations/ITER-0001/progress.md docs/iterations/ITER-0001/test-matrix.md
git commit -m "feat: add worker runtime health"
```

### Task 8: Next.js Health Client and Degradable Status Page

**Files:**
- Create: `apps/web/package.json`
- Create: `apps/web/tsconfig.json`
- Create: `apps/web/next.config.ts`
- Create: `apps/web/eslint.config.mjs`
- Create: `apps/web/vitest.config.ts`
- Create: `apps/web/src/test/setup.ts`
- Create: `apps/web/src/app/layout.tsx`
- Create: `apps/web/src/app/page.tsx`
- Create: `apps/web/src/app/globals.css`
- Create: `apps/web/src/app/health/live/route.ts`
- Create: `apps/web/src/features/system-health/types.ts`
- Create: `apps/web/src/features/system-health/fetch-system-health.ts`
- Create: `apps/web/src/features/system-health/fetch-system-health.test.ts`
- Create: `apps/web/src/features/system-health/system-health-view.tsx`
- Create: `apps/web/src/features/system-health/system-health-view.test.tsx`
- Create: `apps/web/src/shared/server/runtime-config.ts`
- Create: `pnpm-lock.yaml`
- Modify: `docs/iterations/ITER-0001/progress.md`
- Modify: `docs/iterations/ITER-0001/test-matrix.md`

**Interfaces:**
- Consumes: Task 5 JSON contract from the Go API.
- Produces:
  - `type HealthStatus = 'healthy' | 'degraded' | 'unavailable'`.
  - `type SystemHealthReport` matching the OpenAPI contract.
  - `async function fetchSystemHealth(baseURL: string, fetcher: typeof fetch, timeoutMs = 1500): Promise<SystemHealthReport>`.
  - `function unavailableReport(now: Date, correlationId?: string): SystemHealthReport`.
  - `function SystemHealthView({ report }: { report: SystemHealthReport }): React.JSX.Element`.

- [ ] **Step 1: Pin the Web workspace and write failing client tests**

`apps/web/package.json` uses exact versions, no ranges:

```json
{
  "name": "@artificial-brain/web",
  "private": true,
  "version": "0.0.0",
  "engines": { "node": "24.18.0" },
  "scripts": {
    "dev": "next dev --hostname 0.0.0.0",
    "build": "next build",
    "start": "next start --hostname 0.0.0.0",
    "format": "prettier --write .",
    "format:check": "prettier --check .",
    "lint": "eslint . && tsc --noEmit",
    "test": "vitest run"
  },
  "dependencies": {
    "next": "16.2.12",
    "react": "19.2.8",
    "react-dom": "19.2.8"
  },
  "devDependencies": {
    "@testing-library/dom": "10.4.1",
    "@testing-library/jest-dom": "7.0.0",
    "@testing-library/react": "16.3.2",
    "@types/node": "24.13.3",
    "@types/react": "19.2.17",
    "@types/react-dom": "19.2.3",
    "@vitejs/plugin-react": "6.0.4",
    "eslint": "10.6.0",
    "eslint-config-next": "16.2.12",
    "jsdom": "30.0.1",
    "prettier": "3.9.6",
    "typescript": "6.0.3",
    "vitest": "4.1.10"
  }
}
```

Generate `pnpm-lock.yaml` with `corepack pnpm install --frozen-lockfile=false`, then use `--frozen-lockfile` thereafter.

Write client tests for a valid report, non-2xx, malformed JSON, timeout/abort, and raw error redaction. The unavailable fallback must use `status: 'unavailable'`, mark API unavailable, omit raw exception text, and preserve all four UI components by letting the view add Web locally.

- [ ] **Step 2: Run the client tests and verify red**

Run: `corepack pnpm --filter @artificial-brain/web test -- fetch-system-health.test.ts`

Expected: FAIL because the client module is missing.

- [ ] **Step 3: Implement strict server-side health fetching**

`runtime-config.ts` is server-only and reads `API_INTERNAL_URL`, defaulting to `http://localhost:8080`; it validates an absolute `http:` or `https:` URL. No file under `src/features` reads `process.env`.

`fetchSystemHealth` uses `AbortSignal.timeout(timeoutMs)`, `cache: 'no-store'`, and `accept: application/json`. Validate the top-level status, required component keys, component statuses, RFC3339 timestamps, and correlation ID before returning; malformed data becomes `unavailableReport` rather than reaching the view.

- [ ] **Step 4: Write failing view tests for three product states**

```tsx
test.each([
  ['healthy', 'All systems operational'],
  ['degraded', 'Some systems need attention'],
  ['unavailable', 'Health status unavailable'],
] as const)('renders %s status', (status, heading) => {
  render(<SystemHealthView report={reportWithStatus(status)} />)
  expect(screen.getByRole('heading', { name: heading })).toBeInTheDocument()
  for (const name of ['Web', 'API', 'PostgreSQL', 'Worker']) {
    expect(screen.getByText(name)).toBeInTheDocument()
  }
})
```

In that test file, `reportWithStatus` returns a complete `SystemHealthReport` with API, database, and Worker components at `2026-08-13T04:00:00Z`; for degraded it marks Worker unavailable, and for unavailable it marks API unavailable. This helper stays test-local.

Also assert no internal URL, stack trace, or connection detail is rendered.

- [ ] **Step 5: Implement the App Router page and internal liveness route**

`page.tsx` is a Server Component: load the internal URL, call `fetchSystemHealth`, and render `SystemHealthView`. The view adds a local healthy Web card at render time and maps each status to text plus a non-color-only badge. Add semantic headings, `<time dateTime>`, responsive CSS, visible focus styles, and `data-system-status` on the main element for smoke tests.

`GET /health/live` returns `Response.json({ status: 'healthy' })`. `next.config.ts` sets `output: 'standalone'`; do not enable experimental cache behavior in this iteration.

- [ ] **Step 6: Run Web verification**

Run: `corepack pnpm --filter @artificial-brain/web test`

Expected: PASS.

Run: `corepack pnpm --filter @artificial-brain/web lint`

Expected: PASS with strict TypeScript and ESLint.

Run: `corepack pnpm --filter @artificial-brain/web build`

Expected: PASS and produce `.next/standalone`.

- [ ] **Step 7: Update evidence and commit**

```bash
git add apps/web pnpm-lock.yaml docs/iterations/ITER-0001/progress.md docs/iterations/ITER-0001/test-matrix.md
git commit -m "feat: add degradable system health page"
```

### Task 9: Executable Architecture Policy with Positive and Negative Fixtures

**Files:**
- Create: `architecture/policy/policy.go`
- Create: `architecture/policy/policy_test.go`
- Create: `architecture/tests/dependencies_test.go`
- Create: `architecture/tests/testdata/valid/backend/internal/modules/todo/domain/todo.go`
- Create: `architecture/tests/testdata/valid/backend/internal/modules/todo/application/create.go`
- Create: `architecture/tests/testdata/invalid-domain/backend/internal/modules/todo/domain/todo.go`
- Create: `architecture/tests/testdata/invalid-cross-context/backend/internal/modules/todo/application/create.go`
- Create: `architecture/tests/testdata/invalid-platform/backend/internal/platform/database/bad.go`
- Create: `architecture/tests/testdata/invalid-web/apps/web/src/features/system-health/bad.ts`
- Modify: `Makefile`
- Modify: `docs/iterations/ITER-0001/progress.md`
- Modify: `docs/iterations/ITER-0001/test-matrix.md`

**Interfaces:**
- Consumes: repository structure and dependency constraints from approved designs.
- Produces:
  - `type policy.Violation { File, Rule, Import string }`.
  - `func policy.Validate(root string) ([]Violation, error)`.
  - `make architecture-test`.

- [ ] **Step 1: Write failing fixture-driven policy tests**

```go
func TestValidateFixtures(t *testing.T) {
	tests := []struct {
		name string
		root string
		wantRule string
	}{
		{"valid", "testdata/valid", ""},
		{"domain imports adapter", "testdata/invalid-domain", "go-domain-dependency"},
		{"cross context internal import", "testdata/invalid-cross-context", "go-cross-context"},
		{"platform imports business", "testdata/invalid-platform", "go-platform-dependency"},
		{"web feature reads deployment env", "testdata/invalid-web", "web-feature-env"},
	}
	// Run Validate, require zero violations for valid, and exact wanted rule otherwise.
}
```

Negative fixture imports are literal source strings; they do not need to compile because the validator uses `go/parser` and filesystem scanning.

- [ ] **Step 2: Run architecture tests and verify red**

Run: `go test ./architecture/... -v`

Expected: FAIL because `policy.Validate` does not exist.

- [ ] **Step 3: Implement one deterministic validator**

Walk source files in lexical order, ignore `.git`, `node_modules`, `.next`, generated output, and all `testdata` unless the supplied root itself is a fixture. Parse Go imports with `go/parser`; scan TypeScript only for the explicit Web rules.

Rules:

1. A path containing `/domain/` may import only the Go standard library or its own module's `/domain/` path.
2. A path containing `/application/` rejects `/adapters/`, `/platform/database`, `/platform/server`, `net/http`, pgx, River, OpenAI, SMTP, and SMS SDK imports.
3. A file under `modules/<A>` rejects imports under `modules/<B>/(domain|adapters)` when A != B.
4. A file under `backend/internal/platform` rejects any `/backend/internal/modules/` import.
5. A file under `apps/web/src/features` rejects `process.env`, `API_INTERNAL_URL`, Compose hostnames, or imports from `src/shared/server`.

Return all violations, sorted by file/rule/import. Do not stop on the first failure.

- [ ] **Step 4: Add the real-tree architecture gate**

`architecture/tests/dependencies_test.go` finds repository root from the test file location, calls `policy.Validate(root)`, and formats every violation in one failure. Add:

```make
.PHONY: architecture-test
architecture-test:
	@go test ./architecture/... -v
```

- [ ] **Step 5: Verify positive, negative, and real repository cases**

Run: `make architecture-test`

Expected: PASS; fixture tests prove each negative rule fires, and the real repository has zero violations.

- [ ] **Step 6: Update evidence and commit**

```bash
git add architecture Makefile docs/iterations/ITER-0001/progress.md docs/iterations/ITER-0001/test-matrix.md
git commit -m "test: enforce modular architecture boundaries"
```

### Task 10: Reproducible Container Images, Compose Startup Ordering, and Smoke Tests

**Files:**
- Create: `.env.example`
- Create: `.dockerignore`
- Create: `backend/Dockerfile`
- Create: `apps/web/Dockerfile`
- Create: `compose.yaml`
- Create: `tests/smoke/wait_for_url.sh`
- Create: `tests/smoke/stack_test.sh`
- Create: `tests/smoke/migration_test.sh`
- Modify: `Makefile`
- Modify: `docs/iterations/ITER-0001/progress.md`
- Modify: `docs/iterations/ITER-0001/test-matrix.md`

**Interfaces:**
- Consumes: all binaries and Web build from Tasks 3, 6, 7, and 8.
- Produces: `make dev`, `make down`, `make build`, `make migration-test`, `make smoke-test`, and guarded `make clean-local-data`.

- [ ] **Step 1: Write failing Compose structure checks**

Start `tests/smoke/stack_test.sh` with a `--config-only` mode that runs `docker compose config --quiet`, renders JSON/YAML config, and asserts:

```text
runtime services: postgres, migrate, api, worker, web; backend-test may exist only behind the test profile
migrate depends on postgres service_healthy
api and worker depend on migrate service_completed_successfully
web depends on api service_healthy
worker has no host-published ports
postgres image is postgres:18.4-alpine
```

Run: `sh tests/smoke/stack_test.sh --config-only`

Expected: FAIL because Compose files do not exist.

- [ ] **Step 2: Add pinned multi-stage images**

`backend/Dockerfile` uses `golang:1.26.5-alpine` for build and a pinned Alpine runtime. One build stage produces `/out/api`, `/out/worker`, and `/out/migrate` with `CGO_ENABLED=0`; the final target selects one via build arg and runs as a non-root UID. It copies CA certificates and no source files. A separate `test` target retains source and the Go toolchain solely for the Compose `test` profile.

`apps/web/Dockerfile` uses `node:24.18.0-alpine`, enables Corepack, performs `pnpm install --frozen-lockfile`, builds the standalone app, and copies only standalone/server/static output into a non-root runtime stage.

`.dockerignore` excludes `.git`, `.env*` except `.env.example`, `.DS_Store`, node modules, `.next`, coverage, and local build output.

- [ ] **Step 3: Add startup-ordered Compose configuration**

Use a named `postgres-data` volume and these non-secret local defaults in `.env.example`:

```dotenv
POSTGRES_DB=artificial_brain
POSTGRES_USER=artificial_brain
POSTGRES_PASSWORD=local-development-only
SERVICE_VERSION=dev
API_HTTP_ADDRESS=:8080
WORKER_HEALTH_ADDRESS=:8081
WORKER_HEARTBEAT_INTERVAL=2s
WORKER_LEASE_TTL=6s
SHUTDOWN_TIMEOUT=10s
```

Compose derives `DATABASE_URL` inside the private network. Publish Web as `${WEB_PORT:-3000}:3000` and API as `${API_PORT:-8080}:8080`; do not publish PostgreSQL or Worker. The migrate image receives `/migrations` read-only and exits after migration. Healthchecks use internal `wget`/`pg_isready` with bounded intervals and retries. A `backend-test` service behind profile `test` targets the Dockerfile test stage, receives `TEST_DATABASE_URL`, and is never part of `make dev`.

- [ ] **Step 4: Add bounded migration and stack smoke tests**

`wait_for_url.sh URL SECONDS` polls once per second until success or deadline; on timeout it returns non-zero. This is bounded condition polling, not a fixed long sleep.

`migration_test.sh` creates a unique Compose project name and starts only PostgreSQL. Before migration it uses `docker compose run --no-deps` to start API against the empty database, asserts readiness is 503, stops that container, runs Worker and asserts it exits non-zero, then verifies `public.schema_version` still does not exist. Only then it runs migrate twice, verifies `public.schema_version = 1` and exactly one `runtime.worker_heartbeats` table via `psql`, and runs the database/Worker registry integration tests through the `backend-test` profile with `-race`. It always tears down with `down --volumes` in a trap. This proves API and Worker do not auto-migrate and ensures PostgreSQL adapters are exercised in CI.

`stack_test.sh`:

```text
create unique Compose project
-> up --build --detach --wait
-> assert migrate exited 0
-> assert API live/ready and Web live
-> assert Web HTML has data-system-status="healthy" and labels Web/API/PostgreSQL/Worker
-> stop Worker cleanly
-> poll API system health up to lease TTL + 4s until worker=unavailable and overall=degraded
-> assert Web still responds and renders degraded
-> restart Worker and poll back to healthy
-> always print ps + redacted recent logs on failure
-> always down --volumes for the smoke project
```

Logs are redacted with patterns for URI userinfo, `DATABASE_URL`, passwords, and common secret keys before printing.

- [ ] **Step 5: Complete Make orchestration with safety guard**

Add targets:

```make
.PHONY: dev down build migration-test smoke-test clean-local-data

dev:
	@docker compose up --build

down:
	@docker compose down

build:
	@go build ./backend/cmd/api ./backend/cmd/worker ./backend/cmd/migrate
	@corepack pnpm --filter @artificial-brain/web build

migration-test:
	@sh tests/smoke/migration_test.sh

smoke-test:
	@sh tests/smoke/stack_test.sh

clean-local-data:
	@test "$(CONFIRM)" = "delete" || (echo 'Run make clean-local-data CONFIRM=delete' >&2; exit 1)
	@docker compose down --volumes
```

- [ ] **Step 6: Run Compose structure and integration tests**

Run: `sh tests/smoke/stack_test.sh --config-only`

Expected: PASS.

Run: `make migration-test`

Expected: PASS; migration runs twice without duplicate schema objects.

Run: `make smoke-test`

Expected: PASS; healthy → degraded after Worker stop → healthy after restart.

- [ ] **Step 7: Update evidence and commit**

```bash
git add .env.example .dockerignore backend/Dockerfile apps/web/Dockerfile compose.yaml tests/smoke Makefile docs/iterations/ITER-0001/progress.md docs/iterations/ITER-0001/test-matrix.md
git commit -m "build: add reproducible local runtime"
```

### Task 11: Unified Verification, CI, Run Documentation, and Clean Handoff

**Files:**
- Create: `.github/workflows/ci.yml`
- Create: `README.md`
- Create: `scripts/check-format.sh`
- Create: `scripts/check-secrets.sh`
- Create: `tests/harness/workflow_test.go`
- Modify: `Makefile`
- Modify: `docs/iterations/ITER-0001/progress.md`
- Modify: `docs/iterations/ITER-0001/test-matrix.md`
- Modify: `docs/iterations/ITER-0001/handoff.md`
- Modify: `docs/iterations/ITER-0001/decisions.md`

**Interfaces:**
- Consumes: all prior Make targets.
- Produces: `make format`, `make format-check`, `make lint`, `make test`, `make verify`, and CI that calls those same interfaces.

- [ ] **Step 1: Write failing harness assertions for the complete Make interface**

Extend `tests/harness/repository_policy_test.sh` to exercise the Make interface with a temporary executable `make` dependency fixture directory: invoke `make -n format-check lint architecture-test test build verify` and require every target to resolve successfully without executing its recipe. Run `make -n verify` and assert from Make's expanded command graph that it selects `format-check` and never selects the mutating formatter command. Parse `.github/workflows/ci.yml` with `gopkg.in/yaml.v3` in a small Go harness test, traverse `jobs.*.steps[*].run`, and require executable steps for `make verify`, `make migration-test`, and `make smoke-test`; comments and unrelated scalar strings do not count.

Run: `make harness-test`

Expected: FAIL because the final targets and workflow are missing.

- [ ] **Step 2: Implement read-only verification targets**

`scripts/check-format.sh` fails when `gofmt -l` returns any Go file and then calls root `corepack pnpm format:check`, which covers repository configuration plus Web files. `scripts/check-secrets.sh` scans tracked files only, rejects private-key headers, common live-token prefixes, and credential-bearing PostgreSQL URLs outside `.env.example` and Go/TypeScript test files; it prints file/line but never the matched secret.

Complete Make targets:

```make
format:
	@gofmt -w $$(find backend architecture tests -name '*.go' -type f)
	@corepack pnpm format

format-check:
	@sh scripts/check-format.sh

lint:
	@go vet ./...
	@corepack pnpm --filter @artificial-brain/web lint

test:
	@go test ./... -race
	@corepack pnpm --filter @artificial-brain/web test

verify: harness-test format-check lint architecture-test test build
	@sh scripts/check-secrets.sh
```

`verify` is read-only and excludes Docker-dependent integration/smoke checks; CI runs those afterward in the designed order.

- [ ] **Step 3: Add CI with the same entry points**

`.github/workflows/ci.yml` triggers on pull requests and pushes to `master`, sets a 30-minute job timeout, checks out code, sets up Go 1.26.5, Node 24.18.0, and pnpm 11.19.0 with dependency caches, then runs:

```text
corepack pnpm install --frozen-lockfile
make verify
make migration-test
make smoke-test
```

Always upload redacted smoke diagnostics on failure. Do not duplicate lint/test commands directly in YAML.

- [ ] **Step 4: Write operator/developer documentation**

`README.md` includes prerequisites, `make dev`, URLs, configuration table, health semantics, migration ownership, all verification commands, safe shutdown, guarded local data deletion, and common diagnostics. It explicitly says `make down` preserves data and `make clean-local-data CONFIRM=delete` destroys only this Compose project's local volume.

`decisions.md` records only iteration-specific choices: database heartbeat lease, system-health contract outside business contexts, and tern as the one-shot migration adapter. `handoff.md` lists current HEAD, commands run, expected service URLs, known environment prerequisites, and no unresolved implementation gaps.

- [ ] **Step 5: Run the complete local verification sequence**

Run in this exact order:

```bash
corepack pnpm install --frozen-lockfile
make verify
make migration-test
make smoke-test
git status --short
```

Expected: every command exits 0. `git status --short` shows only deliberate iteration evidence edits plus the user's pre-existing untracked `.DS_Store` files.

- [ ] **Step 6: Finalize implementation-context evidence and commit**

Update every test-matrix row with its command and evidence commit. Mark implementation complete but regression pending in `progress.md` and `handoff.md`.

```bash
git add .github/workflows/ci.yml README.md scripts/check-format.sh scripts/check-secrets.sh Makefile tests/harness/repository_policy_test.sh tests/harness/workflow_test.go docs/iterations/ITER-0001
git commit -m "ci: enforce ITER-0001 verification gates"
```

### Task 12: Independent Clean-Context Regression Gate

**Files:**
- Create: `docs/iterations/ITER-0001/regression-report.md`
- Modify only if regression finds a defect: files named by the defect, plus `progress.md`, `test-matrix.md`, and `handoff.md`.

**Interfaces:**
- Consumes: approved design, this plan, the ITER-0001 commit range, README verification instructions, and no implementation conversation.
- Produces: an independent regression verdict with exact evidence.

- [ ] **Step 1: Prepare the clean regression brief**

Give the regression reviewer only:

```text
Spec: docs/superpowers/specs/2026-08-13-iter-0001-runnable-skeleton-design.md
Plan: docs/superpowers/plans/2026-08-13-iter-0001-runnable-skeleton.md
Iteration evidence: docs/iterations/ITER-0001/{brief,spec,plan,test-matrix,handoff}.md
Diff: merge-base with origin/master through current HEAD
Commands: README.md and Makefile
Restrictions: inspect and test first; do not read implementation chat; do not modify code before reporting findings
```

- [ ] **Step 2: Run independent acceptance and scope regression**

The clean reviewer must independently run:

```bash
make verify
make migration-test
make smoke-test
git diff --check origin/master...HEAD
git diff --name-status origin/master...HEAD
```

It also maps all ten approved acceptance criteria to evidence, checks no business behavior or River/model/vendor dependency was introduced, checks yellow/red modification zones, and scans logs/contracts for credential disclosure.

- [ ] **Step 3: Write the immutable regression report**

`regression-report.md` contains reviewer context, exact commit range, environment versions, commands and exit codes, acceptance matrix, scope/architecture/security observations, findings ordered by severity, and one verdict: `PASS` or `FAIL`. It contains no unverified “should pass” language.

- [ ] **Step 4: If FAIL, repair through a fresh TDD loop and rerun with a new clean reviewer**

For each finding: add or strengthen a failing test reproducing it, verify red, implement the smallest fix, verify green, run affected and full gates, update evidence, and commit `fix: <specific regression>`. The original regression report remains immutable; a new clean reviewer creates a superseding report after fixes.

- [ ] **Step 5: If PASS, finalize iteration evidence and commit**

Set ITER-0001 status to complete in `progress.md` and `handoff.md`, reference the regression report commit and exact full-gate evidence, then:

```bash
git add docs/iterations/ITER-0001/regression-report.md docs/iterations/ITER-0001/progress.md docs/iterations/ITER-0001/handoff.md
git commit -m "docs: record ITER-0001 regression approval"
```

## Plan Completion Evidence

Before implementation begins, validate this plan itself:

```bash
git diff --check -- docs/superpowers/plans/2026-08-13-iter-0001-runnable-skeleton.md
rg -n '\b(T[B]D|T[O]DO|F[I]XME|X[X]X)\b|implement[[:space:]]+later|fill[[:space:]]+in|similar[[:space:]]+to|待[定]|稍后[实现]' docs/superpowers/plans/2026-08-13-iter-0001-runnable-skeleton.md
```

Expected: `git diff --check` exits 0 and the placeholder scan returns no matches.
