# ITER-0003 regression report

This report is the immutable record of the independent clean-context regression for ITER-0003 (reliable reminder delivery), written by the regression agent with no access to the implementation conversation. It must not be edited after commit; a failing successor iteration supersedes it with a new report.

- Date: 2026-08-20
- Branch: `iter-0003-reliable-reminder-delivery`, tip `8ef71d6` (`ci: finalize ITER-0003 verification evidence`)
- Merge base: `git merge-base origin/master HEAD` = `b441625cbaa3cb7fda0be0783e1645243d4feb07`
- Diff under review: `b441625...8ef71d6`, 103 files, +12274/−173 lines
- Inputs used: master MVP design, ITER-0003 brief/spec/plan/decisions/test-matrix/handoff, the iteration design and implementation plan, the merge-base…HEAD diff, README.md, Makefile. No other context was consulted.

## Verdict: PASS

All gates pass when run exactly as documented, all 10 acceptance criteria map to concrete evidence, the diff stays inside the declared zones, and no credentials were committed. One verification incident occurred and is recorded below for transparency: an initial `make verify` invocation run with `TEST_DATABASE_URL` exported **and** default package parallelism failed with exit 2 due to shared-test-database contention between concurrently running test packages. That invocation contradicts the documented host procedure (handoff: "packages sharing that database run with `-p=1`") and the CI definition (ci.yml never sets `TEST_DATABASE_URL`); the identical tests pass both in CI mode (skipped) and under the serialized `make migration-test` adapter proof. It is an invocation artifact, not an iteration defect.

## Environment

| Item | Value |
| --- | --- |
| OS | Linux (host runner) |
| Go | go1.26.5 linux/amd64 (pin: 1.26.5 or newer 1.26 patch) |
| Node.js | v24.18.0 |
| pnpm | 11.19.0 (Corepack) |
| Docker / Compose | Docker 26.1.3 / Docker Compose v2.27.0 |
| Test database | dedicated `ab-test-postgres` container (`postgres:18.4-alpine`, host port 5433, db `artificial_brain_test`) |
| Local overrides | gitignored `compose.override.yaml` (mirror build args, host-network builds, `dns_search: []`, absolute `http://api.:8080`), as documented in handoff.md |
| Egress | restricted host; Go modules resolved via Aliyun GOPROXY mirror during image builds |

## Commands run and results

| # | Command | Exit code | Notes |
| --- | --- | --- | --- |
| 1 | `corepack pnpm install --frozen-lockfile` | 0 | |
| 2 | `TEST_DATABASE_URL=postgres://…@127.0.0.1:5433/artificial_brain_test?sslmode=disable make verify` | **2** | **Misconfigured invocation** — see incident below. 6 DB-backed integration tests failed with contention signatures. |
| 3 | `make migration-test` | 0 | Empty-schema readiness gate, API/Worker refuse to migrate, idempotent double migrate, `schema_version=7` pin, `runtime.worker_heartbeats` present, then serialized adapter DB proof `go test -p=1 -race -v ./backend/internal/platform/database ./backend/internal/platform/workerstatus ./backend/internal/modules/... ./backend/cmd/api` all green in a clean container against a freshly migrated v7 schema. |
| 4 | `make smoke-test` | 0 | Full healthy/degraded/recovered compose proof + authenticated E2E incl. the ITER-0003 reminder block (delivery, suppression, signed receipt, ops snapshot, worker-restart recovery). Smoke compose project `artificial-brain-smoke-1787213268-3681065` created 16:07:48, gate exited 0 at 16:08:28 (cached images; all wait loops are ≤30 s deadlines that break as soon as the condition holds). The final `worker-1 Restarting/Started` compose events occur only in the reminder block, confirming the block ran to completion. |
| 5 | `env -u TEST_DATABASE_URL make verify` (CI mode, re-run) | 0 | harness-test, format-check (pretier), lint (`go vet` + eslint + `tsc --noEmit`), architecture-test, `go test ./... -race` (DB integration tests skipped when `TEST_DATABASE_URL` unset, per README), web vitest **15 files / 78 tests passed**, `go build` (api/worker/migrate) + `next build`, `check-secrets.sh` — all green. |
| 6 | `git diff --check b441625cbaa3cb7fda0be0783e1645243d4feb07...HEAD` | 0 | No whitespace errors or conflict markers. |

### Incident record: command 2 (`make verify` with `TEST_DATABASE_URL` set, default parallelism)

