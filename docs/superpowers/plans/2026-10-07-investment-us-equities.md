# 美股日线投资模块 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 在现有工作台交付可解释的美股日线量化、财报/热点分析和带风控的虚拟账户自动交易闭环。

**Architecture:** 新增独立 Investment 业务上下文，采用 `inbound adapter → application → domain`，数据与存储适配器实现应用 ports，具体组合位于 cmd。复用 PostgreSQL、TxRunner、River、登录与 Next.js，历史回测和持续模拟复用领域规则但隔离账本。

**Tech Stack:** Go 1.26.5（或较新的 1.26 patch）、Node.js 24.18.0、pnpm 11.19.0、现有 Next.js/React、PostgreSQL、pgx、River；不增加 Go/Web 依赖。

**Spec:** [已批准的投资设计](../specs/2026-10-07-investment-us-equities-design.md)；[领域词汇](../../../CONTEXT.md)。设计批准日期 2026-10-07；用户已批准本计划并选择 Native 执行。19 项任务及最终独立复核修复已完成，逐项证据见 [验收记录](../../iterations/ITER-0006/acceptance.md)、[执行记录](../../iterations/ITER-0006/progress.md) 和 [复核记录](../../iterations/ITER-0006/review.md)。下文保留原执行步骤供追溯。

## Global Constraints

- 当前模式：美股普通股、日线评估、仅做多、不加杠杆、不做碎股；ETF 仅作基准，默认 SPY。
- 资金默认 100,000 USD；不得接入实盘下单、开设真实账户、购买订阅或把演示数据称为真实行情。
- CI、Compose 默认和 smoke 使用固定 fixtures；实源适配器只用本地 HTTP 测试服务器验证。
- 保留迁移 001–010 字节不变，当前 schema version 10；执行时确认下一编号，预计只追加 `011_investment.sql` 并升级到 11。
- Platform 不导入业务模块；应用仅依赖自己的 ports；跨上下文仅用公共应用接口；AGENTS.md、根构建、CI、架构政策、Go/Web 依赖不变。
- 所有账户/实验/订单/配置按 workspace/owner 隔离；修改幂等，货币以精确十进制字符串传输；API 沿用 `{code,message,correlationId}`。
- 股票池最多 100 只；默认策略至少 10 只共同合格候选；缺失因子不补零、不改权重；201 根预热日线，回测执行期至少 20 个交易日。
- 策略组权重动量 30%、趋势 20%、低波动 15%、估值 15%、财务质量 20%；潜力 >=70 较高、50–69.9 观察、<50 较弱，不是盈利概率。
- 自动交易初始关闭；单股目标 <=10%、行业组 <=30%、总股票目标 <=80%、账户回撤 >=15% 暂停、收盘止损 >=10%、普通双边日换手 <=20% NAV。
- 默认次交易日开盘模拟：10bps 滑点、1bps 费用、最低 $0.01；成交容量为决策时最近20日平均成交量的1%，同账户/证券/执行日共享。
- 美东决策时区 `America/New_York`；交易和结算日历分开；日线收盘至少30分钟后评估，Worker每15分钟补偿检查。
- 首版不包括 Qlib 运行时/训练、券商 Paper Trading 账户、投资对话指令或投资导入导出；不展示未接通的 AI 能力。
- 不触碰用户已有 `manual-verify/`。所有 yellow 变更已在 [ITER-0005 register](../../iterations/ITER-0005/plan.md) 预登记；扩大范围先更新登记。

## Review Focus

1. 数字超过精度/容量、负数、科学计数法及非有限值：拒绝输入或溢出错误，不舍入成另一笔订单、不产生负余额（Task 1/14）。
2. ticker 改名、数据集切换和财报修订：稳定证券身份与固定快照不可被最近记录悄悄改写（Task 2/8/13）。
3. 模拟开盘已过、日线未到时暂停/撤单：不能事后取消已生效订单，也不能把未决持仓当成空仓再买一次（Task 11/13/17）。
4. 交易所开市但证券结算关闭、跨年/提前收盘/夏令时：资金不能提前可用，过期日历必须阻止自动执行（Task 2/12/13）。
5. 浏览器切换账户、请求超时或点击重试：迟到响应不写入新账户页面，同一修改重用幂等键，401与503区分（Task 15/16/17）。

## File Map and Execution Contract

下列任务用“完整目录 + 文件名列表”列出精确路径，目录不是新增包模板；只有可运行实现和测试一起进入提交。Go 文件同名 `_test.go` 为对应测试。表中的每个目录只承担所列职责。

| 目录                                                      | 职责 / 主要文件                                                  | Tasks         |
| --------------------------------------------------------- | ---------------------------------------------------------------- | ------------- |
| `backend/internal/modules/investment/domain`              | 精确资金、时间口径、因子、风控、账本、回放；不导入 provider/平台 | 1–4、9–12、14 |
| `backend/internal/modules/investment/application/dto`     | 应用请求/查询/任务参数；HTTP十进制和分页投影                     | 5–9、11–15    |
| `backend/internal/modules/investment/application/ports`   | 数据、存储、事务和调度接口                                       | 5–9、11–15    |
| `backend/internal/modules/investment/application/command` | owner-scoped事务、同步、评估、执行、回测和启停                   | 6–9、11–14    |
| `backend/internal/modules/investment/application/query`   | owner-scoped数据状态、分析、账户、列表和绩效查询                 | 9–10、15      |
| `backend/internal/modules/investment/adapters/outbound`   | fixture、alpaca、sec、postgres、river适配器                      | 2、5–9、11–14 |
| `backend/internal/modules/investment/adapters/inbound`    | http/worker；只调用应用接口                                      | 13、15        |
| `backend/cmd/api`、`backend/cmd/worker`                   | 具体依赖组装、auth、后台注册                                     | 13、15        |
| `apps/web/src/features/investment`                        | 验证契约、中文页面、可访问SVG图表                                | 16–18         |
| `apps/web/src/app/(workbench)/investment`                 | 页面入口，复用现有session gate                                   | 16–18         |

Go 类型别名约定：文中 `domain.*`、`dto.*`、`ports.*` 指 Investment 自有包。所有 Handle 输入均由 DTO 携带 `Scope domain.Scope`；HTTP 从 `identity/application/dto.PrincipalFromContext` 转换，不接受 body 中的 owner/workspace。命令的 `Now func() time.Time` 和 `NewID func() string` 注入，测试固定时间/ID。测试代码块为核心断言，测试前置数据在同任务明确列出；不需要复制完整测试样板。

依赖顺序：1 → 2 → 3 → 4 → 5 → 6 → 7 → 8 → 9 → 10 → 11 → 12 → 13 → 14 → 15 → 16 → 17 → 18 → 19。Task 7–9 的外部接口实现不得使用网络访问实际供应商；fixtures 能贯通全链后才进行由操作者配置凭据的手动实源验收。

执行准备归属 Task 1：先读取适用 AGENTS.md、设计、计划和yellow register；通过 using-git-worktrees 检查/建立隔离checkout。执行时创建 `docs/iterations/ITER-0006/progress.md` 记录 `git rev-parse HEAD` 的执行起点SHA和各task red/green/verification/commit，原 ITER-0005 register保留投资后续迭代交接说明。后续验证命令中的execution-base指该已记录SHA，不是未知的分支名。没有用户明确要求时不自动合并、推送或部署。

## Task 1: 精确金额与虚拟账户值对象

**Files:** Create `backend/internal/modules/investment/domain/` 中 `money.go`、`account.go`、`order.go`、`errors.go`及各自测试；执行准备创建 `docs/iterations/ITER-0006/progress.md`，补充原黄区register的交接链接。

**Interfaces:** 定义 `Money int64`（分）、`Price int64`（百万分之一美元）、`Quantity int64`、`Scope{WorkspaceID,OwnerUserID string}`；`ParseMoney(string)(Money,error)`、`ParsePrice(string)(Price,error)`、`ParseQuantity(string)(Quantity,error)`、`GrossValue(Price,Quantity)(Money,error)`、`Money.String() string`、`Price.String() string`。Account有ID/Mode/当前绑定版本ID字符串、Scope、Balances、Version int、AutomationEnabled bool、Policy RiskPolicy及PendingConfig *AccountConfig；AccountConfig有StrategyVersionID/UniverseVersionID字符串、Policy及EffectiveAt时间。`Balances{Available,Reserved,Unsettled,Dividends Money}`；LedgerEntry有ID/AccountID/EventKey/Kind/InstrumentID字符串、Delta Balances、QuantityDelta Quantity、EffectiveAt/RecordedAt时间，Delta可负但账户余额不可负。Order有ID/AccountID/InstrumentID/Side/State字符串、Quantity/ReservedQuantity数量、ReservedCash金额、TargetOpenAt/ExpiresAt时间、Version int。定义规格第9节状态/错误，`RiskPolicy{SingleWeight,IndustryWeight,StockWeight,DrawdownPause,StopLoss,TurnoverLimit float64}`及`DefaultRiskPolicy() RiskPolicy`返回全局约束阈值。

- [ ] **Step 1: 写失败测试。** `TestMoneyExactRoundTrip`、`TestGrossValueRoundsHalfUp`、`TestRejectInvalidDecimalAndOverflow`、`TestDefaultRiskPolicy`：金额最多2位、价格最多6位；拒绝科学计数法、NaN/Infinity、超精度及溢出。Money接受int64内安全非负金额，创建账户初始资金额外限 $1,000,000,000；下单价格上限 $1,000,000、下单数量上限1,000,000,000，但财务股本/市场成交量不套用下单限制。算术中间结果用标准库big.Int检查。GrossValue按半入到分，费用另行向上取整。核心断言：

