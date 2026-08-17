# ITER-0001 clean-context regression report

Reviewer: independent clean-context regression Agent.
Review date: 2026-08-14.
Implementation HEAD under review: `760e4c6a51b01124f05cec49a52b95135e5bcfe9` (branch `iter-0001-runnable-skeleton`).

This report fulfils acceptance criterion 10 of the [brief](brief.md) and closes the deferred "Clean-context regression" row of the [test matrix](test-matrix.md). The governing documents are the [brief](brief.md), the [design](../../superpowers/specs/2026-08-13-iter-0001-runnable-skeleton-design.md), and the [implementation plan](../../superpowers/plans/2026-08-13-iter-0001-runnable-skeleton.md).

## Environment

| Tool | Version |
| --- | --- |
| Go | 1.26.5 (darwin/arm64) |
| Node.js | 24.18.0 |
| pnpm | 11.19.0 (activated via corepack; not pre-installed on PATH) |
| Docker client / server | 29.7.2 / 29.5.2 |
| Docker Compose | 5.4.0 |
| Colima | 0.10.3 |
| `curl`, `jq`, Ruby | 8.7.1, 1.7.1, 2.6.10 |

The Docker engine was not running at session start. Colima was started with an isolated home so Docker-dependent gates could not touch ambient state or project/port variables, matching the implementation handoff:

- `COLIMA_HOME=/private/tmp/artificial-brain-colima`
- `DOCKER_CONFIG=/private/tmp/artificial-brain-docker` (provides the Compose v2 plugin symlink)

Docker-dependent commands (`make migration-test`, `make smoke-test`) ran with both variables exported. `make verify` is Docker-free and ran without them.

## Commands executed (in order)

```
corepack prepare pnpm@11.19.0 --activate && corepack enable   # pnpm not on PATH at start
sh scripts/check-toolchain.sh                                  # exit 0
CI=true corepack pnpm install --frozen-lockfile                # exit 0
CI=true make verify                                            # exit 0
CI=true COLIMA_HOME=… DOCKER_CONFIG=… make migration-test      # exit 0
CI=true COLIMA_HOME=… DOCKER_CONFIG=… make smoke-test          # exit 0
```

All commands exited 0. No gate was skipped or partially run.

## Gate evidence

### 1. `make verify` — exit 0

Docker-free, read-only pre-commit gate. Sub-targets resolved in recipe order: `harness-test`, `format-check`, `lint`, `architecture-test`, `test`, `build`, then `scripts/check-secrets.sh`.

- Repository policy (`harness-test`): clean; rejects tracked `.DS_Store` and `.env`.
- Format check: `prettier --check` clean across repo and Web; `gofmt` clean.
- Lint: `eslint . && tsc --noEmit` clean.
- Architecture: `architecture/policy` and `architecture/tests` pass; dependency-direction enforcement holds (positive and negative fixtures plus the real tree).
- Go tests (`-race`): 14 `ok` packages including `backend/cmd/worker`, all platform packages, `tests/contract`, and `tests/harness`. No failures.
- Web tests: `vitest run` — 2 files, 17 tests passed.
- Build: `go build` of all three binaries; `next build` compiled successfully and emitted standalone output with server-rendered `/` and `/health/live`.
- Secrets: `scripts/check-secrets.sh` exited 0 (silent on success).

### 2. `make migration-test` — exit 0

Isolated PostgreSQL via a unique Compose project; bounded by a Ruby deadline.

- API and Worker images built from pinned, non-root Dockerfiles; the migrate command built and ran twice, each invocation logging `schema_version: 1`, proving idempotent migration execution.
- `go test ./backend/internal/platform/database ./backend/internal/platform/workerstatus -race -v` ran inside the backend-test container against live PostgreSQL: 12 `--- PASS` cases, including `TestRunMigrationsTwice`, schema acceptance/rejection (`TestRequireSchemaAcceptsExpectedVersion`, `TestRequireSchemaRejectsMissingSchema`, `TestRequireSchemaRejectsMismatchWithoutLeakingURL`), and the full heartbeat lifecycle under `-race`.
- Migration ownership holds: only `migrate` invokes `RunMigrations`; API and Worker verify schema compatibility and fail safely against an empty schema.

### 3. `make smoke-test` — exit 0

Complete Compose proof (701 log lines). The script runs under `set -e`, so exit 0 means every assertion passed.

- Image proof: pinned non-root backend images (CA bundle, non-root UID 10001) and pinned standalone Web image (Node pin, frozen install, static copy, non-root runtime).
- Lockfile: supply-chain policy passed (496 entries).
- Web build inside the image: Next.js 16.2.12 standalone output emitted.
- Compose topology: ordered, private network; Worker health port and PostgreSQL private; Web starts only after API readiness; read-only mounted migrations.
- Lifecycle against Docker-assigned ports:
  1. PostgreSQL healthy → migrate runs to completion and exits → API and Worker start → API healthy → Web healthy.
  2. Worker stopped → API system health degrades on the expired heartbeat lease → Web renders the degraded four-card state.
  3. Worker restarted → health returns to healthy.
- Redaction: credential forms in logs and responses are redacted; the smoke redaction self-test path is exercised.

## Acceptance-criteria check

| # | Criterion (from brief) | Verdict | Evidence |
| --- | --- | --- | --- |
| 1 | `make dev` starts the stack without manual table creation | Pass | `make smoke-test` brings the full stack healthy with migrate owning schema. |
| 2 | Web status page shows Web/API/Worker/PostgreSQL and renders degraded state | Pass | Smoke proves the four-card healthy and degraded Web render. |
| 3 | API, Worker, and migrate responsibilities separated; startup does not modify schema | Pass | `make migration-test` proves only migrate migrates; API/Worker verify compatibility. |
| 4 | After abnormal Worker exit, health degrades within the lease interval | Pass | Smoke worker-stop step drives API/Web to degraded deterministically. |
| 5 | `make verify` passes in a clean checkout | Pass | `CI=true make verify` exited 0 in this clean context. |
| 6 | Compose smoke tests pass in bounded time and prove degradation after Worker pause | Pass | `CI=true make smoke-test` exited 0 within the Ruby deadline. |
| 7 | Architecture tests reject reverse-layer and cross-context imports | Pass | `make architecture-test` (part of `verify`) passed with negative fixtures. |
| 8 | No real secrets; logs and errors do not leak credentials | Pass | `check-secrets.sh` exited 0; smoke redaction path exercised; config tests prove no secret echo. |
| 9 | Plan, test matrix, progress, and handoff let a new Agent continue | Pass | This reviewer used only those documents and the commit to execute regression. |
| 10 | Independent clean-context regression produces a passing report | Pass | This document. |

## Outcome

ITER-0001 passes independent clean-context regression. The deferred test-matrix row is now satisfied. No defects, gaps, or unresolved issues were found. The implementation is regression-complete and ready for integration.
