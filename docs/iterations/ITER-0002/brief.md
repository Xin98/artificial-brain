# ITER-0002 brief

Purpose: deliver the Identity, Todo, and Conversation closed loop on the ITER-0001 skeleton — cloud phone + SMS-code login (fake adapters), a personal workspace, the full Todo lifecycle with confirmation-gated soft delete, a deterministic dashboard, and structured-intent conversation that routes only registered intents to Todo's public application interfaces. Notification delivery and reminder execution remain out of scope; a no-op scheduling seam pre-warms ITER-0003.

Scope is the login/settings/todo/dashboard/conversation chain, the reminder scheduling seam, versioned OpenAPI contracts, the schema bump, and iteration evidence. Out of scope: reminder delivery, River, real SMS/email providers, Portability, open-ended chat, MCP, and any dependency additions.

The governing [design](../../superpowers/specs/2026-08-17-iter-0002-identity-todo-conversation-loop-design.md) and [implementation plan](../../superpowers/plans/2026-08-17-iter-0002-identity-todo-conversation-loop.md) are authoritative. The three confirmed scope decisions (local cloud-mode login via a fake SMS inbox, the reminder scheduling seam, and both deterministic + OpenAI-compatible model adapters) are recorded in [decisions.md](decisions.md).

## Acceptance criteria

1. A user can request a login code for a phone number, verify it, and receive a session cookie that grants access to exactly one personal workspace.
2. Every business read and write is scoped by workspace and user; integration tests prove cross-workspace access is invisible.
3. A user can create, list, complete, and edit Todos through the same application commands; no-due Todos are legal; delete is soft, terminal, and requires confirmation.
4. The dashboard returns deterministic counts (pending, due-today, overdue, no-due, completed-last-7-days) with reminder retry/fail reported as zero.
5. A conversation message is parsed into a versioned, strictly-validated Intent Proposal and only registered intents reach Todo's public application interface; `todo.create` echoes the resolved absolute time.
6. Missing, ambiguous, or low-confidence input produces a clarification rather than a guess.
7. `todo.delete` requires candidate matching plus a one-time, TTL-bounded Confirmation Request bound to user, workspace, Todo, and Todo version; an unconfirmed delete never executes and natural-language bulk delete is rejected.
8. Prompt-injection content cannot alter the allowed intent list, delete confirmation, or permission rules.
9. A due-dated Todo creates a Reminder Plan through the JobScheduler seam (no-op adapter); completing, deleting, or rescheduling revokes outstanding plans; nothing is delivered.
10. `make verify` passes in a clean checkout with the schema bumped, the new module tree satisfying architecture policy, and zero new dependencies.
11. The Compose smoke test proves the authenticated end-to-end loop (login via dev inbox, create Todo, conversation intent creates a Todo + Reminder Plan, confirmation-gated delete).
12. The ITER-0002 plan, test matrix, progress, and handoff evidence let a new Agent continue without reading the implementation conversation, and an independent clean-context regression Agent produces a passing report.