```go
money, _ := ParseMoney("100000.00")
gross, _ := GrossValue(Price(1234567), Quantity(100))
if money != Money(10000000) || gross != Money(12346) { t.Fatal(money, gross) }
if _, err := ParseMoney("1e5"); err == nil { t.Fatal("accepted scientific notation") }
```

- [ ] **Step 2: 跑失败证明。** `go test ./backend/internal/modules/investment/domain -run 'Test(Money|GrossValue|RejectInvalid|DefaultRisk)' -v`，应因未定义接口失败。
- [ ] **Step 3: 实现值对象。** 不以float64保存钱；Money解析允许零作余额，账户初始资金额外必须正数，价格/下单数量必须正。账户默认关闭自动交易，订单终态不可反转，不创建空业务包。
- [ ] **Step 4: 验证。** 运行同一测试命令及 `go test ./architecture/...`；执行 `make toolchain-check`、`make harness-test` 记录环境基线。如果make不可用，调用原脚本 `sh scripts/check-toolchain.sh`、`sh tests/harness/repository_policy_test.sh` 并记录实际失败，不修改门槛；迁移/完整smoke未通过不能称整体验收完成。
- [ ] **Step 5: 提交。** 只stage本任务文件及进度记录，`git commit -m "feat: add investment money and account primitives"`。

## Task 2: 数据快照、交易/结算日历和离线fixture

**Files:** Create `backend/internal/modules/investment/domain/` 中 `data.go`、`calendar.go`、`snapshot.go`及测试；Create `backend/internal/modules/investment/adapters/outbound/fixture/` 中 `adapter.go`、`adapter_test.go`、`testdata/calendar.json`、`testdata/instruments.json`、`testdata/bars.csv`、`testdata/facts.json`、`testdata/actions.json`、`testdata/news.json`。fixture用go:embed，不依赖工作目录。

**Interfaces:** Instrument有ID/Ticker/CIK/Exchange/SIC/Kind字符串及Tradable bool；`Bar{InstrumentID string; SessionDate,AvailableAt time.Time; Open,High,Low,Close Price; Volume Quantity}`；`FinancialFact{InstrumentID,Accession,Concept,Value,Unit,Currency string; PeriodStart,PeriodEnd,AvailableAt time.Time}`；CorporateAction有ID/InstrumentID/Kind/Currency字符串、EffectiveAt/AvailableAt/PayAt时间、RatioNumerator/RatioDenominator int64、Amount Price（每股分红）、CashInLieu *Money。NewsItem有ID/标题/摘要/链接/Source字符串、InstrumentIDs列表、PublishedAt/AvailableAt时间；Snapshot有ID/DatasetVersion字符串、AsOf时间及上述实体切片/QualityFlags。每种源记录都有Source/SourceRecordID/IngestedAt以保留provenance。`SelectSnapshot(input Snapshot, asOf time.Time)(Snapshot,error)`；`Calendar.NextSession(after time.Time)(Session,error)`、`Calendar.NextSettlement(after time.Time)(time.Time,error)`；`fixture.New()(*Adapter,error)`返回可取固定数据的Adapter，Task 7/8/9各增加端口方法。`Session{Date,OpenAt,CloseAt time.Time}`，日历分离交易/结算记录并保存版本/年份。

- [ ] **Step 1: 写失败测试。** `TestSnapshotDoesNotExposeFutureRevision`：同一事实初版available=09:00、修订版=17:00，16:30快照只选初版，18:00可选修订。`TestCalendarSeparateSettlementAndTradingDays`：fixture某日交易开市/结算关闭，NextSettlement跨过该日；跨年未覆盖报错，夏令时和提前收盘保留准确UTC。`TestFixtureIdentityStableAcrossTickerChange`：同一个instrumentId改ticker仍是一项证券。`TestFixtureRepeatableAndNoNetwork`验证30只虚构普通股+SPY、至少500个按日历交易日的bars、完整财报及拆股/分红，读取两次字节/版本一致。
      核心断言（变量由上述固定场景的执行结果赋值）：

```go
if len(beforeRevision.Facts) != 1 || beforeRevision.Facts[0].Value != "100" { t.Fatal(beforeRevision.Facts) }
```

- [ ] **Step 2: 跑失败证明。** `go test ./backend/internal/modules/investment/domain ./backend/internal/modules/investment/adapters/outbound/fixture -run 'Test(Snapshot|Calendar|Fixture)' -v`，新接口尚不存在。
- [ ] **Step 3: 实现。** Snapshot以availableAt截断，再按证券/概念/报告期选当时可知版本；同日财报未知时刻按下一交易日可用。fixture文件是固定演示输入，不伪称真实证券历史；还包括后面task要使用的重报、缺失、停牌、跳空、拆股余股与分红案例。
- [ ] **Step 4: 验证。** 同一命令PASS；`go test ./architecture/...`；更改未来修订值不改变16:30快照；缺少日历阻止产生交易时刻。
- [ ] **Step 5: 提交。** `git commit -m "feat: add point-in-time investment snapshots and fixtures"`。

## Task 3: 财报归一化和价格指标

**Files:** Create `backend/internal/modules/investment/domain/` 中 `financials.go`、`indicators.go`及测试；扩充Task 2的`facts.json`中按accession标识的累计季度案例。

**Interfaces:** `NormalizeTTM(facts []FinancialFact, asOf time.Time)(TTMFinancials,error)`；`ComputeIndicators(bars []Bar)(Indicators,error)`；`ComputeValuation(price Price, ttm TTMFinancials, shares FinancialValue)(Valuation,error)`。定义 `FinancialValue{Amount,Unit,Currency,Reason string; FactRefs []string}`保留精确十进制原数；`Metric{Value *float64,Reason string,FactRefs []string}`用于最终比率。TTMFinancials含NetIncome/Revenue/OperatingCashFlow/CapitalExpenditure/FreeCashFlow/DilutedEPS/OpeningEquity/ClosingEquity这些FinancialValue及LatestPeriodEnd时间。FinancialMetrics含TTM、Valuation、Indicators、ProfitGrowth/RevenueGrowth/DebtRatio/ROE这些Metric。Valuation包含PE/PB/MarketCap/EarningsYield/FreeCashFlowYield这些Metric；Indicators包含SMA20/50/200、RSI14、AnnualVolatility、MaxDrawdown、AverageVolume20、AverageTurnover20及WindowDays，缺失项保持Metric.Value=nil。财务金额与股本用big.Rat计算，转换比率时检查finite，不把发行股本误用成可下单数量。

- [ ] **Step 1: 写失败测试。** `TestTTMUsesNonOverlappingQuarters`：Q1=10、半年累计=30、9月累计=60、全年=100，季度还原10/20/30/40，TTM=100而不是200。`TestNonPositiveEPSHasNoPE`、`TestFinancialUnitAndCurrencyMismatch`、`TestIndicatorsNoPadding`、`TestRSIBoundaries`：PE分母非正不适用；SMA200不足200条不补零；14个无涨跌RSI=50、仅涨=100、仅跌=0；回撤100→120→90应25%。资本支出符号归一化后OCF=120、CapEx=20，FCF=100。
      核心断言（变量由上述固定场景的执行结果赋值）：

```go
if ttm.NetIncome.Amount != "100" { t.Fatal(ttm.NetIncome) }
if valuation.PE.Value != nil { t.Fatal("PE must be unavailable with nonpositive EPS") }
```

- [ ] **Step 2: 跑失败证明。** `go test ./backend/internal/modules/investment/domain -run 'Test(TTM|NonPositive|Financial|Indicators|RSI)' -v`，缺少计算实现而失败。
- [ ] **Step 3: 实现。** 累计报告先按截止时间/报告版本选事实，再还原季度；无法明确比较返回factor_unavailable，不猜自定义XBRL含义。实现规格§6的PE/PB/市值/增长/现金流/资产负债率/ROE、SMA20/50/200、Wilder RSI14、20日样本波动、252日回撤和20日成交额；价格和股数拆股口径一致。
- [ ] **Step 4: 验证。** 同一命令PASS；固定输入指标与手算结果相符；所有null值有reason及source refs。
- [ ] **Step 5: 提交。** `git commit -m "feat: normalize investment financials and indicators"`。

## Task 4: 多因子信号、风险等级和解释建议

**Files:** Create `backend/internal/modules/investment/domain/` 中 `strategy.go`、`scoring.go`、`analysis.go`及测试。

**Interfaces:** `StrategyParameters{Weights [5]float64; EntryScore,ExitScore,RebalanceBand float64; MaxHoldings int}`（权重之和1，Entry=70、Exit=50、Band=0.02、MaxHoldings=10）；StrategyVersion有ID字符串、Parameters及CreatedAt时间；`DefaultStrategyParameters() StrategyParameters`；`Evaluate(snapshot Snapshot, universe UniverseVersion, strategy StrategyVersion)(Evaluation,error)`；`RankPercentiles(values []float64, higherIsBetter bool)([]float64,error)`；`ClassifyRisk(indicators Indicators) RiskAssessment`；`Explain(signal Signal, metrics FinancialMetrics, risk RiskAssessment)(Recommendation,error)`。UniverseVersion有ID字符串/InstrumentIDs切片/EffectiveAt时间/HistoricalMembershipKnown bool；Evaluation有ID及SnapshotID/StrategyVersionID/UniverseVersionID、Signals及Excluded原因列表；Signal有InstrumentID字符串、Score float64、Rank int、FactorEvidence及QualityFlags。RiskAssessment有Level/Reasons/AsOf；Recommendation有Action/AsOf/版本ID/Evidence/Unknowns，在此只给研究倾向，账户风控由Task10补充。

- [ ] **Step 1: 写失败测试。** `TestPercentilesAverageTies`使用[1,2,2,4]，预期[0,50,50,100]；`TestFixedFactorWeights`测试因子组[100,50,0,50,100]得到67.5；`TestInsufficientUniverseDoesNotReweight`测试9只报insufficient_universe、缺估值不改权重。`TestHighPotentialCanHaveHighRisk`：score=80、volatility=0.65同时高潜力/高风险；v=0.60且drawdown=0.20为中风险，严格区分 > 与 >=。

