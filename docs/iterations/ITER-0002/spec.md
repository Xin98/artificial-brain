# ITER-0002 executable specification

The [approved design](../../superpowers/specs/2026-08-17-iter-0002-identity-todo-conversation-loop-design.md) defines the system for this iteration: phone + SMS-code cloud login over a fake SMS outbox, session-cookie authentication, a personal workspace with enforced isolation, the Todo aggregate with optimistic concurrency and confirmation-gated soft delete, a deterministic dashboard, and a Conversation context whose strictly-validated Intent Proposals route only registered intents to Todo's public application interface. A minimal Reminder scheduling seam (Reminder Plan + no-op JobScheduler) pre-warms ITER-0003 without delivery.

The API surface is the versioned route table in the design; errors reuse the stable `{code, message, correlationId}` envelope and `X-Correlation-ID`. Tooling remains pinned to Go 1.26.5, Node.js 24.18.0, and pnpm 11.19.0, with zero new dependencies. The schema advances from version 1 to 5 through append-only migrations owned solely by `backend/cmd/migrate`; API and Worker keep the equality schema gate. Health routes and the system-health contract are unchanged.

Acceptance criteria are copied in [brief.md](brief.md). The delivery steps are in the [implementation plan](../../superpowers/plans/2026-08-17-iter-0002-identity-todo-conversation-loop.md).
