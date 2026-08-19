# Repository guidance

## Zones

- **Green:** add focused implementation and tests within an existing feature or platform boundary, including the ITER-0003 reminder delivery work (Identity, Todo, Conversation, and Reminder modules).
- **Yellow:** changes to root build configuration, CI, public contracts, migrations, architecture policy, or any `AGENTS.md` must be listed in the iteration plan and handled deliberately.
- **Red:** in ITER-0003 do not add Portability behavior and do not call real providers from CI (fake adapters only); do not commit credentials or local environment files; do not lower CI gates.

## Dependencies and verification

Dependencies flow `inbound adapter -> application -> domain`; application depends on ports, outbound adapters implement them, and `cmd` performs concrete wiring. Platform never imports a business module. Run `make toolchain-check` and `make harness-test` before changing repository policy; later targets are added by their owning iteration tasks.
