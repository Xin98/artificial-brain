# ITER-0002 handoff

ITER-0002 (Identity, Todo, and Conversation closed loop) is implemented on branch `iter-0002-identity-todo-conversation`, branched from `master` after ITER-0001's passing regression. The iteration ledger and the governing [design](../../superpowers/specs/2026-08-17-iter-0002-identity-todo-conversation-loop-design.md) and [implementation plan](../../superpowers/plans/2026-08-17-iter-0002-identity-todo-conversation-loop.md) are in place. Start with [brief.md](brief.md), [progress.md](progress.md), [decisions.md](decisions.md), and the [test matrix](test-matrix.md).

## Current state

Tasks 1–18 are implemented and verified except the independent clean-context regression, which is recorded in `regression-report.md` once that review passes. All business code lives behind the session gate: Identity (phone + code login, fake SMS outbox, gated dev inbox), Todo (full lifecycle, optimistic concurrency, dashboard), Reminder (plan seam with the no-op `JobScheduler`), and Conversation (strict proposal validation, deterministic + OpenAI-compatible adapters, confirmation-gated delete). The Web workbench (login, dashboard, todos, settings, conversation) talks to the API only through the `/api/v1` rewrite.

Useful URLs once the stack is up (`make dev`):

- `http://localhost:3000/` — session-gated dashboard (redirects to `/login` without a session)
- `http://localhost:3000/login` — two-step phone + code login (codes come from `/api/v1/dev/sms-inbox?address=<phone>` in dev)
- `http://localhost:3000/todos`, `/conversation`, `/settings` — workbench features
- `http://localhost:3000/status` — system health page (ITER-0001's front page)
- `http://localhost:8080/api/v1/...` — business API routes (see README)

## How to continue

1. Run the acceptance sequence exactly as CI does: `corepack pnpm install --frozen-lockfile`, `make verify`, `make migration-test`, `make smoke-test`.
2. The independent clean-context regression follows [progress.md](progress.md) task 18 steps 3–5: a reviewer with only the master design, this iteration's ledger documents, the merge-base…HEAD diff, and the README/Makefile commands re-runs the gates, maps acceptance criteria to evidence, checks zone compliance, scans for credential leakage, and writes the immutable `regression-report.md`. On FAIL, fixes land as fresh red→green `fix:` commits and a new reviewer supersedes the old report.
3. Respect the zones: business modules are green; migrations/schema, platform, cmd wiring, contracts, CI/harness lists, web shared/app seams, and `AGENTS.md` files are yellow and already deliberately handled (see the plan's register); delivery, River, real providers, and Portability remain red until later iterations.

## Environment prerequisites

Go 1.26.5 (or newer 1.26 patch), Node.js 24.18.0, pnpm 11.19.0, Docker Compose v2, `curl`, `jq`, and Ruby. Integration tests need `TEST_DATABASE_URL`; Docker-dependent gates (`make migration-test`, `make smoke-test`) need a running Docker engine. The verification sequence is `corepack pnpm install --frozen-lockfile`, `make verify`, `make migration-test`, `make smoke-test`.

### Verification environment on the current Linux host

Development moved from the Windows+WSL host (D9) to an Apsara Linux 3 machine (D10/D12). Notes for this host:

- The pinned toolchain is installed natively: Go 1.26.5 at `/usr/local/go`, Node.js 24.18.0 at `/opt/node`, pnpm 11.19.0 via Corepack, Ruby via dnf.
- Container egress to Google IPs and `registry.npmjs.org` is blocked; the gitignored `compose.override.yaml` passes Aliyun/npmmirror build args (`GOPROXY`, `APK_MIRROR`, `NPM_REGISTRY`) and builds on host networking. Defaults elsewhere are unchanged.
- The host resolver injects `ndots:5` search suffixes into containers, breaking short-name resolution; the override uses the absolute `http://api.:8080` form for the web image (build arg and runtime env). The backend runtime image carries `tzdata` for per-request IANA zones.
- Adapter integration tests use a dedicated `postgres:18.4-alpine` container (`ab-test-postgres`, host port 5433) via `TEST_DATABASE_URL`, and packages sharing that database run with `-p=1`.

The independent clean-context regression re-ran the full gate sequence at `81e969f` and returned **PASS**; its immutable record is [regression-report.md](regression-report.md). ITER-0002 is complete and ready for merge review.
