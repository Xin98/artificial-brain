# Architecture-policy guidance

## Zones

- **Green:** add tests and policy code that enforce an approved dependency rule.
- **Yellow:** changing a dependency rule, an allowlist, or a public architecture contract requires an explicit iteration decision.
- **Red:** policy must not permit domain-to-adapter dependencies, cross-context internals, or platform-to-business imports.

## Dependencies and verification

The enforced direction is `inbound adapter -> application -> domain`, with application depending on ports and concrete adapters composed by `cmd`. Run `make toolchain-check` and `make harness-test`; run `make architecture-test` once that target is introduced.
