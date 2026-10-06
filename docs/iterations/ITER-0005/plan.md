# ITER-0005 execution plan

Execute the approved [implementation plan](../../superpowers/plans/2026-09-27-iter-0005-conversation-sessions.md) in its numbered tasks. Each task records red, green, verification, scope changes, and its commit in this ledger ([progress.md](progress.md)).

Task 1 establishes the branch, these ledger files, and the ITER-0005 zones in the `AGENTS.md` files. Follow the [approved design](../../superpowers/specs/2026-09-27-iter-0005-conversation-sessions-design.md); session/chat behavior enters only from Task 2 onward.

## Yellow-zone register

### User experience remediation (2026-10-06)

The user authorized fixing every issue in the user-path audit. The remediation preserves module ownership, existing dependencies, CI gates, and migrations 001–010. Additional deliberately reviewed yellow changes: additive conversation/todo/identity/portability OpenAPI fields and pagination; composition-root wiring of conversation history/export/import application ports and completion logging; server session-result distinction and safe return navigation; additive globals.css rules for responsive chat/navigation and accessible feedback. No new database migration is planned; transcript outcomes are appended using existing message rows and conversation portability uses the existing session schema.

Acceptance: missing reminder channels never masquerade as scheduled delivery; deletes identify the target; completed operations and complete query results survive history replay; session mutations/history loads have actionable failures and retries; chat supports multiline text, pending messages, draft/session restoration and safe formatted replies; todos can be inspected/edited and creation preserves filters without duplicate-submit confusion; dashboard links carry their filters and data can be refreshed; login supports resend, account/logout, return navigation and distinguishes outages from expiry; bundles export/import conversation history compatibly; navigation/state wording is Chinese; session/history paging and mobile layouts preserve access to chat.

Execution ledger: [UX remediation plan](../../superpowers/plans/2026-10-06-user-experience-remediation.md).

Portability source identities use a SHA-256 namespace of workspace, user, and source instance. Conversation fingerprints and target IDs use the `portability.history:` namespace in `public.instance_meta` because migration 008 freezes the source-record kind constraint. Upload ownership is stored atomically with the bundle using one SQL CTE in the same metadata table; unbound legacy pending uploads must be uploaded again. Same-workspace legacy source records produce an explicit ownership conflict, while different workspaces restore independently. Confirm commits restored rows, source metadata, and the report in the same unit of work. No transcript bodies are stored in metadata.

Final review additionally requires server-side `completedSince` filtering before the todo list limit, and channel enabled-only updates that cannot overwrite verification/resend state. The existing regression report's inline database credentials are replaced with a placeholder so the unchanged credential gate can run.

| UX yellow item | Files / boundary | Deliberate handling |
| --- | --- | --- |
| Completion, paging, delivery truth | `contracts/openapi/conversation.yaml`, `todo.yaml`, module application ports and HTTP adapters | Additive metadata and optional request fields; preserve old clients and transaction ownership. |
| Contact verification recovery | `contracts/openapi/identity.yaml`, identity command/HTTP ports, `backend/cmd/api/wiring.go` | Owner-scoped resend, 60-second cooldown, locking verification and resend; reuse sender and TTL. |
| Conversation portability | `contracts/openapi/portability.yaml`, public passive conversation import/export seams, `backend/cmd/api/portability_conversation.go` | Schema 2 with schema 1 reader; owner-scoped identities in existing instance metadata (no changes to frozen source-kind CHECK); no provider/action replay. |
| Authentication continuity | `apps/web/src/proxy.ts`, workbench layout and shared server session result | Proxy overwrites the return-path header from the request URL; strict internal return-path validation; outages do not imply expired credentials. |
| Responsive UI | `apps/web/src/app/globals.css` | Additive rules; preserve selector gate, visible keyboard focus, reduced motion, Chinese navigation and narrow-screen access. |

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
