# Repository guidance

## Zones

- **Green:** add focused implementation and tests within an existing feature or platform boundary.
- **Yellow:** changes to root build configuration, CI, public contracts, migrations, architecture policy, or any `AGENTS.md` must be listed in the iteration plan and handled deliberately.
- **Red:** do not add Identity, Todo, Conversation, Reminder, or Portability behavior in ITER-0001; do not commit credentials or local environment files.

## Dependencies and verification

Dependencies flow `inbound adapter -> application -> domain`; application depends on ports, outbound adapters implement them, and `cmd` performs concrete wiring. Platform never imports a business module. Run `make toolchain-check` and `make harness-test` before changing repository policy; later targets are added by their owning iteration tasks.