The regression agent exported `TEST_DATABASE_URL` for the whole gate sequence to exercise the DB integration tests during `make verify`. `go test ./... -race` runs packages concurrently, and every DB-backed suite shares the single test database, so concurrently running packages truncated/stole each other's rows and jobs. Failures, all with contention signatures:

- `backend/cmd/api`: `TestTodoReminderAtomicComposition` (`todo: todo not found` mid-test), `TestRiverSchedulerCommitsJobsAtomically` (plan rows gone after create)
- `identity/adapters/outbound/fakeoutbox`: `TestOutboxWritesMessage` (outbox count 0 after write)
- `reminder/adapters/outbound/postgres`: `TestDeliveryStoreDuplicateIdempotencyKey` (`ERROR: deadlock detected (SQLSTATE 40P01)`), `TestDeliveryStoreStatsBuckets` (counts zeroed)
- `reminder/adapters/outbound/river`: `TestScheduleAndWorkDeliversReminderEndToEnd` (15 s timeout; job interfered with by other packages on the shared `river_job` table)

Why this is an invocation artifact, not a defect:

1. The handoff explicitly documents for this host: "Integration tests need `TEST_DATABASE_URL` … packages sharing that database run with `-p=1`." Command 2 violated that requirement; commands 3/5 follow the documented invocations.
2. CI (`.github/workflows/ci.yml`, the normative definition of the acceptance sequence) never sets `TEST_DATABASE_URL`; `make verify` skips the DB integration tests there. In that exact mode `make verify` exits 0 (command 5).
3. Every failing test re-ran fresh and **passed** under the serialized `-p=1` adapter DB proof inside `make migration-test` (command 3): `TestTodoReminderAtomicComposition PASS`, `TestRiverSchedulerCommitsJobsAtomically PASS`, `TestOutboxWritesMessage PASS`, `TestDeliveryStoreDuplicateIdempotencyKey PASS`, `TestDeliveryStoreStatsBuckets PASS`, `TestScheduleAndWorkDeliversReminderEndToEnd PASS` — against a freshly migrated schema v7 in a clean container.
4. The shared-single-database test layout predates ITER-0003 (identity/todo/conversation suites use the same pattern); the iteration did not change the harness's serialization contract.

## Acceptance criteria → evidence

