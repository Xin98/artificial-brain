# ITER-0003 handoff

ITER-0003 (reliable reminder delivery) is open on branch `iter-0003-reliable-reminder-delivery`, branched from `master` after ITER-0002's passing regression. The iteration ledger and the governing [design](../../superpowers/specs/2026-08-19-iter-0003-reliable-reminder-delivery-design.md) and [implementation plan](../../superpowers/plans/2026-08-19-iter-0003-reliable-reminder-delivery.md) are in place. Start with [brief.md](brief.md), [progress.md](progress.md), [decisions.md](decisions.md), and the [test matrix](test-matrix.md).

## Current state

Task 1 (ledger, policy refresh, branch) is establishing the iteration. No delivery code exists yet: the schema is at version 5, the `JobScheduler` port still carries the no-op adapter, the dashboard reminder counters are still ITER-0002's deterministic zeros, and the only new dependencies (River) are not yet in `go.mod`. Implementation proceeds task-by-task in the plan's order, red→green→refactor, committing each task's listed files only.

## How to continue

1. Read the [implementation plan](../../superpowers/plans/2026-08-19-iter-0003-reliable-reminder-delivery.md) global constraints and the task you are resuming.
2. Resume at the first non-Complete task in [progress.md](progress.md). Each task declares its files, interfaces, red→green steps, verification command, and commit message.
3. Respect the zones: green covers the business modules including the reminder delivery work (Identity, Todo, Conversation, Reminder) and their tests; yellow items are listed in the plan's register (River dependencies, migrations 006–007, platform config, cmd wiring, contracts, compose/smoke, README, `AGENTS.md` files). Red: no Portability behavior, no real-provider calls from CI (fake adapters only), no committed credentials, no lowered CI gates.
4. Update `progress.md` and `test-matrix.md` as each task reaches green.

## Environment prerequisites

Go 1.26.5 (or newer 1.26 patch), Node.js 24.18.0, pnpm 11.19.0, Docker Compose v2, `curl`, `jq`, and Ruby. Integration tests need `TEST_DATABASE_URL`; Docker-dependent gates (`make migration-test`, `make smoke-test`) need a running Docker engine. The verification sequence is `corepack pnpm install --frozen-lockfile`, `make verify`, `make migration-test`, `make smoke-test`. The River modules resolve through the Go module proxy; on egress-restricted hosts use `GOPROXY=https://goproxy.cn,direct` (plan Task 2 step 1).

There are no unresolved implementation gaps yet because implementation has just begun. `regression-report.md` will record the independent clean-context regression once implementation completes.
