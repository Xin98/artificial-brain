# Web guidance

## Zones

- **Green:** implement the server-rendered system-health feature, the ITER-0002 workbench features (auth, dashboard, todos, settings, conversation), the ITER-0003 dashboard reminder extension (real reminder counters and the reminder records list), the ITER-0004 `/data` data portability feature (export and two-phase import flow), and the ITER-0005 conversation upgrade (session sidebar, history-loading chat panel, `chat` kind rendering) and their tests under `apps/web`.
- **Yellow:** package boundaries, browser/server data boundaries, root workspace configuration, the `next.config.ts` rewrite and `shared/server` session seams, public UI contracts, and `globals.css` (styling contract enforced by `globals-css.test.ts`) require a planned review; ITER-0005 yellow items are listed in `docs/iterations/ITER-0005/plan.md`.
- **Red:** feature code must not import deployment configuration or expose Compose service names to the browser; in ITER-0005 do not add new web dependencies.

## Dependencies and verification

Web features may depend on local shared code and server-side API contracts, never concrete deployment settings. Before handoff, run `make toolchain-check`, `make harness-test`, and the package targets when they exist: `pnpm --filter @artificial-brain/web format:check`, `lint`, `test`, and `build`.
