# Investment implementation progress

Execution base: `c26fcec0370a905357efb18abdf7f9a320a52450`.
Branch: `codex/investment-us-equities`, managed isolated worktree.
Approved specification and 19-task plan: `docs/superpowers/specs/2026-10-07-investment-us-equities-design.md`, `docs/superpowers/plans/2026-10-07-investment-us-equities.md`.

## Environment and decisions

- Existing dependencies installed with frozen lockfile; no dependency additions.
- Git Bash is available; use the original scripts because make is absent. Toolchain script passes; the subsequent harness intentionally checks incompatible versions before reaching its failing newline fixture. Harness exits at a newline filename unsupported on Windows. Architecture baseline passes.
- Ruling: continue implementation with the Windows harness failure recorded and retain all gates — the failure is an existing filesystem fixture, unrelated to investment — cost if wrong: integration defects must still be caught by targeted tests and final smoke.
- Docker Desktop started hidden to make database acceptance possible; availability pending.
- Pre-flight: Tasks 1→5→11→14 share exact money and account types; Tasks 2→3→4→8→13 share point-in-time snapshot; Tasks 6→15→16 share views; Tasks 10→11→13→14 share risk/reservation rules. Use declared owned ports and keep transaction boundaries in application. No unresolved interface conflicts.

## Tasks

Task 1 RED: domain tests fail on undefined exact money/account interfaces. GREEN: all 5 domain tests pass; architecture tests pass. Toolchain passes; harness fails at the existing Windows newline filename fixture.
Task 2 RED: missing snapshot/calendar/fixture APIs. GREEN: 9 domain+fixture tests pass; architecture passes. Embedded deterministic synthetic data includes 31 instruments, 2023–2026 bars, accession facts, amendments, missing/stale/finance candidates, split/dividends, gap and halted-bar cases. Docker Linux engine now available.
Task 3 RED: missing TTM/valuation/indicator APIs. GREEN: all 14 domain tests pass. Cumulative quarters are differenced before TTM; exact rational finances feed ratios; nonpositive denominators have reasons; RSI boundary and drawdown checks pass.
Task 4 RED: missing ranking/evaluation APIs. GREEN: 19 domain tests and 3 fixture tests pass, including full reproducible evaluation, ineligible exclusions, industry lookup and missing-risk states; architecture passes.
Ruling: update the pre-existing ConversationSessionsEndToEnd assertion to the current due-time and missing-reminder-channel wording — its old September assertion predates the UX change in the execution base, while the current application unit tests assert the new behavior — cost if wrong: this integration assertion would need revision; production conversation behavior is unchanged. RED observed twice with a real database before updating the assertion.
Ruling: use an isolated Linux verifier with curl connect-to for Docker Desktop dynamic published ports — native Ruby lacks process groups, and measured curl requests show dynamic ports reachable through the Windows host gateway but not verifier loopback; connect-to preserves URLs, Host headers and cookies — cost if wrong: transport differences could mask host routing behavior, so local app checks remain separate. No repository gate or CI script is changed. Linux toolchain and harness baseline passed in a separate ext4 checkout.
Task 5 RED: missing store constructors. GREEN: real PostgreSQL schema/owner/version/rollback/snapshot tests and explicit 10→11/repeated upgrade tests pass without skips; API/database suites pass; migration 001–010 unchanged. Full migration gate running in Linux, pending completion.
Ruling: commit verified persistence and continue dependent implementation while the full migration runner downloads its Linux build toolchain — typed stores and direct PostgreSQL upgrade tests already pass; the outstanding full gate remains mandatory before final acceptance — cost if wrong: a composition defect may be discovered later and require correction before delivery.