```go
scores, _ := RankPercentiles([]float64{1, 2, 2, 4}, true)
if scores[1] != 50 || scores[2] != 50 { t.Fatal(scores) }
if ClassifyRisk(Indicators{AnnualVolatility: Metric{Value: ptr(0.65)}, MaxDrawdown: Metric{Value: ptr(0.15)}}).Level != "high" { t.Fatal("risk") }
```

- [ ] **Step 2: 跑失败证明。** `go test ./backend/internal/modules/investment/domain -run 'Test(Percentiles|FixedFactor|InsufficientUniverse|HighPotential)' -v`；测试内定义只用于字面量的`ptr`小帮助函数。
- [ ] **Step 3: 实现。** 用Task3指标生成规格§5所有子因子；共同完整候选至少10只，201根价格历史；SEC SIC金融类/未知行业不自动候选，完整SIC→行业组表版本在`strategy.go`定义并接受查表测试。相同分数instrumentId稳定排序；财报最近期末180天限制只用于最近季度。保留excluded原因、因子贡献/源引用和“相对评分非概率”说明。
- [ ] **Step 4: 验证。** 同一命令PASS；重复Evaluate结果一致；未来数据变动不改旧快照评分；补充缺波动和缺回撤分别得到风险未知，而不是低。
- [ ] **Step 5: 提交。** `git commit -m "feat: add explainable multifactor investment research"`。

## Task 5: Investment schema、事务存储及所有权隔离

**Files:** Create `deploy/migrations/011_investment.sql`；Create `backend/internal/modules/investment/application/ports/` 中 `stores.go`、`unit_of_work.go`；Create `backend/internal/modules/investment/application/dto/` 中 `paging.go`、`records.go`；Create `backend/internal/modules/investment/adapters/outbound/postgres/` 中 `accounts.go`、`catalog.go`、`data.go`、`runs.go`、`orders.go`、`ledger.go`、`requests.go`、`integration_test.go`。Modify `backend/internal/platform/database/schema.go`、`migrate_integration_test.go`、`tests/smoke/migration_test.sh`、`tests/smoke/stack_test.sh`的schema/upgrade pins为11。

**Interfaces:** `UnitOfWork.Run(context.Context,func(context.Context)error)error`同既有TxRunner；每个Postgres Store用`database.ExecutorFromContextOr`，不自行Begin。以下ctx均context.Context、Scope均domain.Scope，id均string。所有私有方法scope在参数中，失配返回ErrNotFound。`AccountStore.Insert(ctx,domain.Account)error`、`Get(ctx,Scope,id)(domain.Account,error)`、`Lock(ctx,Scope,id)(domain.Account,error)`、`Save(ctx,domain.Account,expectedVersion int)error`；CatalogStore的`InsertUniverse(ctx,Scope,domain.UniverseVersion)error`、`GetUniverse(ctx,Scope,id)(domain.UniverseVersion,error)`和对应Strategy方法。SnapshotStore的`Insert(ctx,domain.Snapshot)error`、`Get(ctx,datasetVersion,id string)(domain.Snapshot,error)`、`Find(ctx,datasetVersion string,asOf time.Time)(domain.Snapshot,error)`；RunStore的`SaveEvaluation(ctx,Scope,accountID string,domain.Evaluation)error`、`GetEvaluation(ctx,Scope,id)(domain.Evaluation,error)`。订单存储由Task11增加；LedgerStore.InsertLedger(ctx,Scope,accountID string,entries []domain.LedgerEntry)error在Task6首次用于初始资金，不能预先声明空实现。

- [ ] **Step 1: 写失败集成测试。** `TestInvestmentSchemaAndOwnerIsolation`验证新schema及accounts、universe_versions、universe_members、strategy_versions、datasets、data_snapshots、data_sync_runs、instruments、instrument_facts、price_bars、financial_facts、news_items、corporate_actions、evaluation_runs、signals、recommendations、backtest_runs、orders、fills、ledger_entries、positions、nav_snapshots、automation_events、mutation_requests存在；账号读取换owner/workspace→notfound，改配置expectedVersion不符→conflict，版本记录不可UPDATE。`TestInvestmentTransactionRollback`：账户创建/初始流水事务中失败，两者都不留记录。
      核心断言（变量由上述固定场景的执行结果赋值）：

```go
if !errors.Is(otherOwnerError, domain.ErrNotFound) { t.Fatal(otherOwnerError) }
if accountCountAfterRollback != 0 || ledgerCountAfterRollback != 0 { t.Fatal("partial commit") }
```

- [ ] **Step 2: 跑失败证明。** 有独立测试库时 `go test ./backend/internal/modules/investment/adapters/outbound/postgres ./backend/internal/platform/database -run 'Test(Investment|Migrate)' -v`；预期缺schema失败。没有TEST_DATABASE_URL只能记录SKIP，不能计为数据库证明。
- [ ] **Step 3: 实现。** 重查迁移编号再追加：accounts含四现金科目/版本/模式/绑定版本；私有子表用workspace+owner+account复合FK避免跨owner引用；versions/snapshots不可变，provider历史事实保留sourceKey+accession+availableAt。orders存生效/过期/预留/状态；fills、ledger、公司行动应用键唯一；mutation_requests唯一(scope,route,key)，hash/response同事务保存；自动批次唯一(account,session,strategyVersion,mode)，且账户同交易日只能有一个发单批次。金额BIGINT分、Price BIGINT微美元，quantity BIGINT；余额/预留CHECK非负。JSONB只存封闭、版本化因子证据/参数/数据投影，金额和状态不藏在不受约束JSON中。
- [ ] **Step 4: 验证。** 同一测试PASS且无skip；`make migration-test`验证空库/重复迁移/10→11升级；迁移001–010哈希不变、API/Worker仍不迁移。更新stack升级pin时不能删旧升级场景。
- [ ] **Step 5: 提交。** `git commit -m "feat: persist investment facts with scoped transactions"`。

## Task 6: 账户、版本配置与修改幂等

**Files:** Create `backend/internal/modules/investment/application/` 中 `mutations.go`、`mutations_test.go`；Create `application/command/` 中 `create_account.go`、`versions.go`、`configure_automation.go`及测试；Create `application/dto/` 中 `commands.go`、`views.go`；Extend `application/ports/stores.go`、`adapters/outbound/postgres/requests.go`、`ledger.go`及集成测试。views.go定义基础AccountView（ID/Mode/绑定版本/四现金字符串/Version/AutomationEnabled/PendingConfig）和VersionView（ID/CreatedAt/参数或成员）；后面任务在此增补投影，不重复声明类型。

**Interfaces:** `dto.Mutation{Scope domain.Scope,Route,Key string,ExpectedVersion int}`；`MutationExecutor.Execute(ctx,mutation dto.Mutation,input any,work func(context.Context)(json.RawMessage,error))(json.RawMessage,error)`；`RequestStore.Claim(ctx,Scope,route,key,hash string)(dto.RequestRecord,error)`、`Complete(ctx,...,response json.RawMessage)error`。命令固定`Handle(ctx,request)(view,error)`：`CreateAccountHandler`用`CreateAccountRequest{Mutation,InitialCash string,Mode,StrategyVersionID,UniverseVersionID}`返回AccountView；`CreateUniverseVersionHandler`、`CreateStrategyVersionHandler`返回VersionView；`ConfigureAutomationHandler`返回AccountView。Config启用前检查snapshot/calendar覆盖和账户owner；Task11补入暂停的订单状态推进。

- [ ] **Step 1: 写失败测试。** `TestCreateAccountIdempotent`：同scope/route/key/body返回同ID且一条初始入账；同key不同body→idempotency_conflict，不创建第二账户。`TestVersionsImmutableAndPoolLimit`验证101成员拒绝、旧版本内容不变、无效权重拒绝；`TestModeChangeNeedsNewAccount`阻止fixture/provider模式改变拼接同一业绩；`TestEnableRequiresSettlementCalendar`缺真实结算日历不能启用，默认disabled。
      核心断言（变量由上述固定场景的执行结果赋值）：

```go
if first.ID != retry.ID || initialCreditCount != 1 { t.Fatal("duplicate account funding") }
if !errors.Is(changedBodyError, domain.ErrIdempotencyConflict) { t.Fatal(changedBodyError) }
```

- [ ] **Step 2: 跑失败证明。** `go test ./backend/internal/modules/investment/application/... -run 'Test(CreateAccount|Versions|ModeChange|Enable)' -v`。
- [ ] **Step 3: 实现。** 输入按严格DTO解析后做canonical JSON/SHA256 hash，scope/route参与幂等命名；Claim、work、Complete在同一短UnitOfWork，基础设施失败回滚，work不得进行外部HTTP读取。确定业务拒绝以带ErrorCode的封闭结果保存并提交审计，回传业务错误在事务完成之后转换，避免风险拒绝日志随rollback消失。账户初始化只一次available=10,000,000分的默认资金及对应初始流水。创建真实模式股票池初始为空，fixture版本30只演示股票；Universe/Strategy变更新增版本，不改历史。启用/暂停立即生效，策略/股票池/风控参数变更保存在PendingConfig并在下一交易日生效；锁账户、expectedVersion校验，写automation_events，历史参数保存在事件及相应run中。
- [ ] **Step 4: 验证。** 同一测试PASS；补充并发Claim相同key结果唯一、回滚后可重试；数据库测试验证科目余额/初始流水一致。分页视图和最终HTTP映射归Task14。
- [ ] **Step 5: 提交。** `git commit -m "feat: manage paper accounts and immutable strategy versions"`。

## Task 7: 只读行情接入、增量同步与服务端配置

