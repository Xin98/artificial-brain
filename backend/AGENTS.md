# Backend guidance

## Zones

- **Green:** implement the health-chain code and the ITER-0003 business modules (`identity`, `todo`, `reminder`, `conversation`) — including the reminder delivery hexagon (River worker inbound adapter and fake/SMTP/Aliyun provider outbound adapters) — and their tests inside `backend/internal/modules/<context>/{domain,application,adapters}` and established platform boundaries.
- **Yellow:** commands, migrations, configuration contracts, the platform transaction/router seams, and cross-package boundaries require a planned review.
- **Red:** API and Worker never run migrations; platform never imports a business module; do not create empty business packages; in ITER-0003 tests and CI deliver only through fake adapters (no real-provider calls from CI) and no Portability behavior is added.

## Dependencies and verification

Dependencies flow `inbound adapter -> application -> domain`; application uses ports and adapters implement them; `cmd` owns concrete composition. Cross-context calls go only through public application interfaces. Run `make toolchain-check` and `make harness-test`; also run targeted `go test` for the packages you touch and the root Make verification targets.
