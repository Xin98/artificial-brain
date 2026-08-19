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