**Files:** Create `backend/internal/modules/investment/application/ports/market_data.go`、`application/dto/sync.go`、`application/command/sync_market.go`及测试；Create `adapters/outbound/alpaca/` 中 `market.go`、`http_client.go`及测试；Extend `adapters/outbound/fixture/adapter.go`、`adapters/outbound/postgres/data.go`；Modify `backend/internal/platform/config/config.go`，Create `config_investment_test.go`，Modify `.env.example`、`compose.yaml`。

**Interfaces:** `MarketDataPort.Instruments(ctx,ids []string)([]domain.Instrument,error)`、`Bars(ctx,ids []string,from,to time.Time)([]domain.Bar,error)`、`Calendar(ctx,from,to time.Time)(domain.Calendar,error)`、`Actions(ctx,ids []string,from,to time.Time)([]domain.CorporateAction,error)`；source记录另带feed/sourceKey/availableAt。`SyncMarketHandler.Handle(ctx,dto.SyncRequest)(dto.SyncStatus,error)`；`SyncRequest{Mutation,Mode,DatasetVersion,InstrumentIDs,From,To}`；`SnapshotStore.AppendMarket(ctx,datasetVersion string,batch dto.MarketBatch)error`。`alpaca.NewMarket(client *http.Client,cfg MarketConfig)(*Market,error)`；`MarketConfig{DataURL,ReadOnlyTradingURL,Feed,Key,Secret string,Timeout time.Duration}`。配置 `INVESTMENT_DATA_MODE=fixture|alpaca_sec` 默认fixture、`INVESTMENT_ALPACA_FEED=iex|sip` 默认iex、`INVESTMENT_DATA_TIMEOUT=15s`，key/secret无默认，`INVESTMENT_SETTLEMENT_CALENDAR_FILE` 指向操作者提供的带source/version/year的JSON，读取到期/缺失为业务阻断状态。

- [ ] **Step 1: 写失败测试。** `TestMarketAdapterReadOnlyAndPaged`本地服务器记录method/path，资产、bars、calendar、actions多页返回完整结果；所有请求GET，任何`/orders`请求立即使测试失败。`TestMarket429TimeoutAndEntitlement`：429按Retry-After、401不重试、超时/权限不足返回可分类错误、sip不能静默降为iex。`TestMarketSyncModeVersionIsolation`：切mode/feed建立新datasetVersion、旧快照不变。`TestInvestmentConfigFixtureDoesNotNeedSecrets`验证默认fixture不读APIkey也不连接外网。
      核心断言（变量由上述固定场景的执行结果赋值）：

```go
if unauthorizedAttempts != 1 || brokerOrderRequests != 0 { t.Fatal("unsafe provider requests") }
if fixtureExternalRequests != 0 { t.Fatal("fixture performed network access") }
```

- [ ] **Step 2: 跑失败证明。** `go test ./backend/internal/modules/investment/adapters/outbound/alpaca ./backend/internal/modules/investment/application/command ./backend/internal/platform/config -run 'Test(Market|InvestmentConfig)' -v`。
- [ ] **Step 3: 实现。** 使用标准库HTTP与context deadline，本地fixture不创建真实provider连接；数据URL及只读TradingURL由cmd固定为官方端点，测试注入httptest地址。最多3次可重试读取、有界延迟，批量证券查询与分页全部完成才提交覆盖状态，停牌/空bars不得补造价格。网络读取在DB事务外，AppendMarket和新快照元数据在短事务内提交；保留provider资产稳定ID，CIK/SIC由Task8补齐。实例配置不由浏览器修改，Compose仅传服务端变量；未配置结算日历可看分析但不得启用自动交易。
- [ ] **Step 4: 验证。** 同一命令PASS，httptest验证带真实结构的响应但不调用实源；默认Compose仍fixture。检查日志输出不包含key/secret、feed/延迟/覆盖状态有准确值；JSON结算日历未知字段、重复日期、source/year缺失均拒绝。
- [ ] **Step 5: 提交。** `git commit -m "feat: ingest read-only equity market data"`。

## Task 8: SEC 按申报版本的财报接入

**Files:** Create `backend/internal/modules/investment/adapters/outbound/sec/` 中 `financials.go`、`mapping.go`及测试、`testdata/submissions.json`、`testdata/companyfacts.json`；Create `application/ports/financial_data.go`、`application/command/sync_financials.go`及测试；Extend `application/dto/sync.go`、fixture适配器、postgres/data.go；Modify `config.go`、`config_investment_test.go`、`.env.example`、`compose.yaml`增加 `INVESTMENT_SEC_USER_AGENT` 和版本化SIC映射配置引用。

**Interfaces:** `FinancialDataPort.Facts(ctx,ciks []string,asOf time.Time)([]domain.FinancialFact,error)`、`Company(ctx,cik string)(domain.Instrument,error)`；`sec.NewFinancials(client *http.Client,cfg SECConfig)(*Financials,error)`，`SECConfig{BaseURL,UserAgent string,Timeout time.Duration}`；`SyncFinancialsHandler.Handle(ctx,dto.SyncRequest)(dto.SyncStatus,error)`；`SnapshotStore.AppendFinancials(ctx,datasetVersion string,facts []domain.FinancialFact)error`。

- [ ] **Step 1: 写失败测试。** `TestSECKeepsAccessionAndRevisionTime`：2026-03-01申报的Q4=100、2026-04-01修订Q4=80，两条都留，3月15日NormalizeTTM不能读80；`TestSECTickerChangeKeepsIdentity`通过CIK/交易所及providerID证据对应到稳定instrumentId，冲突不猜映射。`TestSECRejectsUnitsAndCustomConceptAmbiguity`单位不符、无法明确使用的custom taxonomy返回missing。`TestSECPublicationDateUsesNextSession`只有filed日期从下一session可用。`TestSECRequestsBoundedAndIdentified`：User-Agent存在、默认读取速率<=2req/s、分页/429/超时有界。
      核心断言（变量由上述固定场景的执行结果赋值）：

```go
if marchSnapshot.Facts[0].Value != "100" { t.Fatal("read April revision in March") }
if oldInstrument.ID != renamedInstrument.ID { t.Fatal("ticker changed identity") }
```

- [ ] **Step 2: 跑失败证明。** `go test ./backend/internal/modules/investment/adapters/outbound/sec ./backend/internal/modules/investment/application/command -run 'TestSEC' -v`；时间/限流器由client测试注入，不阻塞真实长等待。
- [ ] **Step 3: 实现。** 先读取submissions查accession/accepted或filed时刻，再解析与该accession关联的历史companyfacts，保存原始value单位及版本；无法从聚合记录确定历史值时读取对应申报事实或明确拒绝，而不是套用当前汇总值。支持普通US-GAAP企业的明确映射，annual/ytd交给Task3还原；CIK冲突进入数据质量状态。SIC采用显式表而非大模型猜行业；导入关联事实同时写instrument事实版本，不能改写旧快照ticker或SIC。
- [ ] **Step 4: 验证。** 同一命令PASS；将历史源中新增修订条目后重评先前snapshot仍一致；Postgres facts/source唯一键保留两份版本；fixture覆盖SEC网络不可用状态。
- [ ] **Step 5: 提交。** `git commit -m "feat: ingest point-in-time SEC financial statements"`。

## Task 9: 新闻主题、分析证据和数据状态查询

**Files:** Create `backend/internal/modules/investment/application/ports/news.go`、`application/dto/analysis.go`、`application/query/analysis.go`、`data_status.go`、`application/command/sync_news.go`及测试；Create `adapters/outbound/alpaca/news.go`及测试；Create `domain/news.go`及测试；Extend fixture、postgres/data.go、config测试与env/Compose配置 `INVESTMENT_NEWS_ENABLED` 默认false（fixture的演示新闻仍可读）。

**Interfaces:** `NewsPort.Items(ctx,instrumentIDs []string,from,to time.Time)([]domain.NewsItem,error)`；`AggregateTopics(items []NewsItem,asOf time.Time)([]Topic,error)`；`AnalysisQuery.Handle(ctx,dto.AnalysisRequest)(dto.AnalysisView,error)`；`DataStatusQuery.Handle(ctx,scope domain.Scope,mode string)(dto.DataStatus,error)`。`AnalysisRequest{Scope,InstrumentID,AccountID string,AsOf time.Time}`；`AnalysisView{Instrument,AsOf,Mode,Metrics,Signal,Risk,Recommendation,NewsStatus,Topics,QualityFlags}`；`SyncNewsHandler.Handle(ctx,dto.SyncRequest)(dto.SyncStatus,error)`。AccountID缺省给研究建议，存在时交给Task10风控约束，必须校验owner。

- [ ] **Step 1: 写失败测试。** `TestTopicWindowAndArticleDedup`：asOf之前7自然日、24小时变化、同文章ID去重，未来新闻不计入；`TestAnalysisHighPotentialHighRiskIndependent`同时保留两个标签和因子证据；`TestNewsUnavailableIsNotEmptyCoverage`关闭/401/超时分别返回not_configured/unavailable，不伪装0条新闻。`TestNewsDoesNotChangeTradingSignal`改变新闻数量不改变Task4信号；`TestAnalysisAccountScope`另一owner账户返回notfound。
      核心断言（变量由上述固定场景的执行结果赋值）：

```go
if beforeNews.Signals[0].Score != afterNews.Signals[0].Score { t.Fatal("news changed factor score") }
if newsStatus == "complete" { t.Fatal("unavailable news reported complete coverage") }
```

