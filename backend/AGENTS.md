# Backend guidance

## Zones

- **Green:** implement the health-chain code and tests inside established backend package boundaries.
- **Yellow:** commands, migrations, configuration contracts, and cross-package boundaries require a planned review.
- **Red:** API and Worker never run migrations; platform never imports a business module; do not create empty business packages.

## Dependencies and verification

Dependencies flow `inbound adapter -> application -> domain`; application uses ports and adapters implement them; `cmd` owns concrete composition. Run `make toolchain-check` and `make harness-test`; when Go packages exist, also run their targeted `go test` and the root Make verification targets added by later tasks.
