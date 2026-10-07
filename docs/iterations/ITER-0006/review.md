# Investment final independent review

Fresh-context `gpt-6-astra` reviewed `c26fcec..83767d0` read-only, without subagents. All five Review Focus items and module/persistence/source/web paths were checked. No Critical or Minor findings; nine Important findings required fixes. The executor confirmed them and completed one fix pass, with no second review.

| Finding | Failing regression and resolution |
| --- | --- |
| SEC composition fetched homepage | `TestRealCompositionUsesSECDirectoryJSON`: actual runtime/source pipeline with PostgreSQL, HTTP routed only to a local server; correct adapter JSON directory default |
| Incomplete input consumed day | `TestAutomaticEvaluationWaitsForDelayedInputs`: delayed bars and absent-then-arriving financials consume no immutable run; later orders issue once. Readiness computes outside the account transaction; lock verifies inspected version |
| Ranking loss disabled protection | `TestRankingFailurePreservesAccountProtectionsAndAdvice`: stop loss/drawdown survive missing financials; automatic/replay paths withhold ranked buys and retain definite reductions |
| Ex-date split applied twice | `TestSplitDoesNotReadjustEffectiveSession`: pre/ex-date 100/50 becomes 50/50; original raw input unchanged |
| Split valuation mixed share units | `TestAnalysisWithholdsUnprovenSplitFinancialUnits`, `TestValuationRejectsSharesStillReportedBeforeOlderSplit`: explicit unavailable valuation when TTM crosses split or shares predate it; independent price risk retained |
| Poll error destroyed mutation identity | Account-panel test submits uncertain order, polls 503, recovers newer version, and proves retained draft/body/key; existing account/stock isolation tests retained |
| News failure aborted maintenance | `TestOptionalNewsFailureDoesNotAbortRealSource`: composed local 403/429/timeout all preserve `news_unavailable` and successful market/financial sync; persistence failure still propagates |
| Removed holding lost refresh | `TestRealRefreshIncludesHoldingsOrdersAndPendingUniverse`: deduplicated sorted active/pending pools + holdings + pending orders; provider batches <=100 |
| Holding advice only checked buys | Account-protection and stock-analysis tests show shared-rule reduction advice/stop-loss reason; drawdown yields pause advice |
| Additional reproduced ticker-reuse identity defect | `TestCompositionResolvesReusedTickerFromCurrentProviderIdentity`: old source composition associates financials with a retired security sharing the ticker; the current-provider response's stable ID now supplies the association, without another HTTP request |

The corrected stop-loss fixture and delayed-financial scenario also fail against the original evaluator through Go's overlay option and pass on corrected source; overlay does not alter checkout history. Test setup initially used a timeout shorter than SEC's deliberate rate limiter and a fixed clock preceding actual ingestion; corrected setup uses a sufficient bounded timeout/current clock. No real provider was called.

Post-fix exact-code `go test -p=1 ./... -race` with actual PostgreSQL exits 0 (source composition 16.560s; unchanged business packages reuse successful exact-code test cache). `make toolchain-check harness-test verify` exits 0 with all 39 web files / 225 tests and production build. Complete `make smoke-test` exits 0, including the investment journey and all original stack scenarios. Fresh migration acceptance from Task 19 remains valid because this fix pass changes no schema. Detailed proof is recorded in [acceptance](acceptance.md) and [progress](progress.md). No deferred minors. Reviewer-declined live connectivity/permissions/coverage, profitability, and real broker/intraday/derivatives/short/private-model capabilities each have explicit executor rulings and costs in progress.md.