- [ ] **Step 2: 跑失败证明。** `go test ./backend/internal/modules/investment/domain ./backend/internal/modules/investment/application/query ./backend/internal/modules/investment/adapters/outbound/alpaca -run 'Test(Topic|Analysis|News)' -v`。
- [ ] **Step 3: 实现。** 按供应商明确关联证券归类，固定关键词版本映射到财报/并购/产品/监管/宏观等主题，标题+供应商摘要+原链接，不生成真实性或概率。禁用全文抓取；新闻端点权限/授权保存范围人工实源验收前保持disabled，不把本地测试称实源已连接。数据状态分market/financial/news/calendar模块，显示最近同步/覆盖/feed/错误原因；prices完整不代表financial/news完整。
- [ ] **Step 4: 验证。** 同一命令PASS；fixture在无凭据时提供明确演示新闻；数据状态503与空集合不同；报告中不泄露服务端URL/key。
- [ ] **Step 5: 提交。** `git commit -m "feat: explain stock research with financial and news evidence"`。

## Task 10: 独立风控和确定性日线调仓规划

**Files:** Create `backend/internal/modules/investment/domain/risk.go`、`rebalance.go`及测试；Extend `domain/account.go`定义Position、RiskPolicy、PortfolioInput；Extend `application/query/analysis.go`应用账户风控结论。

**Interfaces:** `Position{InstrumentID,Quantity,ReservedQuantity,CostBasis Money}`，CostBasis为剩余持仓含买费的总成本；`PortfolioInput{Account,Positions,Snapshot,Evaluation,Policy,NAV,PeakNAV,SessionTurnover}`；`BuildRebalance(input PortfolioInput)(RebalancePlan,error)`；`ValidateOrder(input OrderRiskInput)(RiskDecision,error)`；`OrderRiskInput{Account,Positions,InstrumentID,Side,Quantity,Price,NAV,Policy,SessionTurnover,Automatic bool}`；`RebalancePlan{Orders []PlannedOrder,Pause bool,Reasons []string}`，PlannedOrder有InstrumentID/Side/Quantity/Reason；`RiskDecision{Allowed bool,ReasonCode string,MaxQuantity Quantity}`。

- [ ] **Step 1: 写失败测试。** `TestRiskCapsBothManualAndAutomatic`：NAV100,000、单股10,000、行业30,000、总额80,000，下一买单越任一上限则拒绝/缩量；卖出未结算款不可用于买入。`TestRebalancePriorityAndBand`：收盘止损10%先卖、score49.9卖、50/69.9持有不加、70可入、差<2百分点不调；高风险不自动入选；top10不足可留现金。`TestDrawdownPauseKeepsPositions`：peak100,000/NAV85,000触发暂停、不隐含强制卖出；普通换手20%边界接受，超出仅止损/减风险卖单允许并记reason。
      核心断言（变量由上述固定场景的执行结果赋值）：

```go
if !drawdownPlan.Pause || len(drawdownPlan.Orders) != 0 { t.Fatal(drawdownPlan) }
if excessiveBuy.Allowed { t.Fatal("accepted an order above the risk cap") }
```

- [ ] **Step 2: 跑失败证明。** `go test ./backend/internal/modules/investment/domain ./backend/internal/modules/investment/application/query -run 'Test(RiskCaps|Rebalance|Drawdown)' -v`。
- [ ] **Step 3: 实现。** 先减风险/止损，再保留50–69.9中间持仓占用，最后按稳定排名分配高分候选，每只最多10%、总最多80%，受行业/现金/双边换手约束；无足够候选不改变因子权重。手动订单也受硬仓位/资金限制但不要求量化入选，Automatic=true另要求合格排名且非高/未知风险。计划只返回确定性结果，不改余额；现有市场波动超限不反向改历史，只阻止加仓并规划减仓。
- [ ] **Step 4: 验证。** 同一命令PASS；随机但固定seed输入验证总目标不超80%、预留不负、排序稳定；缺最新价格不生成估算订单，排名不足可保留具明确依据的减仓。
- [ ] **Step 5: 提交。** `git commit -m "feat: enforce portfolio risk limits and daily rebalancing"`。

## Task 11: 订单、日线撮合与原子账本

**Files:** Create `backend/internal/modules/investment/domain/execution.go`、`ledger.go`及测试；Create `application/command/place_order.go`、`cancel_order.go`、`execute_orders.go`及测试；Extend `application/command/configure_automation.go`，Create `application/dto/orders.go`；Extend `application/ports/stores.go`、`adapters/outbound/postgres/orders.go`、`ledger.go`、`accounts.go`及集成测试。

**Interfaces:** `MatchAtOpen(input MatchInput)(FillResult,error)`；MatchInput有Order、OpenPrice Price、AvailableCapacity Quantity、Portfolio PortfolioInput、RecordedAt时间；FillResult有Fill *Fill、CancelledQuantity数量及Reason string，Fill保存Quantity/Price/Gross/Fee/EffectiveAt/RecordedAt。`ApplyFill(account Account,positions []Position,fill Fill)(LedgerMutation,error)`，LedgerMutation有Account、Positions和Entries，不写DB。`PlaceOrderHandler.Handle(ctx,dto.PlaceOrderRequest)(dto.OrderView,error)`，请求Mutation/AccountID/InstrumentID/Side/Quantity十进制字符串；`CancelOrderHandler.Handle(ctx,dto.CancelOrderRequest)(dto.OrderView,error)`；`ExecuteOrdersHandler.Handle(ctx,dto.ExecuteOrdersRequest)(dto.ExecutionResult,error)`，请求Scope/AccountID/SessionDate/FinalAttempt。Store增加LockOrder、ListPending、SaveOrder、InsertFill、LoadPositions、SavePositions及SessionCapacityUsed，全部ctx/Scope/accountID并加入ambient事务。Create `application/command/reserve_orders.go`定义`ReservationWriter.Apply(ctx context.Context,scope domain.Scope,account domain.Account,orders []domain.Order)(domain.Account,error)`，只在既有事务中写预留/orders/ledger；PlaceOrder与Task13 Evaluate复用，不嵌套UnitOfWork。

- [ ] **Step 1: 写失败测试。** `TestFillCostsAndGapUsesOnlyReservation`：买10股、开盘100→price100.100000、gross1001.00、fee0.11，总1001.11；按这个数预留，开盘120时只可成交8股，金额961.06，退回40.05，不消费其他订单预留。`TestSharedCapacityCannotBeSplitAround`：前20日均量1,000，同账户多订单合计最多10股；不读取目标日全天volume。`TestPauseAfterOpenCannotUndoAwaitingBar`：开盘前可cancel、到了open自动推进awaiting_bar且手动撤销409。`TestFillTransactionCrashAndRace`：两个执行/暂停竞争最多一条fill、余额/仓位/预留守恒，提交后重试返回已有结果。

```go
if fill.Price != Price(100100000) || fill.Gross != Money(100100) || fill.Fee != Money(11) { t.Fatal(fill) }
if gapFill.Quantity != Quantity(8) || gapFill.Gross+gapFill.Fee != Money(96106) { t.Fatal(gapFill) }
```

- [ ] **Step 2: 跑失败证明。** `go test ./backend/internal/modules/investment/domain ./backend/internal/modules/investment/application/command ./backend/internal/modules/investment/adapters/outbound/postgres -run 'Test(Fill|SharedCapacity|PauseAfter)' -v`；数据库case必须有真实测试DB才算PASS。
- [ ] **Step 3: 实现。** 预留available→reserved，lock账户后按instrumentId顺序lock订单/仓位；到生效时间先推进状态，取消只允许尚未生效。开盘价+滑点重算Task10资金/仓位限制，容量与预留取最小整股数，部分成交立即取消余量。gross半入、fee向上到分且至少0.01；卖出释放预留股数、净款进unsettled，平均成本卖出分摊按半入、最后一股带走剩余成本防尾差。fill/ledger/position/order/release同事务；market stale/无开盘价不猜价，延迟至规定deadline再expired。
- [ ] **Step 4: 验证。** 同一命令PASS，`go test ./backend/internal/modules/investment/... -race`；重复fill业务键不重复写账，cancel/execute并发结果按服务端clock+锁确定。更新启停命令：取消未生效订单，提示已生效等待确认；配置变动取消旧版本未生效买单，不撤回已生效事实。
- [ ] **Step 5: 提交。** `git commit -m "feat: execute paper orders with atomic reservation accounting"`。

## Task 12: 结算、拆股和分红的单次应用

**Files:** Create `backend/internal/modules/investment/domain/settlement.go`、`corporate_actions.go`及测试；Create `application/command/reconcile_account.go`及测试、`application/dto/reconcile.go`；Extend `application/ports/stores.go`、`adapters/outbound/postgres/ledger.go`及集成测试。

**Interfaces:** `SettleReceivables(account Account,entries []LedgerEntry,calendar Calendar,now time.Time)(LedgerMutation,error)`；`ApplyCorporateAction(account Account,positions []Position,action CorporateAction,eligibility []Position)(LedgerMutation,error)`；`ReconcileAccountHandler.Handle(ctx,dto.ReconcileRequest)(dto.ReconcileResult,error)`，请求Scope/AccountID/Through time.Time；`LedgerStore.ClaimEvent(ctx,Scope,accountID,eventKey string)(bool,error)`、`Unsettled(ctx,Scope,accountID)([]LedgerEntry,error)`、`ActionEligibility(ctx,Scope,accountID,actionID)([]Position,error)`。

- [ ] **Step 1: 写失败测试。** `TestSettlementSkipsNonSettlementDay`：交易开市但结算关闭时卖出款仍unsettled，下一结算日才可用；补记晚到成交不把原结算日期顺延，也不能用于过去新单。`TestSplitPreservesCostAndCancelsPending`：10股总成本1000、2:1拆股→20股/总成本1000/均价50；pending取消，awaiting若核对出事前拆股按公司行动原因取消。`TestSplitFractionWithoutCashDataBlocks`拒绝自行舍掉半股。`TestDividendEligibilityAndIdempotence`：除息前持有10股、每股1美元→应收10、支付日available+10/应收-10，重复重放无第二次收入。
      核心断言（变量由上述固定场景的执行结果赋值）：

