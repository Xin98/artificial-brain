# ITER-0003 decisions

These choices are specific to the reliable reminder delivery closed loop. Broader architecture remains governed by the [MVP design](../../superpowers/specs/2026-08-13-ai-native-personal-workbench-mvp-design.md) and the [iteration design](../../superpowers/specs/2026-08-19-iter-0003-reliable-reminder-delivery-design.md).

## D1 — Pipeline approach A

The River worker is Reminder's only inbound delivery entry point and calls the `SendReminder` application command; all suppression/idempotency/provider logic lives in the application layer. Rejected: orchestration inside the worker adapter (layering violation) and API-side queue consumption (MVP §6.6).

## D2 — River behind the evolved `JobScheduler` port

`Schedule(ctx, ReminderJob) ([]ScheduledChannel, error)` (fan-out per channel; job IDs returned for writeback) and `Cancel(ctx, jobID int64) error` (best-effort). The River scheduler joins the caller's ambient transaction via `database.ExecutorFromContext` + a `pgx.Tx` type assertion and `InsertTx`; without an ambient transaction it errors (no silent fallback). The no-op adapter is retired from `cmd/api` wiring (kept only as a test fake).

## D3 — Deliveries are created at plan time

One `scheduled` delivery per requested channel in the same transaction as the plan; UNIQUE `(todo_id, todo_reminder_version, channel)` ≡ idempotency key. "Retrying" is derived (`state=sending ∧ attempt_count>0`); `sending` with `attempt_count=0` is the first attempt in flight. Terminal states are immutable; receipts never flip them.

## D4 — River SQL inlined into the tern sequence

`006_river_v1.sql` carries River's official SQL with the source River version in the header; River upgrades append new migrations, never edit 006. Rejected: a parallel `rivermigrate` pipeline and worker-time self-migration (violates migration ownership).

## D5 — Stdlib-only real adapters

SMTP via `net/smtp` (PLAIN auth, dial+send timeouts, 4xx transient / 5xx permanent) and Aliyun SMS via standard-library HTTP + HMAC-SHA1 RPC signing (`Code=OK` ⇒ `BizId` as provider message ID; throttle/5xx/timeout transient; `isv.*`-class permanent). No provider SDKs.

## D6 — Receipts are informational and provider-keyed

`RecordReceipt` applies once per `ProviderMessageID` and only to `succeeded` deliveries; unknown IDs are safely ignored. Lookup by provider message ID is the one documented unscoped read (provider evidence key, cf. session token lookup).

## D7 — Ops endpoint is deterministic SQL

Queue depth and oldest wait come from `river_job`; delivery counts, retry rate, and latency P95 (`percentile_cont(0.95)` of `submitted_at - scheduled_at` over succeeded deliveries in the last 24 h) come from `reminder_deliveries`. No metrics dependency.

## D8 — Web stays on the dashboard page

Four real reminder tiles plus a "提醒记录" list inside the existing dashboard panel; no new route page, no new web dependencies.

## D9 — Revoke finalizes scheduled deliveries as suppressed (2026-08-20, user decision)

On revoke (complete/delete/reschedule), every delivery that has not started delivering is finalized as `suppressed` with the caller's reason (`todo_completed` / `todo_deleted` / `version_stale`) immediately, inside the caller's ambient transaction — atomic with the todo transition. The reason crosses the seam as a string and is validated by `domain.NewSuppressionReason` before any store mutation. Suppression runs after `RevokePlanned` and the best-effort cancels, so `PlannedJobIDs` still sees the scheduled rows' job IDs to cancel, and a cancel error never skips suppression. **Sending rows are not touched and the execution-time re-read stays the correctness boundary for jobs already in flight.** This supersedes the O2 smoke adaptation: step 7 asserts the literal `state='suppressed' and suppression_reason='todo_completed'` for the completed todo instead of the prior "no row outside (scheduled, suppressed)" adaptation.

## Working assumptions

The plan's accepted assumptions register:

- **A1** The `ChannelsProvider` is wired for real in ITER-0003 (nil in ITER-0002); plans created afterwards carry actual enabled+verified channel snapshots.
- **A2** Fake adapters run with zero delay and no failure injection in wired defaults; knobs exist only for tests.
- **A3** SMTP has no receipt channel this iteration; SMS receipts arrive via the generic webhook with fixtures — no MNS/AES integration.
- **A4** The receipt webhook authenticates with a single shared HMAC-SHA256 secret; per-provider key management is deferred.
- **A5** `ReminderSendArgs` JSON shape is the River job payload contract; payload evolution rides on the `reminder_send` kind.
- **A6** Ops "oldest wait" = `now - min(scheduled_at)` across active River jobs (`available|scheduled|retryable|running`), clamped at 0; ops data is instance-wide, not workspace-scoped.
- **A7** The worker-restart smoke step stands in for crash recovery; kill-mid-flight semantics are covered by the River adapter's stop/restart integration case (Task 8f).
- **A8** Defaults: queue concurrency 5/5 (compose overrides 2/2), `MaxAttempts` 5, retry backoff `min(500ms·2^(n−1), 60s)`.
- **A9** `provider_job_id bigint` matches `river_job.id`; job IDs travel as int64 through the port.
- **A10** `reminder.fake_outbox` stores rendered message bodies in plaintext (dev-only table; same exception class as identity's fake outbox).
- **A11** Receipt lookup by `provider_message_id` is the single documented unscoped read (D6).

## Outcomes

D1–D8 all held without revision, and every accepted assumption (A1–A11) survived implementation. Two adaptations were needed where the plan met the pinned River release and the smoke harness:

### O1 — River v0.44.0 module layout and API shape (Tasks 2, 8)

The plan assumed a standalone `github.com/riverqueue/riverpgxv5` module and a `JobCancelByID` call. River v0.44.0 nests the pgx v5 driver inside the main repository, so the direct dependencies recorded in go.mod are `github.com/riverqueue/river`, `github.com/riverqueue/river/riverdriver/riverpgxv5`, and `github.com/riverqueue/river/rivertype` — three module paths from the two sanctioned upstream repositories (still exactly the ADR-0002 dependency surface, `riverpgxv5` included). Cancellation uses the v0.44.0 `client.JobCancel(ctx, jobID)` call, and the capped exponential backoff is wired through the worker's `NextRetry(job *river.Job[dto.ReminderSendArgs]) time.Time` hook built on the tested `NextRetryDelay(attempt)` series. Migration 006 inlines the v0.44.0 `rivermigrate/main` SQL with the version in its header, so the queue version is pinned in the schema too.

### O2 — Smoke step-7 suppression assertion (Task 15)

Plan step 7 of the reminder smoke block asserted that the completed todo's delivery rows all reach `suppressed`. In practice revoke's best-effort `JobCancel` cancels the scheduled River jobs before they run, and a cancelled job never executes — so its delivery row legitimately remains `scheduled` forever; `suppressed` is only written when a job does run and the execution-time re-read suppresses it. The smoke step therefore asserts the business contract instead: after the due instant, no row for the completed todo is outside `{scheduled, suppressed}`, and no fake-outbox message containing the suppression todo's text ever appears for either address. Kill-mid-flight semantics remain covered by the River adapter's stop/restart integration case (A7).

The unified verification sequence (`corepack pnpm install --frozen-lockfile`, `make verify`, `make migration-test`, `make smoke-test`) was green at the branch tip when the ledger was finalized; the independent clean-context regression is the remaining gate.
