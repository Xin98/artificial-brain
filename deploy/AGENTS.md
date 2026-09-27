# Deployment guidance

## Zones

- **Green:** add deployment assets required by the approved runnable skeleton, including the ITER-0004 `deploy/private/**` assets (deployment handbook, env template, backup/restore scripts) and the backup-restore/upgrade runbooks.
- **Yellow:** Compose topology, migrations, image/toolchain pins, CI, and root build targets need explicit planned review; ITER-0005 adds append-only migration `010_conversation_sessions.sql` (schema 9→10) and `CONVERSATION_HISTORY_TURNS` compose env passthrough, registered in `docs/iterations/ITER-0005/plan.md`.
- **Red:** never place real secrets in deployment files, never make API or Worker perform schema migration, and never edit migrations 001–009 (frozen; ITER-0005 appends 010 only).

Note: the private stack deliberately ships no reverse proxy — TLS termination, LAN exposure risk, and the enterprise-gateway hookup are documented in `deploy/private/README.md` instead (master design §9.2 deviation, iteration decision D8).

## Dependencies and verification

Deployment composes the Web, API, Worker, PostgreSQL, and one-shot migrate process without reversing application dependencies. Run `make toolchain-check` and `make harness-test`; run `make migration-test`, `make smoke-test`, and `make verify` when later tasks introduce them.
