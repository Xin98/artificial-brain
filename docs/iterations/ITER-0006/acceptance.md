# ITER-0006 investment acceptance

Scope: US equities, daily decisions, long-only integer-share virtual accounts. Branch `codex/investment-us-equities`, execution base `c26fcec0370a905357efb18abdf7f9a320a52450`. Design and all 19 tasks are recorded under `docs/superpowers/` and the [durable execution ledger](progress.md).

## Functional evidence

| Capability | Evidence |
| --- | --- |
| Exact cash and account constraints | Domain overflow/rounding/conservation tests; actual PostgreSQL rollback, owner-scoped versions and funding idempotency |
| Point-in-time quant research | Snapshot availability/ingestion cutoffs, raw accession revisions, 201-bar eligibility, common-candidate ranking, stable ties and future-input mutation tests |
| Independent potential and risk | Domain analysis tests and web component tests display high potential with high risk; missing financial metrics retain explicit reasons |
| Read-only data adapters | Local HTTP servers prove bounded 429 retry, single-attempt 401/403, timeouts, redirect refusal, SEC identity/units and disabled-news semantics |
| Shared hard limits | Manual/automatic order tests, pending exposure/turnover and 120 seeded portfolios |
| Opening fills, pause and recovery | Real DB concurrent execution, commit/rollback retries, late-bar original opening, expiry non-revival, pre/post-open pause and exact release |
| Settlement and corporate actions | NY midnight T+1; separate synthetic non-settlement day; split cost preservation/cancellation; pre-ex dividend entitlement/payment; delayed-fill and revised-action blocking |
| API composition closed loop | `TestInvestmentClosedLoopRestartCompetitionAndSettlement` authenticates two owners, submits idempotent manual orders, processes an automatic frozen evaluation, advances D/open/close/T+1, competes two runtimes and instantiates fresh runtimes after commits; one fill per order and one settlement |
| Real-mode honesty | Composition reads unconfigured `alpaca_sec/sip`: explicit not-configured status and empty instruments, never fixture substitution |
| Replay parity | Actual DB full July forward commands equal isolated replay NAV and fill count; benchmark shares, cash, costs and dividends; future knowledge and late confirmation tests |
| Web user journey | Isolated Chrome at 1200px/390px, light/dark: account create/enable/manual pending/pause, analysis, completed month and seven-month async River backtests; no document overflow or non-finite plots |
| Same-origin stack journey | `tests/smoke/investment_test.sh` runs within the existing ephemeral fixture stack, uses two cookie jars, checks owner 404, source labels, decimals, retries, enable/pause, async result and benchmark |

Browser checks current-time business flows. Multi-day opening/settlement transitions use an injected Go clock; no production clock/cash override endpoint exists. Split/dividend/late-data/provider fault scenarios are proved at their established integration boundaries and included in final suites rather than duplicated in one HTTP test.

## Final verification

| Gate | Actual result |
| --- | --- |
| `make toolchain-check` | Linux ext4 verifier: exit 0, Go 1.26.5 / Node 24.18.0 / pnpm 11.19.0 |
| `make harness-test` | Linux ext4 verifier: exit 0; Windows newline-filename limitation retained in ledger |
| `make verify` | Linux ext4 verifier: exit 0; format, vet/lint, architecture, contracts, race, Go/web builds and credential scanner; web 39 files / 222 tests |
| `go test -p=1 ./... -race` with `TEST_DATABASE_URL` | Windows native: exit 0, actual PostgreSQL; fixture 190.021s and investment DB 272.626s |
| `make migration-test` | Docker Desktop Linux verifier: exit 0; fresh schema 11, repeated migrate, constraints, all module/database/API tests with race; API composition 32.268s |
| `make smoke-test` | Docker Desktop Linux verifier: exit 0; investment journey, original identity/todo/conversation/reminder/portability flows, private phone/email gates, backup/restore, upgrade and final health |

Whole-branch independent review follows the final stack run. A skipped database suite is never described as passed: the Linux `verify` run has no DB configuration and is complemented by the explicit native race/database and fresh migration proofs above. Tests that truncate shared schemas run serially in the actual DB proof, matching the migration script's existing `-p=1` invocation. The tracked-file credential scanner also passes on the complete staged native branch; the config fixture uses a credential-free database URL. The disposable ext4 git index is refreshed before its final repeat to include every new source file.

Windows has no native make and its newline-filename harness fixture fails. The same make targets run in the disposable Linux ext4 verifier; migration/stack targets run through Docker Desktop with a curl host-gateway translation that preserves original URLs, headers and cookies. The repository's scripts, CI gates and pinned dependencies are retained.

## External validation and delivery boundary

All shipped default data is `fixture/synthetic/v2`, explicitly marked demonstration data. Actual Alpaca/SEC access, SIP entitlement, news permissions, settlement-calendar operations and real strategy performance remain **unconfigured / unverified**. No private High-Flyer model, trained Qlib artifact or real broker-account trading is claimed. Read [the runbook](../../runbooks/investment.md) before configuring read-only sources.

The isolated branch/worktree is the reviewable local deliverable. No push, merge, publishing or production deployment is part of this task. Original checkout and its untracked manual verification files remain preserved.
