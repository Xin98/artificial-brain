# Repository guidance

## Zones

- **Green:** add focused implementation and tests within an existing feature or platform boundary, including the ITER-0004 private deployment and data portability work (Identity, Todo, Conversation, Reminder, and Portability modules, plus the `/data` web feature).
- **Yellow:** changes to root build configuration, CI, public contracts, migrations, architecture policy, or any `AGENTS.md` must be listed in the ITER-0004 iteration plan's yellow-zone register and handled deliberately.
- **Red:** in ITER-0004 do not call real providers from CI (private-mode smoke runs development fakes), do not commit credentials, do not lower CI gates, and migrations 001–007 stay untouched.

## Dependencies and verification

Dependencies flow `inbound adapter -> application -> domain`; application depends on ports, outbound adapters implement them, and `cmd` performs concrete wiring. Platform never imports a business module. Run `make toolchain-check` and `make harness-test` before changing repository policy; later targets are added by their owning iteration tasks.
