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

The plan's accepted assumptions register:

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

## Outcomes

D1–D10 all held without revision, and every accepted assumption (A1–A10) survived implementation. Six adaptations were needed where the plan met implementation reality:

### O1 — Preview persistence and source-record target resolution (Tasks 9, 10)

Confirm's report must come from the stored bytes, and `GET imports/{id}` must return the preview without re-parsing the bundle, so migration 008 gained a `preview` jsonb column on `portability.portability_imports` (branch-local migration; the red zone covers 001–007 only). `SourceRecordStore` gained a third method `Targets(ctx, sourceInstanceID, ids)` — required by the plan's own "resolve via Sources" rule — and delivery source records store the D4 idempotency key `import:<instance>:<record>` as their `TargetID` because the frozen T7 seam returns no row id. `Imports.Commit` sits outside `UoW.Run` by design: a re-confirm self-heals through Source Identity.

### O2 — `registration_closed` maps to 403 on both login routes (Task 13)

Task 13's review found that identity HTTP returned 500 for `ErrRegistrationClosed` (the gate landed in the application layer, D6, but neither login route mapped the sentinel). The fix (`1aa697e`) maps `ErrRegistrationClosed` → 403 `registration_closed` on both login routes, with unit and composition coverage tightened.

### O3 — Confirm-report validator follows the contract's 7-key ImportReport (Task 15)

The web confirm-report validator accepts the contract's 7-key `ImportReport` shape (a strict 5-key validator would fail closed against the real backend); the TypeScript surface keeps the brief's 5-key projection. The `globals.css` edit was mandated by the house `globals-css.test.ts` gate and is styling-only on the existing palette.

### O4 — Private-mode env template must disable the dev surfaces (Task 16)

`deploy/private/env.template` carries `DEV_INBOX_ENABLED=false` and `REMINDER_DEV_OUTBOX_ENABLED=false`: compose's defaults are `true`, and `config.Load` fail-closes those in production (`APP_ENV=production`), so a private stack without the explicit disables would not boot.

### O5 — Three out-of-scope regression fixes restored the gate contracts (Task 17, `5b8b752`)

Tasks 11–16 deferred the Docker gates to Task 17, which surfaced three latent regressions; each fix is minimal, contract-preserving, and committed separately from the task files:

1. `backend/cmd/api/main.go` — the empty-schema fail-safe was restored: startup schema-verification failure no longer hard-exits (which broke the ITER-0001/0002 migration gate's empty-schema API probe); the API logs, serves degraded (readiness 503), and startup provisioning (instance identity + private admin) runs only when schema verification succeeds.
2. `backend/cmd/api/wiring.go` — the `channelImportShim` downgrade is transaction-clean: inside confirm's single UoW, a failed INSERT aborts the whole PostgreSQL transaction, so relying on `ErrChannelExists` from the insert 500'd every self-import confirm. The shim now pre-checks duplicates with a SELECT that joins the ambient transaction and returns the existing channel id plus the `ErrChannelExists` sentinel before any insert is attempted.
3. `backend/internal/modules/reminder/adapters/outbound/postgres/deliveries.go` — the historical `scanDelivery` scans the now-nullable `plan_id` into a `*string` (mirroring `scanDeliveryWithOrigin`), so `GET /api/v1/reminders` and every `List` path survive plan-less imported rows; regression coverage added.

Together with (2), this pins the **channel self-import downgrade contract**: duplicate channels resolve the existing channel id and downgrade to `skipped`, registered against the existing row — never a second channel row.

### O6 — Smoke self-import assertions follow the downgrade contract (Task 17)

The brief's wording "the copied channels appear unverified in the settings listing" cannot hold for a self-import (the unique `(user, kind, address)` constraint plus the T9 downgrade, O5). The portability smoke block asserts the T9 contract instead: report `new == counts.todos + counts.deliveries`, `skipped == counts.channels`, the downgraded channel's source record maps to the pre-existing channel id, and no duplicate row appears. The imported-into-a-fresh-workspace path (channels unverified by construction) is covered by `TestPortabilityRoundTripBetweenTwoWorkspaces`.

The unified verification sequence (`corepack pnpm install --frozen-lockfile`, `make verify`, `make migration-test`, `make smoke-test`) was green at the branch tip `4e94f7e` when the ledger was finalized; the independent clean-context regression is the remaining gate.
