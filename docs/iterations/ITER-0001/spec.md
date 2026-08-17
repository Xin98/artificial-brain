# ITER-0001 executable specification

The [approved design](../../superpowers/specs/2026-08-13-iter-0001-runnable-skeleton-design.md) defines the system. This iteration supplies a vertically runnable Web → API → PostgreSQL health path, a Worker heartbeat lease, and a one-shot migration process. The API routes are limited to `/health/live`, `/health/ready`, and `/api/v1/system/health`; the internal Worker routes are limited to `/health/live` and `/health/ready`.

Tooling is pinned to Go 1.26.5, Node.js 24.18.0, and pnpm 11.19.0. API and Worker may verify schema compatibility but only migrate changes schema. Health statuses are `healthy`, `degraded`, and `unavailable`; timestamps are UTC RFC3339.

Acceptance criteria are copied in [brief.md](brief.md). The delivery steps are in the [implementation plan](../../superpowers/plans/2026-08-13-iter-0001-runnable-skeleton.md).
