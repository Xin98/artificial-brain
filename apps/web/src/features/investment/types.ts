// Public API types mirror contracts/openapi/investment.yaml. Decimal strings remain exact.
export type AccountConfig = {
  effectiveAt: string;
  policy: RiskPolicy;
  strategyVersionId: string;
  universeVersionId: string;
};
export type AccountView = {
  asOf: string;
  automationEnabled: boolean;
  blockReasons: string[];
  cash: CashView;
  createdAt: string;
  datasetVersion: string;
  feed: string;
  id: string;
  initialCash: Money;
  mode: string;
  name: string;
  nav: Money | null;
  pauseReason: string;
  pendingConfig: AccountConfig | null;
  policy: RiskPolicy;
  qualityFlags: string[];
  strategyVersionId: string;
  universeVersionId: string;
  version: number;
};
export type AccountsPage = {
  items: AccountView[];
  nextCursor: string;
};
export type AnalysisView = {
  accountRisk: RiskDecision | null;
  asOf: string;
  datasetVersion: string;
  feed: string;
  instrument: Instrument;
  metrics: FinancialMetrics;
  mode: string;
  newsReason: string;
  newsStatus: string;
  qualityFlags: string[];
  recommendation: Recommendation;
  risk: RiskAssessment;
  signal: Signal | null;
  topics: Topic[];
};
export type AutomationEventView = {
  effectiveAt: string;
  id: string;
  kind: string;
  reason: string;
  recordedAt: string;
};
export type AutomationRequest = {
  enabled: boolean;
  expectedVersion: number;
  mode?: string;
  policy: RiskPolicy;
  strategyVersionId: string;
  universeVersionId: string;
};
export type BacktestRequest = {
  expectedVersion?: number;
  from: string;
  initialCash?: Money;
  strategyVersionId: string;
  to: string;
  universeVersionId: string;
};
export type BacktestView = {
  benchmarkCurve: NAVPoint[];
  benchmarkMetrics: Performance | null;
  benchmarkReason: string;
  createdAt: string;
  curve: NAVPoint[];
  datasetVersion: string;
  errorCode: string;
  executionModel: string;
  feed: string;
  from: string;
  initialCash: Money;
  intervalLabel: string;
  metrics: Performance | null;
  mode: string;
  phase: string;
  qualityFlags: string[];
  reason: string;
  runId: string;
  snapshotIds: string[];
  status: string;
  strategyVersionId: string;
  to: string;
  universeVersionId: string;
  updatedAt: string;
};
export type BacktestsPage = {
  items: BacktestView[];
  nextCursor: string;
};
export type Balances = {
  available: SignedMoney;
  dividends: SignedMoney;
  reserved: SignedMoney;
  unsettled: SignedMoney;
};
export type CancelOrderRequest = {
  expectedVersion: number;
};
export type CashView = {
  available: Money;
  dividends: Money;
  reserved: Money;
  unsettled: Money;
};
export type CreateAccountRequest = {
  expectedVersion?: number;
  initialCash?: Money;
  mode?: string;
  name?: string;
  strategyVersionId?: string;
  universeVersionId?: string;
};
export type CreateStrategyRequest = {
  expectedVersion?: number;
  parameters: StrategyParameters;
  strategyId?: string;
};
export type CreateUniverseRequest = {
  expectedVersion?: number;
  instrumentIds: string[];
  mode?: string;
  name: string;
  universeId?: string;
};
export type DataComponentStatus = {
  asOf: string;
  count: number;
  from: string;
  reason: string;
  state: string;
  to: string;
};
export type DataStatus = {
  asOf: string;
  calendar: DataComponentStatus;
  datasetVersion: string;
  feed: string;
  financial: DataComponentStatus;
  lastSync: RunView | null;
  market: DataComponentStatus;
  mode: string;
  news: DataComponentStatus;
  qualityFlags: string[];
};
export type ErrorEnvelope = {
  code: string;
  correlationId: string;
  message: string;
};
export type EvaluateRequest = {
  expectedVersion?: number;
  purpose?: string;
};
export type EvaluationReadView = {
  accountId: string;
  asOf: string;
  datasetVersion: string;
  excluded: Exclusion[];
  id: string;
  issuedOrders: boolean;
  mode: string;
  orderIds: string[];
  purpose: string;
  qualityFlags: string[];
  reason: string;
  sessionDate: string;
  signals: Signal[];
  snapshotId: string;
  state: string;
  strategyVersionId: string;
  universeVersionId: string;
};
export type EvaluationsPage = {
  items: EvaluationReadView[];
  nextCursor: string;
};
export type EventsPage = {
  items: AutomationEventView[];
  nextCursor: string;
};
export type Exclusion = {
  instrumentId: string;
  reason: string;
};
export type FactorEvidence = {
  contribution: number;
  factRefs: string[];
  group: string;
  percentiles: number[];
  rawValues: number[];
  weight: number;
};
export type FillView = {
  effectiveAt: string;
  fee: Money;
  gross: Money;
  id: string;
  price: Price;
  quantity: Quantity;
  recordedAt: string;
  settlesAt: string | null;
};
export type FinancialMetrics = {
  debtRatio: Metric;
  indicators: Indicators;
  profitGrowth: Metric;
  revenueGrowth: Metric;
  roe: Metric;
  ttm: TTMFinancials;
  valuation: Valuation;
};
export type FinancialValue = {
  amount: string;
  currency: string;
  factRefs: string[];
  periodStart: string;
  reason: string;
  unit: string;
};
export type Indicators = {
  annualVolatility: Metric;
  averageTurnover20: Metric;
  averageVolume20: Metric;
  maxDrawdown: Metric;
  rsi14: Metric;
  sma20: Metric;
  sma200: Metric;
  sma50: Metric;
  windowDays: number;
};
export type Instrument = {
  cik: string;
  exchange: string;
  id: string;
  ingestedAt: string;
  kind: string;
  name: string;
  sic: string;
  source: string;
  sourceRecordId: string;
  ticker: string;
  tickerHistory: TickerChange[];
  tradable: boolean;
};
export type InstrumentResearchView = {
  instrument: Instrument;
  potential: string;
  price: Price | null;
  priceAsOf: string | null;
  reason: string;
  risk: RiskAssessment;
  signal: Signal | null;
};
export type InstrumentsPage = {
  items: InstrumentResearchView[];
  nextCursor: string;
};
export type LedgerEntry = {
  accountId: string;
  delta: Balances;
  effectiveAt: string;
  eventKey: string;
  id: string;
  instrumentId: string;
  kind: string;
  quantityDelta: SignedQuantity;
  recordedAt: string;
};
export type LedgerPage = {
  items: LedgerEntry[];
  nextCursor: string;
};
export type Metric = {
  factRefs: string[];
  reason: string;
  value: string | null;
};
export type Money = string;
export type NAVPoint = {
  nav: Money;
  sessionDate: string;
};
export type NewsItem = {
  availableAt: string;
  id: string;
  ingestedAt: string;
  instrumentIds: string[];
  publishedAt: string;
  source: string;
  sourceRecordId: string;
  summary: string;
  title: string;
  url: string;
};
export type OrderView = {
  accountId: string;
  capacity: Quantity;
  createdAt: string;
  expiresAt: string;
  fill: FillView | null;
  id: string;
  instrumentId: string;
  origin: string;
  quantity: Quantity;
  reason: string;
  reservedCash: Money;
  reservedQuantity: Quantity;
  side: string;
  state: string;
  targetOpenAt: string;
  version: number;
};
export type OrdersPage = {
  items: OrderView[];
  nextCursor: string;
};
export type Performance = {
  annualReturn: number | null;
  cumulativeReturn: number;
  maxDrawdown: number;
  missingReasons: string[];
  sharpe: number | null;
  tradingDays: number;
  turnover: number;
};
export type PerformanceView = {
  asOf: string;
  benchmarkCurve: NAVPoint[];
  benchmarkMetrics: Performance | null;
  benchmarkReason: string;
  curve: NAVPoint[];
  datasetVersion: string;
  feed: string;
  initialCash: Money;
  kind: string;
  metrics: Performance | null;
  mode: string;
  qualityFlags: string[];
};
export type PlaceOrderRequest = {
  expectedVersion: number;
  instrumentId: string;
  quantity: Quantity;
  side: string;
};
export type Position = {
  costBasis: Money;
  industry: string;
  instrumentId: string;
  quantity: Quantity;
  reservedQuantity: Quantity;
};
export type PositionsPage = {
  items: Position[];
  nextCursor: string;
};
export type Price = string;
export type Quantity = string;
export type Recommendation = {
  action: string;
  asOf: string;
  evidence: string[];
  potential: string;
  strategyVersionId: string;
  universeVersionId: string;
  unknowns: string[];
};
export type RiskAssessment = {
  asOf: string;
  level: string;
  reasons: string[];
};
export type RiskDecision = {
  allowed: boolean;
  maxQuantity: Quantity;
  reasonCode: string;
};
export type RiskPolicy = {
  drawdownPause: number;
  industryWeight: number;
  singleWeight: number;
  stockWeight: number;
  stopLoss: number;
  turnoverLimit: number;
};
export type RunView = {
  createdAt: string;
  errorCode: string;
  phase: string;
  reason: string;
  runId: string;
  status: string;
  updatedAt: string;
};
export type Signal = {
  factorEvidence: FactorEvidence[];
  industry: string;
  instrumentId: string;
  metrics: FinancialMetrics;
  qualityFlags: string[];
  rank: number;
  recommendation: Recommendation;
  risk: RiskAssessment;
  score: number;
};
export type SignedMoney = string;
export type SignedQuantity = string;
export type StrategyParameters = {
  entryScore: number;
  exitScore: number;
  maxHoldings: number;
  rebalanceBand: number;
  weights: number[];
};
export type SyncRequest = {
  expectedVersion?: number;
  from: string;
  instrumentIds?: string[];
  to: string;
};
export type SyncsPage = {
  items: RunView[];
  nextCursor: string;
};
export type TTMFinancials = {
  capitalExpenditure: FinancialValue;
  closingEquity: FinancialValue;
  dilutedEps: FinancialValue;
  freeCashFlow: FinancialValue;
  latestPeriodEnd: string;
  netIncome: FinancialValue;
  openingEquity: FinancialValue;
  operatingCashFlow: FinancialValue;
  revenue: FinancialValue;
};
export type TickerChange = {
  effectiveAt: string;
  ticker: string;
};
export type Topic = {
  articles: NewsItem[];
  change24Hours: number;
  count24Hours: number;
  count7Days: number;
  mappingVersion: string;
  name: string;
  previous24Hours: number;
};
export type Valuation = {
  earningsYield: Metric;
  freeCashFlowYield: Metric;
  marketCap: Metric;
  pb: Metric;
  pe: Metric;
};
export type VersionView = {
  createdAt: string;
  effectiveAt: string;
  id: string;
  instrumentIds: string[];
  mode: string;
  name: string;
  parameters: StrategyParameters | null;
  parentId: string;
};
export type VersionsPage = {
  items: VersionView[];
  nextCursor: string;
};