```go
if splitPositions[0].Quantity != 20 || splitPositions[0].CostBasis != Money(100000) { t.Fatal(splitPositions) }
if paidAccount.Balances.Dividends != 0 || dividendPaymentCount != 1 { t.Fatal("dividend duplicated") }
```

- [ ] **Step 2: 跑失败证明。** `go test ./backend/internal/modules/investment/domain ./backend/internal/modules/investment/application/command ./backend/internal/modules/investment/adapters/outbound/postgres -run 'Test(Settlement|Split|Dividend)' -v`。
- [ ] **Step 3: 实现。** 以原effectiveAt排序补记、保存除息资格快照、唯一eventKey与科目划转同事务；公司行动在该日open前处理，缺关键字段冻结该证券执行和可信绩效。持仓分红应收纳入NAV，但支付不再新增收益；结算仅unsettled→available，不改NAV。不支持的退市/并购明确blocking状态，不凭0价格清仓。Calendar未覆盖年份不能执行结算或自动交易。
- [ ] **Step 4: 验证。** 同一命令PASS；重启/并发应用公司行动和结算仍仅一组流水；账户的四现金科目与仓位市值总和等于NAV，无分红双计。
- [ ] **Step 5: 提交。** `git commit -m "feat: reconcile paper settlements splits and dividends"`。

## Task 13: 每日评估、恢复调度和 Worker 组合

**Files:** Create `backend/internal/modules/investment/application/command/evaluate_account.go`、`tick.go`、`start_sync.go`及测试；Create `application/dto/jobs.go`、`application/ports/scheduler.go`；Create `adapters/outbound/river/scheduler.go`及测试、`adapters/inbound/worker/worker.go`及测试；Extend postgres/accounts.go、runs.go；Create `backend/cmd/worker/investment.go`、`investment_test.go`，Modify `backend/cmd/worker/main.go`仅注册investment queue/worker/periodic job。jobs.go同时定义RunView（ID/Status/Phase/ErrorCode/Reason字符串、CreatedAt/UpdatedAt时间）和AccountRef（Scope、AccountID）。

**Interfaces:** `EvaluateAccountHandler.Handle(ctx,dto.EvaluateRequest)(dto.EvaluationView,error)`；EvaluateRequest有Scope domain.Scope、AccountID/Purpose字符串、SessionDate时间、Mutation dto.Mutation、FinalAttempt bool，Purpose=`research|automatic`；`TickHandler.Handle(ctx,now time.Time)error`；`dto.InvestmentJobArgs{JobType,WorkspaceID,OwnerUserID,AccountID,RunID string; SessionDate time.Time}`和`Kind() string`返回`investment_job`，不在应用包导入River；`JobScheduler.Enqueue(ctx,args dto.InvestmentJobArgs)(int64,error)`。AccountStore的`WorkDue(ctx,now time.Time)([]dto.AccountRef,error)`是仅供可信后台的跨scope扫描，返回带Scope的最小引用；所有业务读取仍用scope锁。RunStore的`ClaimEvaluation(ctx,Scope,accountID string,session time.Time,strategyVersion,mode string)(dto.EvaluationClaim,error)`返回冻结snapshot/已完成结果；发布订单另加唯一(account,session)守卫。`StartSyncHandler.Handle(ctx,dto.SyncRequest)(dto.RunView,error)`在短MutationExecutor事务保存data_sync_runs中的scope/模式/日期/证券ID并排队；Worker依RunID读冻结请求后才调用Task7–9同步，外部读取不置于MutationExecutor内。`InvestmentWorker.Work(ctx,*river.Job[dto.InvestmentJobArgs])error`分派sync、evaluate、execute、reconcile，始终传完整scope。

- [ ] **Step 1: 写失败测试。** `TestDailyEvaluationFreezesInputAcrossRetries`：同(account,session,strategy,mode)两个worker只一run/一批order，重试遇新数据仍使用旧snapshot。`TestMissedOpenDoesNotBackdateNewOrders`：超过下一open只能研究，不补发过去开盘订单。`TestPendingPreviousFillBlocksRebuy`：上次awaiting_bar不能视为空仓。`TestTickEarlyCloseDSTAndRecovery`：close+29m不执行、+30m可执行；每15m补偿仍唯一，半日市/夏令时采用calendar。`TestWorkerPauseStillReconcilesEffectiveOrders`：pause后不评估发新单，但旧生效订单/资金仍能确认。`TestInvestmentCompositionFixtureOnly`本地依赖禁任何外网请求。
      核心断言（变量由上述固定场景的执行结果赋值）：

```go
if len(evaluationRuns) != 1 || len(orderBatches) != 1 { t.Fatal("duplicate daily run") }
if len(backdatedNewOrders) != 0 { t.Fatal("issued new orders after their intended open") }
```

- [ ] **Step 2: 跑失败证明。** `go test ./backend/internal/modules/investment/application/command ./backend/internal/modules/investment/adapters/inbound/worker ./backend/internal/modules/investment/adapters/outbound/river ./backend/cmd/worker -run 'Test(DailyEvaluation|MissedOpen|PendingPrevious|Tick|WorkerPause|InvestmentComposition)' -v`。
- [ ] **Step 3: 实现。** 组合复用现有River client，不另建自研队列；periodic Tick只发现到期工作并持久排队。执行顺序：补齐此前已生效成交→结算/公司行动→激活到期PendingConfig→NAV→冻结snapshot→Evaluate→BuildRebalance→ReservationWriter在同事务插orders/预留/建议/run；不调用会自行开始事务的PlaceOrderHandler，provider读取不在账户DB锁内。应用最终提交再次确认enabled、expected account config、deadline和无旧未决输入。FinalAttempt后写可见failed业务run，retry策略最多5次、500ms倍增上限60s；非重试配置/覆盖错误直接block并记录原因，不吞掉为成功。同步run状态供owner-scoped data-status查询，已接受202任务的错误不会伪装为已同步。
- [ ] **Step 4: 验证。** 同一命令PASS且 `go test ./backend/internal/modules/investment/... ./backend/cmd/worker -race`；本地DB验证River InsertTx参加ambient事务，事务回滚不留下任务；worker断点模拟commit前后恢复最多一批订单。已有reminder worker/heartbeat不受影响。
- [ ] **Step 5: 提交。** `git commit -m "feat: run recoverable daily investment automation"`。

## Task 14: 隔离历史回测、基准和绩效

**Files:** Create `backend/internal/modules/investment/domain/backtest.go`、`performance.go`及测试；Create `application/command/create_backtest.go`、`run_backtest.go`及测试、`application/dto/backtests.go`；Extend `application/ports/stores.go`、postgres/runs.go、Task13 worker分派。

**Interfaces:** `Replay(input BacktestInput)(BacktestResult,error)`；BacktestInput有Snapshots []Snapshot、Calendar Calendar、Universe UniverseVersion、Strategy StrategyVersion、InitialCash Money、From/To时间、BenchmarkID字符串；BacktestResult有Trace []ReplayEvent、Curve/BenchmarkCurve []NAVPoint、Metrics Performance及QualityFlags；ReplayEvent保存effectiveAt/recordedAt/Order/Fill/ledger事实。`ComputePerformance(curve []NAVPoint,fills []Fill,initial Money)(Performance,error)`；`NAVPoint{SessionDate time.Time; NAV Money}`；Performance含CumulativeReturn/MaxDrawdown/Turnover float64、AnnualReturn/Sharpe *float64及MissingReasons。`CreateBacktestHandler.Handle(ctx,dto.CreateBacktestRequest)(dto.RunView,error)`，request携带Mutation/版本/日期/InitialCash；`RunBacktestHandler.Handle(ctx,dto.RunBacktestRequest)(dto.RunView,error)`，request有Scope/RunID/FinalAttempt。RunStore的`InsertBacktest(ctx,Scope,dto.BacktestRecord)error`、`LockBacktest(ctx,Scope,id)(dto.BacktestRecord,error)`、`SaveBacktest(ctx,Scope,dto.BacktestRecord,expectedVersion int)error`；输入配置和snapshotID集合在创建时冻结。

- [ ] **Step 1: 写失败测试。** `TestReplayCannotUseNextCloseOrFutureFacts`：改变D+1最高/最低/收盘/全天volume与未来财报，D生成订单及D+1开盘成交数量/价格不变。`TestBacktestIsolatesPaperAccount`同源重放不写持续账户/订单；`TestPerformanceExactThresholds`：NAV100→120→90，累计-10%、maxDD25%；少于252日不年化、少于60个收益不Sharpe、零std不适用。`TestBenchmarkSameFeesAndDividendBasis`：SPY首执行日开盘买入+费用/滑点+分红，余款现金；价格only基准不能冒充总回报。`TestRejectIncompleteHistory`预热200根/执行19日/缺公司行动或退市终值拒绝，当前股票池标记幸存者偏差。
      核心断言（变量由上述固定场景的执行结果赋值）：

```go
if math.Abs(performance.CumulativeReturn+0.10) > 1e-9 || math.Abs(performance.MaxDrawdown-0.25) > 1e-9 { t.Fatal(performance) }
if shortWindow.AnnualReturn != nil || shortWindow.Sharpe != nil { t.Fatal("invented short-window metrics") }
```

- [ ] **Step 2: 跑失败证明。** `go test ./backend/internal/modules/investment/domain ./backend/internal/modules/investment/application/command -run 'Test(Replay|Backtest|Performance|Benchmark|RejectIncomplete)' -v`。
- [ ] **Step 3: 实现。** Replay在内存使用Task10/11/12规则，以event日历和可知快照按时间顺序推进；不创建真实Account rows。绑定固定数据/参数/股票池/成本版本，停止/故障写run失败而非拼出曲线。SPY基准也是整数股、同费用/滑点，按规则收分红，不支持基准时只取消比较结果。按spec§12定义年化、零无风险Sharpe和双边换手，不新增“胜率”。保存research/test区间标签，不能把全期调参结果标成样本外。
- [ ] **Step 4: 验证。** 同一命令PASS；同一固定输入两次Replay的trace和曲线一致；逐日持续fixture执行的同版本订单/账本与Replay一致；创建实验+排队原子、重复job只有一套结果。
- [ ] **Step 5: 提交。** `git commit -m "feat: backtest investment strategies with isolated ledgers"`。

