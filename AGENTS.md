# Repository guidance

## Zones

- **Green:** add focused implementation and tests within an existing feature or platform boundary, including the ITER-0005 conversation upgrade (Conversation module sessions/free-chat/history work plus the `/conversation` web feature).
- **Yellow:** changes to root build configuration, CI, public contracts, migrations, architecture policy, or any `AGENTS.md` must be listed in the ITER-0005 iteration plan's yellow-zone register and handled deliberately.
- **Red:** in ITER-0005 do not call real providers from CI (conversation smoke runs the deterministic development adapter), do not commit credentials, do not lower CI gates, do not add Go or web dependencies, and migrations 001–009 stay untouched.

## Dependencies and verification

Dependencies flow `inbound adapter -> application -> domain`; application depends on ports, outbound adapters implement them, and `cmd` performs concrete wiring. Platform never imports a business module. Run `make toolchain-check` and `make harness-test` before changing repository policy; later targets are added by their owning iteration tasks.
