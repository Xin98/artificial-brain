# Backend guidance

## Zones

- **Green:** implement the health-chain code and the business modules (`identity`, `todo`, `reminder`, `conversation`, `portability`) — including the ITER-0003 reminder delivery hexagon, the ITER-0004 identity/todo/reminder import-export seams, and the ITER-0005 conversation sessions/free-chat/history hexagon (unified `ModelPort.Complete` envelope, session commands and history queries, deterministic/OpenAI adapter updates) — and their tests inside `backend/internal/modules/<context>/{domain,application,adapters}` and established platform boundaries.
- **Yellow:** commands, migrations, configuration contracts, the platform transaction/router seams, and cross-package boundaries require a planned review; ITER-0005 yellow items are listed in `docs/iterations/ITER-0005/plan.md`.
- **Red:** API and Worker never run migrations; platform never imports a business module; do not create empty business packages; in ITER-0005 tests and CI deliver only through the deterministic/fake model adapter (no real-provider calls from CI).

## Dependencies and verification

Dependencies flow `inbound adapter -> application -> domain`; application uses ports and adapters implement them; `cmd` owns concrete composition. Cross-context calls go only through public application interfaces. Run `make toolchain-check` and `make harness-test`; also run targeted `go test` for the packages you touch and the root Make verification targets.
