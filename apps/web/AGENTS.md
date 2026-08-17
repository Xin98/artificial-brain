# Web guidance

## Zones

- **Green:** implement the server-rendered system-health feature and its tests under `apps/web`.
- **Yellow:** package boundaries, browser/server data boundaries, root workspace configuration, and public UI contracts require a planned review.
- **Red:** feature code must not import deployment configuration or expose Compose service names to the browser; do not add business-domain behavior in ITER-0001.

## Dependencies and verification

Web features may depend on local shared code and server-side API contracts, never concrete deployment settings. Before handoff, run `make toolchain-check`, `make harness-test`, and the package targets when they exist: `pnpm --filter @artificial-brain/web format:check`, `lint`, `test`, and `build`.
