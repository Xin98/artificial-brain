# Deployment guidance

## Zones

- **Green:** add deployment assets required by the approved runnable skeleton.
- **Yellow:** Compose topology, migrations, image/toolchain pins, CI, and root build targets need explicit planned review.
- **Red:** never place real secrets in deployment files, and never make API or Worker perform schema migration.

## Dependencies and verification

Deployment composes the Web, API, Worker, PostgreSQL, and one-shot migrate process without reversing application dependencies. Run `make toolchain-check` and `make harness-test`; run `make migration-test`, `make smoke-test`, and `make verify` when later tasks introduce them.
