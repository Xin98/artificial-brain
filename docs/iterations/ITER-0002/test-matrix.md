# ITER-0002 test matrix

Rows are recorded as each task reaches green. IDs group by subsystem: `IDM` identity domain/application, `AUT` auth/session/settings HTTP, `TDO` todo, `RMD` reminder seam, `CNV` conversation, `WEB` web features, `CNT` contracts, `MIG` migrations/schema, `OPS` compose/smoke/CI.

| Requirement | Command and evidence | Evidence commit | Status |
| --- | --- | --- | --- |
| MIG-01 — schema advances 1→5 via append-only migrations | `make migration-test`; empty-schema gate, idempotent re-run, and `schema_version=5` assertion | `8bd87b7`, re-verified at `ae91592` | Complete |
| IDM-01 — identity domain invariants and application rules | `go test ./backend/internal/modules/identity/... -race` | `a2b26c3`, `f5db3ff` | Complete |
| AUT-01 — login round-trip, session cookie, and gated dev inbox | `TEST_DATABASE_URL=… go test -p=1 ./backend/cmd/api ./backend/internal/modules/identity/adapters/inbound/http -race` | `f5db3ff` | Complete |
| TDO-01 — todo lifecycle, filters, and dashboard queries | `go test ./backend/internal/modules/todo/... -race` | `e5f66b0` | Complete |
| RMD-01 — reminder plan seam with noop scheduler | `go test ./backend/internal/modules/reminder/... -race` plus `TEST_DATABASE_URL=…` postgres adapter run | `3add142` | Complete |
| TDO-02 — todo postgres adapter and atomic reminder seam | `TEST_DATABASE_URL=… go test -p=1 ./backend/internal/modules/todo/adapters/outbound/postgres ./backend/cmd/api -race` | `88d4f2f`, `e9bce3d` | Complete |
| CNV-01 — intent validation, router, clarification, and confirmation | `go test ./backend/internal/modules/conversation/... -race` | `138549d` | Complete |
| CNV-02 — deterministic corpus and OpenAI-compatible model adapters | `go test ./backend/internal/modules/conversation/adapters/outbound/... -race` | `d22e5bd`, `c8a2e94` | Complete |
| CNT-01 — identity/todo/conversation/dashboard OpenAPI contracts | `go test ./tests/contract -race` | `ab26a4d` | Complete |
| WEB-01 — web auth, dashboard, todos, settings, and conversation features | `corepack pnpm --filter @artificial-brain/web test` | `3e813af` | Complete |
| Dependency direction | `make architecture-test`; fixtures and the real populated module tree enforce import and deployment-reference policy | `3e813af`, `ab26a4d` | Complete |
| OPS-01 — authenticated end-to-end smoke loop | `make smoke-test`; login via dev inbox, create todo, conversation intent creates todo + reminder plan, confirmation-gated delete | `ae91592` | Complete |
| Clean-context regression | `make verify && make migration-test && make smoke-test`; immutable reviewer report is `regression-report.md` | | Pending |
