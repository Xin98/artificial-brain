# Web guidance

## Zones

- **Green:** implement the server-rendered system-health feature, the ITER-0002 workbench features (auth, dashboard, todos, settings, conversation), and the ITER-0003 dashboard reminder extension (real reminder counters and the reminder records list) and their tests under `apps/web`.
- **Yellow:** package boundaries, browser/server data boundaries, root workspace configuration, the `next.config.ts` rewrite and `shared/server` session seams, and public UI contracts require a planned review.
- **Red:** feature code must not import deployment configuration or expose Compose service names to the browser; in ITER-0003 do not add portability UI or new web dependencies.

## Dependencies and verification

Web features may depend on local shared code and server-side API contracts, never concrete deployment settings. Before handoff, run `make toolchain-check`, `make harness-test`, and the package targets when they exist: `pnpm --filter @artificial-brain/web format:check`, `lint`, `test`, and `build`.