| # | Criterion (brief.md) | Evidence | Status |
| --- | --- | --- | --- |
| 1 | Plan + per-channel Delivery + River Job atomically in the create/reschedule transaction; revoke + best-effort cancel on complete/delete/reschedule | `TestSchedulerScheduleIsAtomicWithAmbientTransaction PASS`, `TestRiverSchedulerCommitsJobsAtomically PASS`, `TestTodoReminderAtomicComposition PASS` (migration gate); `TestPlanReminderSavesDeliveryPerChannelThenWritesBackJobIDs`, `TestRevokePlansCancelsEveryPlannedJobID`, `TestRevokePlansSurvivesCancelErrorAndLogsIt` (verify); `outbound/river/scheduler.go` uses `InsertTx` into the ambient tx and errors without one (`TestScheduleWithoutAmbientTransactionFails`); migration 007 UNIQUE `(todo_id, todo_reminder_version, channel)` ≡ idempotency key; port evolved per D2 (`Schedule` fan-out + `Cancel`, job IDs int64 per A9) | Met |
| 2 | Delivery initiated ≤30 s after due; each enabled+verified email/SMS channel produces a Delivery; fake outbox holds the message | Smoke E2E: todo 冒烟提醒 due +5 s → fake outbox message ≤30 s for the email address, `GET /api/v1/reminders` reaches **2 `succeeded` rows, channels `[email, sms]`**, sms row carries `providerMessageId`; `TestScheduleAndWorkDeliversReminderEndToEnd PASS` | Met |
| 3 | Todo completed/deleted/rescheduled before due never delivers a stale reminder | Smoke E2E: todo 冒烟抑制 completed before due → after the due instant **no row outside `{scheduled, suppressed}`**, exactly 2 rows, and no fake-outbox message containing 冒烟抑制 for either address; unit tests for every suppression branch (`TestSendReminderSuppressesOnExecutionTimeReread`, `TestSendReminderRevokedPlanSuppressesPlanRevoked`, `TestSendReminderUnusableChannelSuppresses`, `TestMarkSuppressedRecordsReason`, `TestSendReminderSuppressionCreatesMissingDeliveryRow`). **Documented adaptation O2 verified**: with best-effort cancel succeeding, a cancelled job never runs so its row legitimately stays `scheduled`; this is consistent with design D1/§6.1 ("抑制以执行时重读为准", cancel is not the correctness boundary), and the `suppressed` write path itself is exercised at unit/integration level by the tests listed here | Met |
| 4 | Transient failures retry via River; retries skip succeeded channels; worker crash recovers without duplicate messages | `TestTransientFailureIsRetriedUntilSuccess PASS`, `TestDuplicateExecutionSendsOnce PASS`, `TestCrashMidFlightDeliversExactlyOnce PASS` (migration gate, fresh runs); `TestSendReminderSecondRunAfterSuccessIsNoOp`, `TestSendReminderFinalDeliveryIsIdempotent`; backoff series `min(500ms·2^(n−1), 60s)` tested (`TestSendWorkerNextRetryDelaySeries`, `TestSendWorkerNextRetryUsesBackoff`); smoke worker-restart step: after `compose restart worker`, todo 冒烟恢复 due +5 s delivered ≤30 s | Met |
| 5 | Permanent failure / retry exhaustion → `failed` terminal + dead-letter log event, visible in dashboard and ops | `TestSendReminderPermanentErrorDeadLetters`, `TestSendReminderTransientErrorOnFinalAttemptDeadLetters`, `TestSendWorkerWorkFinalAttemptDeadLetters`, provider-code classification tests (wrapped/bare/generic); `TestDeliveryStoreStatsBuckets PASS` (failed counted), ops store + HTTP ops tests; dashboard counters derive from the same stats port | Met |
| 6 | Receipt endpoint: valid signature idempotent by ProviderMessageID; invalid rejected; duplicate/unknown safe | HTTP tests: `TestReceiptValidSignatureRecordsReceipt`, `TestReceiptTamperedBodyRejected`, `TestReceiptMissingOrInvalidHeaderRejected`, `TestReceiptUnknownProviderIDStillAccepted`, malformed-JSON/missing-ID/oversized-body rejections; unit: `TestRecordReceiptAppliesOnceToSucceededDelivery`, `TestRecordReceiptIgnoresNonSucceededDelivery`, `TestRecordReceiptUnknownProviderIDIsIgnored`; smoke: HMAC-SHA256-signed receipt for the real sms `providerMessageId` → 200 and `receiptState == "received_ok"` | Met |
| 7 | Real four-state dashboard counters; delivery record list; ops endpoint (queue depth, oldest wait, state counts, retry rate, dead letters, latency P95) | `reminder.yaml` contract (ReminderOps: per-queue `depth`+`oldestWaitSeconds`, DeliveryCounts incl. `retrying`/`failed`, `retryRate`, `latencyP95Ms`) validated by `tests/contract` green; `TestOpsReturnsReminderOpsJSON`, `TestOpsEmptyQueuesSerializesAsArray`, ops integration tests; smoke asserts `.queues | length == 2` and `.deliveries.succeeded >= 2`; web vitest 15/78 green incl. four real reminder tiles + 提醒记录 list (`fetch-reminders.test.ts`, dashboard-panel/view tests) | Met |
| 8 | CI/local deliver only via fake adapters; production selecting fake fails to start; real adapters unit-tested | Config tests green: `TestLoadRejectsFakeAdaptersInProduction`, `TestLoadAcceptsRealAdaptersInProduction`, `TestLoadRejectsReminderDevOutboxInProduction`, `TestLoadReceiptSecretRequiredForAPIRoleOnly`, `TestLoadReminderErrorsNeverEchoSecretValues`; SMTP via local test listener and Aliyun via httptest fixtures (`smtp/notifier_test.go`, `aliyun/notifier_test.go` + verdict-normalization tests); compose + `.env.example` default `fake/fake`; smoke static assertion pins those env values; CI never configures a real provider | Met |
| 9 | `make verify`, `make migration-test`, `make smoke-test` green on clean checkout; architecture policy green; go.mod adds only the two River modules | Exit codes above (0/0/0 in the documented modes); `architecture/policy` + `architecture/tests` green incl. `TestRepositoryHasNoArchitectureViolations`; go.mod adds direct deps `riverqueue/river`, `riverqueue/river/riverdriver/riverpgxv5`, `riverqueue/river/rivertype` (three module paths from the two ADR-0002-sanctioned upstream repositories — the documented O1 adaptation to River v0.44.0's nested module layout); `jackc/pgx/v5` 5.9.2→5.10.0 is the minimum River v0.44.0 itself requires (verified in River's own go.mod); remaining additions are transitive indirects (`rivershared`, `riverdriver`, tidwall json libs); zero web dependency changes (`package.json`/`pnpm-lock.yaml` untouched) | Met |
| 10 | Iteration ledger complete; independent clean-context regression produces a PASS report | brief/spec/plan/decisions/test-matrix/handoff/progress all present and mutually consistent with the diff; this report | Met |

