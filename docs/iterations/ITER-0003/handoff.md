# ITER-0003 handoff

ITER-0003 (reliable reminder delivery) is implemented on branch `iter-0003-reliable-reminder-delivery`, branched from `master` after ITER-0002's passing regression. The iteration ledger and the governing [design](../../superpowers/specs/2026-08-19-iter-0003-reliable-reminder-delivery-design.md) and [implementation plan](../../superpowers/plans/2026-08-19-iter-0003-reliable-reminder-delivery.md) are in place. Start with [brief.md](brief.md), [progress.md](progress.md), [decisions.md](decisions.md), and the [test matrix](test-matrix.md).

## Current state

Tasks 1–16 are implemented and verified; the independent clean-context regression returned PASS, and a user-mandated follow-up fix (decision D9) landed with a passing superseding regression. The implementation HEAD is `31425d4` (`fix: finalize scheduled deliveries as suppressed on revoke`); the chain on top of the task work is `8ef71d6` (`ci: finalize ITER-0003 verification evidence`), `2049071` (`docs: record ITER-0003 regression approval`), `592d3ee` (`docs: mark ITER-0003 complete after passing regression`), `31425d4` (D9 fix), and `fa139f6` (`docs: record superseding ITER-0003 regression approval`).

What is implemented: reminder plans fan out into one `scheduled` delivery row per enabled channel at plan time (same transaction, UNIQUE business idempotency key). The Worker runs a River reminder worker over two queues (`reminder_email`, `reminder_sms`); the API inserts jobs through the evolved `JobScheduler` port via `InsertTx` into the caller's ambient transaction and best-effort `JobCancel`s on revoke. Suppression is decided at execution time by re-reading Todo/Plan/Channel, so a todo completed or cleared before its due instant never delivers. Failed attempts retry with capped exponential backoff (`min(500ms·2^(n−1), 60s)`, `MaxAttempts` 5); exhausted jobs become business dead letters. Provider adapters: fake outbox (Compose default), stdlib SMTP, Aliyun SMS (HMAC-SHA1 RPC). Receipts are informational: a single shared-secret HMAC-SHA256 webhook (`POST /api/v1/webhooks/receipts/sms`) records evidence once per provider message ID without flipping terminal states. Ops (`GET /api/v1/ops/reminder`) is deterministic SQL over `river_job` + `reminder_deliveries`. The schema advanced 5→7 (River v1 SQL inlined in `006`, delivery tables in `007`). The dashboard shows four real reminder counters and a 提醒记录 list.

Useful URLs once the stack is up (`make dev`):

- `http://localhost:3000/` — session-gated dashboard, now with the reminder tiles and the 提醒记录 section
- `http://localhost:3000/login` — two-step phone + code login (codes come from `/api/v1/dev/sms-inbox?address=<phone>` in dev)
- `http://localhost:8080/api/v1/reminders` — session-gated reminder delivery records
- `http://localhost:8080/api/v1/ops/reminder` — session-gated reminder ops snapshot
- `http://localhost:8080/api/v1/webhooks/receipts/sms` — receipt webhook (no session cookie; `X-Receipt-Signature` HMAC-SHA256 over the body with `REMINDER_RECEIPT_SECRET`)
- `http://localhost:8080/api/v1/dev/reminder-outbox?address=<email-or-phone>` — gated dev outbox for fake-adapter reminder messages (non-production `APP_ENV` + `REMINDER_DEV_OUTBOX_ENABLED=true`)

## How to continue

1. Run the acceptance sequence exactly as CI does: `corepack pnpm install --frozen-lockfile`, `make verify`, `make migration-test`, `make smoke-test`. The full sequence was green at the branch tip; `make smoke-test` includes the reminder delivery/suppression/receipt/ops/worker-restart block and takes several minutes with Docker.
2. The independent clean-context regression follows [progress.md](progress.md) task 16 steps 3–5: a reviewer with only the master design, this iteration's brief/spec/plan/test-matrix/handoff, the merge-base…HEAD diff, and the README/Makefile commands re-runs the gates, maps the acceptance criteria to evidence, checks yellow/red zone compliance (only the River dependencies added; migrations 001–005 untouched; health contracts untouched; CI workflow unchanged; no real-provider egress in CI), scans for credential leakage (receipt secret, SMTP password, Aliyun keys), and writes the immutable `regression-report.md`. On FAIL, fixes land as fresh red→green `fix:` commits and a new reviewer supersedes the old report.
3. Respect the zones: business modules are green; the yellow register in the plan's ledger (River dependencies, migrations 006–007, platform config, cmd wiring, contracts, compose/smoke, README, `AGENTS.md` files) is deliberately handled; delivery, River, real providers, and Portability remain red until later iterations.

## Environment prerequisites

Go 1.26.5 (or newer 1.26 patch), Node.js 24.18.0, pnpm 11.19.0, Docker Compose v2, `curl`, `jq`, and Ruby. Integration tests need `TEST_DATABASE_URL` (this host uses the dedicated `ab-test-postgres` container, `postgres:18.4-alpine` on host port 5433; packages sharing that database run with `-p=1`). Docker-dependent gates (`make migration-test`, `make smoke-test`) need a running Docker engine. The verification sequence is `corepack pnpm install --frozen-lockfile`, `make verify`, `make migration-test`, `make smoke-test`. The River modules resolve through the Go module proxy; on egress-restricted hosts use `GOPROXY=https://goproxy.cn,direct` (plan Task 2 step 1).

### Verification environment on the current Linux host

Notes carried over from ITER-0002's host setup (unchanged):

- The pinned toolchain is installed natively: Go 1.26.5 at `/usr/local/go`, Node.js 24.18.0 at `/opt/node`, pnpm 11.19.0 via Corepack, Ruby via dnf.
- Container egress to Google IPs and `registry.npmjs.org` is blocked; the gitignored `compose.override.yaml` passes Aliyun/npmmirror build args (`GOPROXY`, `APK_MIRROR`, `NPM_REGISTRY`) and builds on host networking. Defaults elsewhere are unchanged.
- The host resolver injects `ndots:5` search suffixes into containers, breaking short-name resolution; the override uses the absolute `http://api.:8080` form for the web image (build arg and runtime env). The backend runtime image carries `tzdata` for per-request IANA zones.

The independent clean-context regression re-ran the full gate sequence and returned **PASS** (`regression-report.md`, `2049071`). After decision D9 (`31425d4`, revoke finalizes still-scheduled deliveries as `suppressed` with the caller's reason), a fresh superseding clean-context regression re-verified the whole iteration at the new HEAD and returned **PASS** (`regression-report-v2.md`, `fa139f6`). ITER-0003 is complete and ready for merge review.
