# ITER-0005 execution plan

Execute the approved [implementation plan](../../superpowers/plans/2026-09-27-iter-0005-conversation-sessions.md) in its numbered tasks. Each task records red, green, verification, scope changes, and its commit in this ledger ([progress.md](progress.md)).

Task 1 establishes the branch, these ledger files, and the ITER-0005 zones in the `AGENTS.md` files. Follow the [approved design](../../superpowers/specs/2026-09-27-iter-0005-conversation-sessions-design.md); session/chat behavior enters only from Task 2 onward.

## Yellow-zone register

Every yellow-zone change in this iteration is listed here before it lands, per the repository policy:

| Item | Files | Task |
| --- | --- | --- |
| Policy files (zone refresh) | root `AGENTS.md`, `backend/AGENTS.md`, `apps/web/AGENTS.md`, `deploy/AGENTS.md` | 1 |
| Migration 010 + schema 9→10 pins | `deploy/migrations/010_conversation_sessions.sql`; `backend/internal/platform/database/schema.go`; `backend/internal/platform/database/migrate_integration_test.go`; `tests/smoke/migration_test.sh` | 2 |
| Platform config + env surface | `backend/internal/platform/config/config.go` + `config_iter0005_test.go`; `.env.example`; `compose.yaml` | 4 |
| Composition root | `backend/cmd/api/wiring.go` (+ `composition_integration_test.go`) | 5 |
| Public contract | `contracts/openapi/conversation.yaml`; `tests/contract/conversation_contract_test.go` | 6 |
| Web styling contract | `apps/web/src/app/globals.css` (additive only; house gate `globals-css.test.ts`) | 8 |
| Smoke gates | `tests/smoke/stack_test.sh` | 9 |

Red-zone compliance for this iteration: migrations 001–009 stay byte-untouched (`git diff --name-only deploy/migrations/` shows only `010_conversation_sessions.sql`); the OpenAI-compatible adapter is never called from CI (unit tests use `httptest`; smoke and composition run `MODEL_ADAPTER=deterministic`); no credentials are committed (`scripts/check-secrets.sh` inside `make verify`); no CI gate is lowered; `go.mod`, `package.json`, and `pnpm-lock.yaml` show zero dependency changes.