### Master-design acceptance scenarios (delivery slice 3)

- **Scenario 7** (each enabled+verified email/sms channel delivers at due time): covered by the smoke E2E delivery block (2 channels, 2 `succeeded` rows, fake-outbox messages) plus `TestScheduleAndWorkDeliversReminderEndToEnd`.
- **Scenario 8** (crash mid-delivery recovers; duplicate execution idempotent): covered by `TestCrashMidFlightDeliversExactlyOnce`, `TestDuplicateExecutionSendsOnce` (fake-outbox count stays 1), and the smoke worker-restart recovery step (documented adaptation A7: restart stands in for crash at the smoke level; kill-mid-flight semantics live in the adapter tests).
- **Scenario 9** (completed/deleted/rescheduled todo never delivers stale reminder): covered by the smoke suppression block (execution-time suppression + best-effort cancel, O2 adaptation verified against D1) and the full suppression unit-test branch set.

## Zone compliance

| Check | Result |
| --- | --- |
| New Go dependencies | Compliant: only the River modules (see criterion 9). River imports confined to `reminder/adapters/outbound/river`, `reminder/adapters/inbound/worker`, and `cmd` wiring; domain/application remain River-free; architecture tests enforce this and pass. The no-op scheduler is retired from production wiring and survives only as a test fake (per D2). |
| Migrations | Compliant: `001–005` byte-identical (diff touches only new `006_river_v1.sql` and `007_create_reminder_deliveries.sql`); 006 inlines River v0.44.0 official SQL with the source version in its header (D4); both files carry tern `---- create above / drop below ----` drop sections; `CurrentSchemaVersion` 5→7; `migration_test.sh` pinned to 7; migration-test exercised up/down ownership rules (API/Worker refuse empty schema, idempotent re-run). |
| Contracts | Compliant: `contracts/openapi/reminder.yaml` added and `dashboard.yaml` extended with `reminderSucceeded`/`reminderSuppressed`; system-health contract and health routes unchanged; contract tests green. |
| CI workflow | Compliant: `.github/workflows/ci.yml` untouched; harness `TestWorkflow` pins the gate sequence and passes. |
| AGENTS.md | Compliant: root, `backend/`, and `apps/web/AGENTS.md` refreshed to ITER-0003 zones (green reminder delivery work; red = no Portability, no real-provider calls from CI, no credentials, no lowered gates). |
| Web scope | Compliant: dashboard-page only (four tiles + 提醒记录 section), no new route page, zero new web dependencies (D8). |
| Architecture policy files | Unchanged, and `make architecture-test` green as planned (yellow-register item 11). |
| Working tree | Clean at tip; one untracked, non-committed artifact `api.exe` exists at the repo root (mtime predates ITER-0003, not in the diff, not tracked) — observation only, no action required for this iteration. |

## Credential scan

- `scripts/check-secrets.sh` ran green inside `make verify` (both runs).
- Manual scan of the full diff for `secret|password|token|api_key|access_key|LTAI` hits found only:
  - the documented local-development-only defaults (`REMINDER_RECEIPT_SECRET=local-development-only` in `.env.example`, compose interpolation `${REMINDER_RECEIPT_SECRET:-local-development-only}`, README env table);
  - obvious test fixtures in test files (`receipt-secret`, `other-secret`, `composition-receipt-secret`, `test-access-key-id`, `test-access-key-secret`, `secret-password`);
  - config plumbing names (`ReminderSmtpPassword`, `ReminderAliyunAccessKeySecret`) with empty-string defaults, plus a test asserting reminder config errors never echo secret values.
- No real receipt secret values, SMTP passwords, Aliyun access keys, or non-documented database passwords anywhere in the diff. Smoke diagnostics on failure are redacted by `scripts/redact-logs.sh` in CI.

## Closing statement

The iteration's claims reproduced from a clean context: the documented acceptance sequence is green on this host, the delivery closed loop is evidenced at unit, integration, contract, and smoke levels, the zone and dependency discipline held, and no credentials leaked. The single failed gate attempt was caused by the regression agent's own non-documented invocation and is fully accounted for above.

**Verdict: PASS.**
