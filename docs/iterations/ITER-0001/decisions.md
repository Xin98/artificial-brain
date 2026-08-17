# ITER-0001 decisions

These choices are specific to the runnable skeleton. Broader architecture remains governed by the [approved design](../../superpowers/specs/2026-08-13-iter-0001-runnable-skeleton-design.md).

## Database heartbeat lease for Worker status

Worker publishes an expiring heartbeat lease in PostgreSQL. API reads that lease to classify Worker health, while Web depends only on API. Normal shutdown removes the lease on a bounded cleanup context; abnormal shutdown becomes unavailable when the lease expires. This avoids exposing the Worker's private health port and avoids permanently stale healthy state.

## System health outside business contexts

System health is platform behavior, not Identity, Todo, Conversation, Reminder, or Portability behavior. Its contract and implementation remain outside business modules so this iteration does not create a false domain owner or reverse the dependency direction.

## tern as the one-shot migration adapter

The migrate command is the sole schema owner and invokes tern once against the mounted, read-only migration directory. API and Worker check compatibility but never migrate. This keeps startup ownership explicit and makes failed migration a Compose ordering failure rather than an application side effect.
