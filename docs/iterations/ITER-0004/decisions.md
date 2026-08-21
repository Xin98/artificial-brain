# ITER-0004 decisions

These choices are specific to private deployment and data portability. The brainstorming decisions P1–P10 are recorded in the [iteration design](../../superpowers/specs/2026-08-21-iter-0004-private-deployment-and-data-portability-design.md) §1 table; broader architecture remains governed by the [MVP design](../../superpowers/specs/2026-08-13-ai-native-personal-workbench-mvp-design.md) and the [iteration design](../../superpowers/specs/2026-08-21-iter-0004-private-deployment-and-data-portability-design.md).

## D1 — Portability via consumer-owned ports + cmd shims

Same seam shape as ITER-0003's `reminderPlannerShim`: portability defines narrow ports; `cmd/api` adapts Todo/Identity/Reminder public handlers. Rejected: direct cross-context application imports (allowed by policy but couples the context) and internal HTTP/events (MVP §6.1).

## D2 — Bundle streams; manifest last

`archive.Writer` tees each entry through sha256 while encoding; counts and hashes land in `manifest.json` written as the final zip entry. No disk staging, no temp files.

## D3 — Import stores the validated bundle as bytea

Import stores the validated bundle as bytea in `portability_imports` (≤ 32 MB default) so any API replica can confirm; confirm re-parses and re-decides from the stored bytes — preview never goes stale, execution happens exactly once (`state=committed`; a second confirm returns 409 `import_conflict` carrying the stored report).

## D4 — Delivery history imports with NULL plan_id

Migration 008 drops the NOT NULL on `reminder.reminder_deliveries.plan_id` (FK kept, nullable); imported rows carry `origin='imported'` and idempotency key `import:<sourceInstanceId>:<sourceRecordId>`. The plan-time insert path is untouched.

## D5 — Fixed admin is provisioned, not generated

`PRIVATE_ADMIN_PHONE` is required in private mode; the API provisions the admin user + workspace idempotently at startup through Identity's public `ProvisionAdmin` command. Rejected: log-printed generated phones (losing the log loses the admin).

## D6 — Login gate sits in the application layer

Both login handlers carry `PrivateAdminPhone`; when non-empty, any other phone fails with `domain.ErrRegistrationClosed` → HTTP 403 `registration_closed`. The web login flow is untouched (P1).

## D7 — Export seams are dedicated export handlers

(`ExportTodos`, `ExportChannels`, `ExportDeliveries`) returning export DTOs, so `TodoStore.List` keeps its "never deleted" contract and the reminder delivery scan gains `origin` only on the export path.

## D8 — No reverse proxy in the box

(P3): deploy/private documents the enterprise-proxy hookup and the LAN exposure risk; `deploy/AGENTS.md` records the deviation from master design §9.2.

## D9 — Backup/restore are operator scripts with a CONFIRM gate

Smoke-tested against the smoke compose project via env-parameterized project/credentials; `make backup`/`make restore` wrap them for the default project.

## D10 — Upgrade drill is same-architecture

Smoke writes data, recreates the stack (migrate re-runs idempotently), and proves data + health intact. Cross-version drills await the first release.

## Working assumptions

## Outcomes