## Task 15: 公共 HTTP 契约、查询与 API 接线

**Files:** Create `contracts/openapi/investment.yaml`、`tests/contract/investment_contract_test.go`；Create `backend/internal/modules/investment/application/query/accounts.go`、`lists.go`、`performance.go`及测试；Extend `application/dto/views.go`；Create `adapters/inbound/http/` 中 `handler.go`、`accounts.go`、`research.go`、`analysis.go`、`data.go`、`json.go`及测试；Create `backend/cmd/api/investment.go`、`investment_integration_test.go`，Modify `backend/cmd/api/wiring.go`。

**Interfaces:** `http.Handler`消费前序命令/查询的窄Handle接口，`http.RegisterRoutes(mux *http.ServeMux,auth func(http.Handler)http.Handler,h *Handler)`；`AccountsQuery.Handle(ctx,Scope,id)(dto.AccountView,error)`、`ListQuery.Handle(ctx,dto.ListRequest)(dto.Page,error)`、`PerformanceQuery.Handle(ctx,Scope,id)(dto.PerformanceView,error)`。`ListRequest{Scope,Resource,ParentID,Cursor string,Limit int}`；`Page{Items json.RawMessage,NextCursor string}`，Resource是封闭枚举，owner加入query而非过滤已截断数据。`AccountView`有十进制字符串的四科目/NAV、mode/feed/asOf/version/auto及block状态；OrderView有state/effectiveAt/recordedAt/价格/费用/原因。

- [ ] **Step 1: 写失败测试。** `TestInvestmentContractRoutesAndDecimalSchemas`覆盖spec§11全部方法/路径与schema，OpenAPI3.1.1、closed objects。`TestInvestmentHTTPAuthScopeAndStrictBody`401/跨owner404、body owner/未知字段/数字金额/超精度/超长请求拒绝；`TestInvestmentIdempotencyAfterTimeout`重复create/order/toggle/backtest只一结果，body不同409。`TestInvestmentCursorNoDuplicates`25默认/100上限，同排序时间id稳定，换parent/owner的cursor422；`TestInvestmentServiceFailureNotEmptyAccount`503不是零余额。
      核心断言（变量由上述固定场景的执行结果赋值）：

```go
if otherOwnerResponse.Code != http.StatusNotFound { t.Fatal(otherOwnerResponse.Code) }
if changedBodyResponse.Code != http.StatusConflict { t.Fatal(changedBodyResponse.Code) }
```

- [ ] **Step 2: 跑失败证明。** `go test ./tests/contract ./backend/internal/modules/investment/adapters/inbound/http ./backend/internal/modules/investment/application/query ./backend/cmd/api -run 'TestInvestment' -v`。
- [ ] **Step 3: 实现。** 按spec完整注册data-status/sync、instruments/analysis、universes/versions、strategies/versions、accounts/positions/ledger/performance、orders/cancel、automation/evaluations、backtests。创建资源201，异步sync/backtest/evaluation202+runId，查询200，未认证401，越权404，版本/幂等/生效后撤单409，数据/输入不满足422，provider暂不可用503；已接受异步任务的最终失败通过run status体现。钱/价格/整数股为字符串：余额/价格非负，ledger变化可有负号；评分/风险/quality分开返回。所有修改要求Idempotency-Key，缺失422；源同步读取当前全局配置和owner股票池，不允许客户端提交provider URL/secret。只在cmd选择provider和TxRunner，API只InsertTx，worker执行任务。
- [ ] **Step 4: 验证。** 同一命令PASS；全私有GET/PATCH/POST均校验session/owner；查询游标包含封闭资源、parent和最后排序值并校验作用域，offset不作为公开cursor。composition证明Investment和旧todo/conversation/reminder均能使用原auth，新模块未反向进入platform。
- [ ] **Step 5: 提交。** `git commit -m "feat: expose scoped investment APIs and contracts"`。

## Task 16: Web 数据状态、股票池和个股分析

**Files:** Create `apps/web/src/features/investment/` 中 `types.ts`、`fetch-investment.ts`、`fetch-investment.test.ts`、`research-panel.tsx`、`research-panel.test.tsx`、`stock-analysis.tsx`、`stock-analysis.test.tsx`、`universe-form.tsx`、`universe-form.test.tsx`、`status-badge.tsx`；Create `apps/web/src/app/(workbench)/investment/page.tsx`、`stocks/[instrumentId]/page.tsx`；Modify `apps/web/src/app/globals.css`添加investment前缀selector，不改globals-css.test.ts门槛。

**Interfaces:** `InvestmentClient`封装`request<T>(path,options,decode):(Promise<Outcome<T>>)`，Outcome={ok:true,value:T}|{ok:false,code,correlationId?}，parse十进制字符串不强制转换Number；`createInvestmentClient(baseURL string,fetcher typeof fetch): InvestmentClient`及各route调用。组件`ResearchPanel({client,mode})`、`StockAnalysis({client,instrumentId,accountId?})`、`UniverseForm({client,onCreated})`，具体view shape按Task15契约。客户端请求只同源`/api/v1/investment`，server页复用shared/server session配置，不向client传Compose名或API internal URL。

- [ ] **Step 1: 写失败测试。** Vitest `shows demo/feed/asOf visibly`、`shows high potential and high risk together`、`renders unavailable PE and news reason`、`universe rejects 101 members and retains retry draft`、`late analysis response cannot overwrite another stock/account`、`does not treat 503 as expired session`。核心断言：

```tsx
expect(screen.getByText("潜力较高")).toBeVisible();
expect(screen.getByText("风险高")).toBeVisible();
expect(screen.getByText("演示数据")).toBeVisible();
expect(screen.getByText("市盈率不适用：盈利非正")).toBeVisible();
```

- [ ] **Step 2: 跑失败证明。** `corepack pnpm --filter @artificial-brain/web test -- src/features/investment/fetch-investment.test.ts src/features/investment/research-panel.test.tsx src/features/investment/stock-analysis.test.tsx src/features/investment/universe-form.test.tsx`，未实现组件而FAIL。
- [ ] **Step 3: 实现。** 执行前使用design-taste-frontend审视现有workbench样式，采用现有中文导航、表格/证据层级和紧凑布局，不新建无关营销设计。排名列表、risk/potential过滤、选股池/参数版本表单、事实来源、热点外链接、数据显示缺失/失败重试。每次查询带AbortController/请求代次；切对象忽略迟到响应。source warning常驻、潜力/风险各有文字可访问名称；固定图例，键盘可操作，空态引导真实模式先配置股票池。
- [ ] **Step 4: 验证。** 同一Vitest PASS；`corepack pnpm --filter @artificial-brain/web lint`和globals-css gate通过；390px/小笔记本宽度人工浏览验证无水平溢出、证据可读、键盘focus清楚。可用fixture浏览验证不声称真实provider正确。
- [ ] **Step 5: 提交。** `git commit -m "feat: add investment research and analysis screens"`。

## Task 17: Web 账户、订单与自动交易启停

**Files:** Create `apps/web/src/features/investment/` 中 `account-panel.tsx`、`account-panel.test.tsx`、`account-form.tsx`、`account-form.test.tsx`、`order-form.tsx`、`order-form.test.tsx`、`automation-form.tsx`、`automation-form.test.tsx`；Create `apps/web/src/app/(workbench)/investment/accounts/[accountId]/page.tsx`；Extend `fetch-investment.ts`及测试和globals.css。

**Interfaces:** `AccountPanel({client,accountId})`、`AccountForm({client,onCreated})`、`OrderForm({client,accountId,onSubmitted})`、`AutomationForm({client,account,onChanged})`。client为一次用户意图生成一个crypto.randomUUID幂等键，网络不确定/重试保留该key及原body；body更改意味着新意图/新key。自动配置发送expectedVersion，收到409重新加载配置但不静默重发新写入。

- [ ] **Step 1: 写失败测试。** `double click and timeout retry use same key`断言两次请求同Idempotency-Key/相同body、只有一次余额更新；`switch account ignores old response`；`pause shows awaiting bar still may record`；`reserved and unsettled amounts remain separate`；`old config conflict cannot overwrite current version`；`order before open can cancel and after open cannot cancel`。订单状态中文包括“待生效”“已生效，等待日线确认”“部分成交，余量取消”“行情缺失已过期”，不把提交成功展示“已买入”。
      核心断言（变量由上述固定场景的执行结果赋值）：

```tsx
expect(requestKeys[0]).toBe(requestKeys[1]);
expect(screen.getByText("已生效，等待日线确认")).toBeVisible();
```

- [ ] **Step 2: 跑失败证明。** `corepack pnpm --filter @artificial-brain/web test -- src/features/investment/account-panel.test.tsx src/features/investment/account-form.test.tsx src/features/investment/order-form.test.tsx src/features/investment/automation-form.test.tsx`。
- [ ] **Step 3: 实现。** 建立账户、显示四科目/NAV/持仓/流水、owner账户切换、带原因的订单列表和分页，表单整数股与精确字符串校验。启用页展示已批准默认风控/策略版本/日线延迟说明，并提供启用/暂停，不加每笔确认。等待任务定期查询仅在页面可见时运行、组件销毁停止；交易失败保留输入、明确重试，401走现有session-recovery，503保留当前账户/草稿。
- [ ] **Step 4: 验证。** 同一Vitest PASS；慢请求与账户切换/暂停受控交错均正确；lint与相关CSS gate通过；浏览器fixture走完整创建→启用→观察pending→生效→fill→暂停路径，手动订单正常。
- [ ] **Step 5: 提交。** `git commit -m "feat: manage simulated portfolios and automation in web"`。

