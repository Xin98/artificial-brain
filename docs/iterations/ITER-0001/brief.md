# ITER-0001 brief

Purpose: establish a clean-context, vertically runnable Next.js Web, Go API, Go Worker, and PostgreSQL skeleton with reproducible verification evidence.

Scope is the health-delivery chain, independent migrations, architecture enforcement, CI-facing Make entry points, pinned toolchains, and iteration evidence. Out of scope: Identity, Todo, Conversation, Reminder, Portability, River, model SDKs, external providers, production cloud infrastructure, and fabricated business modules.

The governing [design](../../superpowers/specs/2026-08-13-iter-0001-runnable-skeleton-design.md) and [implementation plan](../../superpowers/plans/2026-08-13-iter-0001-runnable-skeleton.md) are authoritative.

## Acceptance criteria

1. New developers can use `make dev` to start the complete stack without manually creating tables.
2. The Web status page shows Web, API, Worker, and PostgreSQL, and it can render a degraded state when a dependency fails.
3. API, Worker, and migration process responsibilities are separated; application startup does not implicitly modify the Schema.
4. After an abnormal Worker exit, health becomes unavailable within the deterministic lease interval.
5. `make verify` passes in a clean checkout and covers formatting, static checks, architecture, tests, migrations, and builds.
6. Compose smoke tests pass within a bounded time and prove degradation after the Worker is paused.
7. Architecture tests reject at least one reverse-layer dependency and one cross-context internal import.
8. The repository has no real secrets, and logs and error responses do not leak database connection strings or environment values.
9. The ITER-0001 plan, test matrix, progress, and handoff evidence let a new Agent continue without reading the implementation conversation.
10. An independent clean-context regression Agent uses this specification and the commit difference to execute regression and produce a passing report.
