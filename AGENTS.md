# Repository guidance

## Zones

- **Green:** add focused implementation and tests within an existing feature or platform boundary, including the ITER-0002 Identity, Todo, Conversation, and reminder-seam modules.
- **Yellow:** changes to root build configuration, CI, public contracts, migrations, architecture policy, or any `AGENTS.md` must be listed in the iteration plan and handled deliberately.
- **Red:** in ITER-0002 do not add reminder delivery, River, real notification providers, or Portability behavior; do not commit credentials or local environment files.

## Dependencies and verification

Dependencies flow `inbound adapter -> application -> domain`; application depends on ports, outbound adapters implement them, and `cmd` performs concrete wiring. Platform never imports a business module. Run `make toolchain-check` and `make harness-test` before changing repository policy; later targets are added by their owning iteration tasks.