## Task 18: Web 回测、绩效图表与工作台导航

**Files:** Create `apps/web/src/features/investment/` 中 `backtest-panel.tsx`、`backtest-panel.test.tsx`、`performance-chart.tsx`、`performance-chart.test.tsx`、`performance-panel.tsx`、`performance-panel.test.tsx`；Create `apps/web/src/app/(workbench)/investment/research/page.tsx`；Extend `account-panel.tsx`集成绩效、`fetch-investment.ts`及测试；Modify `apps/web/src/app/(workbench)/workbench-shell.tsx`、`workbench-shell.test.tsx`、globals.css。

**Interfaces:** `BacktestPanel({client})`；`PerformancePanel({client,resource:"account"|"backtest",id})`；`PerformanceChart({series,benchmarkSeries?,title})`，series为精确金额原串+RFC3339日期，显示标签保持原数值；SVG坐标映射用有界相对值，不用Number余额参与交易。PNG/PDF导出不在首版范围。

- [ ] **Step 1: 写失败测试。** `shows backtest separate from forward paper performance`；`insufficient history never draws fake curve`；`short history hides annualized and Sharpe figures with reason`；`missing benchmark shows cannot compare`；`single point empty and constant series render safely`验证无NaN/Infinity path；`investment subroutes keep navigation active`：/investment/stocks/id和/accounts/id同样突出投资菜单，不影响/根路由匹配。
      核心断言（变量由上述固定场景的执行结果赋值）：

```tsx
expect(chartMarkup).not.toContain("NaN");
expect(chartMarkup).not.toContain("Infinity");
expect(screen.getByText("无法比较基准：数据不完整")).toBeVisible();
```

- [ ] **Step 2: 跑失败证明。** `corepack pnpm --filter @artificial-brain/web test -- src/features/investment/backtest-panel.test.tsx src/features/investment/performance-chart.test.tsx src/features/investment/performance-panel.test.tsx 'src/app/(workbench)/workbench-shell.test.tsx'`；Windows shell传参使用实际双引号引住带括号路径。
- [ ] **Step 3: 实现。** 回测创建→202/runId→状态→独立曲线/参数/股票池/数据版本/费用/质量标记；曲线、回撤和持仓分布使用SVG/HTML并有等价文字表，颜色非唯一区分。导航添加“投资”，子路由prefix匹配只针对非根路径；最多100条页面数据有分页，不一次载全部流水。年化/Sharpe不适用显示原因，不显示0；幸存者偏差/覆盖不完整说明和真实数据权限在结果旁可见。
- [ ] **Step 4: 验证。** 同一Vitest PASS；web format:check/lint/test/build全通过。真实浏览器在桌面和390px验证股票池→分析→账户→回测链接与键盘操作，不用截图或fixture图表声称收益已验证。
- [ ] **Step 5: 提交。** `git commit -m "feat: visualize investment backtests and portfolio performance"`。

## Task 19: 完整闭环验收、运行手册和最终审查

**Files:** Create `tests/smoke/investment_test.sh`、`docs/runbooks/investment.md`、`docs/iterations/ITER-0006/acceptance.md`；Modify `tests/smoke/stack_test.sh`调用新的smoke脚本，Extend `tests/smoke/migration_test.sh`证明investment表及资金约束，Modify `README.md`增加入口/只读数据配置/模拟规则；完成`docs/iterations/ITER-0006/progress.md`及原yellow register交接说明。

**Interfaces:** stack中的full_stack_test在已分配端口后调用 `sh tests/smoke/investment_test.sh "$WEB_PORT" "$API_PORT"`；新脚本校验两个整数端口，使用127.0.0.1同源代理、复用现有fixture验证码登录方式自行取得两个临时session，不输出cookie，不启动第二套常驻stack。fixture连续交易的测试时钟通过应用Now注入，在专用Go composition/integration test中推进；不为smoke增加生产可访问的“任意改时间/余额”路由。shell只用已公开业务路由走用户路径、查询后台最终结果。

- [ ] **Step 1: 写失败验收。** smoke脚本创建两个用户的股票池/账户、检验越权404、查数据状态演示标签、人工提交订单/重试幂等、启用/暂停和创建回测；断言价格/资金是字符串、自动交易初始关闭、分析可以高潜力/高风险、真实数据模式未配置不伪造结果。Go composition scenario推进D→D+1→结算日，测试Worker重启和两实例竞争、split/dividend/迟到数据/限流的回放，不依赖真实时间多日等待。
      核心断言（变量由上述固定场景的执行结果赋值）：

```go
if fillCountAfterWorkerRestart != 1 || fillCountAfterConcurrentRetry != 1 { t.Fatal("duplicate economic event") }
if otherOwnerHTTPStatus != http.StatusNotFound { t.Fatal(otherOwnerHTTPStatus) }
```

- [ ] **Step 2: 跑失败证明。** 新scenario加入stack时先确认缺少预期行为确实FAIL，不以网络超时当成正确失败。`go test ./backend/cmd/api ./backend/cmd/worker -run 'TestInvestment' -v`及`make smoke-test`。
- [ ] **Step 3: 完成最小集成和文档。** 修复验收暴露的具体缺口，保持模块归属和依赖；手册给出fixture→实源配置、结算日历导入格式/来源/覆盖检查、新闻权限校验、停用/失败恢复、版本变更和每个默认风险阈值；只记录变量名/假值。实源验收单列“未配置/已验证”及feed/日期范围，不自动注册、采购、调用券商订单或提交凭据。
- [ ] **Step 4: 执行最终验证并记录证据。** `make toolchain-check`、`make harness-test`、`make verify`、`make migration-test`、`make smoke-test`。verify需包含Go race/vet/build、architecture、contracts、web format/lint/test/build和credential scanner；不重复通过的宽测试，除非后续修复改变相关行为。核验 `git diff <execution-base> -- go.mod package.json apps/web/package.json pnpm-lock.yaml deploy/migrations/001* deploy/migrations/002* deploy/migrations/003* deploy/migrations/004* deploy/migrations/005* deploy/migrations/006* deploy/migrations/007* deploy/migrations/008* deploy/migrations/009* deploy/migrations/010*`为空；核验CI、根Makefile、AGENTS未变。Windows make/脚本或Docker不可用时记录原失败及同目标直接验证结果，数据库SKIP绝非PASS，迁移/全栈验收未完成必须明确报告。
- [ ] **Step 5: 审查和提交。** 按用户所选执行方式完成独立审查，关注Review Focus五项及资金/订单时间语义；修复后只重跑相关证明。`git commit -m "test: verify the investment paper trading closed loop"`，报告已验证能力、实源配置情况和未完成环境gates，交付本地可审阅结果。除非用户另外要求，不合并/发布/部署。

## Coverage and Handoff

| Spec section         | 实现任务                         | 关键证据                                         |
| -------------------- | -------------------------------- | ------------------------------------------------ |
| §1–2目标与范围       | 1、4、13–19                      | fixture闭环、无实盘/Qlib假状态                   |
| §3架构/模块接口      | 1–19                             | architecture测试、cmd组合、既有业务回归          |
| §4数据/身份/可知时间 | 2、5、7–9                        | source/accession快照、模式/证券身份稳定          |
| §5因子               | 3–4                              | 精确权重、并列百分位、共同候选不足阻断           |
| §6分析/颜色/热点     | 3–4、9、16                       | 事实来源、风险独立、新闻不改交易评分             |
| §7账户/风控          | 1、6、10–13、17                  | 科目守恒、版本/限额、暂停和并发                  |
| §8撮合/结算/行动     | 11–12                            | 时钟边界、费用/预留/容量、结算和公司行动单次应用 |
| §9持久化/恢复        | 5–6、11–14                       | DB唯一键/原子事务、Worker commit前后故障         |
| §10调度/故障         | 2、7–9、13                       | 日历和30分钟/15分钟阈值、可见blocked/failed      |
| §11Web/公共契约      | 15–18                            | 全路径合约、owner、分页、完整中文用户路径        |
| §12回测/绩效         | 14、18                           | 无未来信息、账本隔离、同口径基准及缺失原因       |
| §13验收              | 19及各任务                       | 所有gates实际结果，实源单独验收                  |
| §14yellow登记        | 原register、1、5、7–9、13、15–19 | 追加schema/配置/接线/契约/样式，原red规则不变    |
| §15资料验证边界      | 7–9、19                          | 本地HTTP证明和手动实源证明分开                   |

计划自审检查：所有spec要求均映射到任务；精确接口/类型由对应任务定义；Review Focus逐项有测试；任务不是只建空目录/配置的提交。最终执行必须把真实red/green/verification/commit填入进度账本，本计划中的预期结果不是已经运行通过的证据。

本机计划编写环境已查到Go、Node、Corepack和Docker命令，未查到make；尚未运行数据库/业务测试，也未验证Docker engine状态。执行准备重新检测实际环境，不复用旧迭代的通过结论。

推荐 **Native（当前会话直接实现）**：这些任务紧密共享可知数据和资金账本接口，统一执行有利于保持口径，也减少每个任务重新加载上下文的开销；按该方式在全分支完成后安排独立审查。另一选项 **Subagent-driven** 会为每个任务安排实现者和审查者，更细致但增加上下文和审查成本。

交接要求：用户审阅本计划并选择执行方式后，Native使用superpowers:executing-plans，Subagent-driven使用superpowers:subagent-driven-development。在该确认之前不创建产品实现、不安装依赖、不改数据库；现阶段仅提交计划、设计确认状态和yellow预登记。
