# User experience remediation implementation plan

**Goal:** Fix the complete user-path audit authorized on 2026-10-06, including trust, continuity, recoverability, portability, and mobile usability.

**Architecture:** Keep inbound adapters → application → domain and outbound implementations of ports. Extend existing module contracts additively; concrete cross-module composition stays in cmd. Use existing transcript rows rather than introducing a migration. Preserve backward compatibility for old bundles and clients.

**Constraints:** No new Go/web dependencies, no real providers in tests/CI, no credentials, no weakened gates, migrations 001–010 unchanged. Preserve the user's untracked manual-verify files. Work in the current checkout. Register yellow changes in ITER-0005/plan.md before implementation.

- [x] Task 1 — Conversation backend: expose actual reminder scheduling status and target details; persist full query summaries and completed-delete outcomes; paginate sessions/history using scoped stores. Verify with focused Go command/query/HTTP tests and contracts.
- [x] Task 2 — Workbench flows: todo edit/details/local time correction, form reset/success/filter continuity; dashboard deep links and refresh/actionable Chinese reminder status; login resend/safe return/error distinctions and account/logout; channel validation/busy/retry/verification guidance. Verify with focused Vitest flows and type/lint checks.
- [x] Task 3 — Portability: include full session/message history in versioned bundles, read older bundles, restore scoped sessions idempotently through public application ports; readable preview decisions and cancel/restart import controls. Verify archive roundtrip, command import/export/tenant boundaries and web import tests.
- [x] Task 4 — Conversation web: pending message, multiline safe formatted text, explicit target/scheduling feedback, reliable retry, draft/session persistence, loading/error states, activity ordering, paging, mobile collapse. Verify async response/session-switch races and history recovery in Vitest.
- [x] Task 5 — Integration: reconcile additive OpenAPI and parser contracts, composition wiring, CSS selector gate, Chinese navigation, server outage/session distinction; ensure all audit items are covered. Run toolchain/harness, Go suite/race/vet/build, web format/lint/test/build, architecture/contract tests, then browser visual verification if a local preview can run. Report unavailable Docker/DB gates explicitly.

Review focus: a delayed request must never change another session's active state; a timeout must not encourage duplicate writes; history pagination must not drop the boundary message; import retries must not duplicate messages or cross tenants; service outages must not be presented as invalid credentials; edits must retain local time and optimistic version protection.

Execution uses focused parallel agents for independent module ownership, with parent integration and whole-change review. Tests precede behavior changes; run each relevant failing regression then verify the implementation and wider suites.

## Execution evidence — 2026-10-06

All five implementation tasks are complete in the working checkout. Fresh review reproduced and fixed four additional issues: old-history responses replacing live turns, saved-session restoration interrupting the first send, recent completions disappearing behind the 200-row cap, and channel toggles overwriting a freshly rotated verification code. Deferred-response and controlled-interleaving regressions now pass. Browser review also corrected the fixed-height session button that overlapped its title and timestamp.

Passed verification:

- Direct toolchain script: Go 1.26.5, Node 24.18.0, pnpm 11.19.0.
- `go test ./... -race`, `go vet ./...`, and API/worker/migrate build; architecture and OpenAPI/export-schema contracts included.
- Web ESLint and TypeScript, all 198 Vitest tests in 27 files, and final production build.
- Go formatting, direct Prettier checks using the same repository targets, and `git diff --check`.
- Unchanged credential scanner against a temporary candidate index containing the final tracked changes and new source files. The real user index was left untouched. An existing database URL in the old iteration regression report was sanitized; the old real-index blob still triggers the scanner until that fix is staged.
- Actual browser interactions against the production web build using a local deterministic fixture: login challenge/countdown, dashboard empty state and links, multiline chat/pending reply, safe formatted reply, saved-session restoration, phone sidebar expand/collapse. At a 390px viewport the body measured 375px, with no horizontal overflow. After the CSS correction, title and timestamp bounding boxes no longer overlap. Local screenshot: `.artifacts/ux-mobile-verified.jpg`.

Environment limits, not waived gates:

- `make` is unavailable; the required scripts and equivalent commands were invoked directly. The existing harness runs but fails at its newline-in-filename fixture on Windows (`path with\nnewline.txt`); its policy checks were not changed.
- The root pnpm formatting script has a Windows cmd single-quote glob incompatibility. The same Prettier targets were checked directly and passed; package scripts were not changed.
- Docker's Linux engine pipe is unavailable and `TEST_DATABASE_URL` is unset. PostgreSQL-backed integration cases skip; migration, composition/database, and full stack smoke verification still require a running database/Docker environment. Browser fixture verification does not claim real provider or database coverage.

No dependencies, migrations, CI gates, or repository policy files changed. The user's untracked `manual-verify/` directory remains untouched. No changes were committed or deployed.
