"use client";
import {
  investmentClient,
  failureText,
  type InvestmentClient,
} from "./fetch-investment";
import { useResource } from "./hooks";
import type {
  PerformanceView,
  BacktestView,
  Performance,
  PositionsPage,
} from "./types";
import { PerformanceChart, DrawdownChart, cents } from "./performance-chart";
import { SourceNotice, reasonText } from "./status-badge";
function Metrics({
  value,
  title,
}: {
  value: Performance | null;
  title: string;
}) {
  if (!value) return <p>{title}：尚无完整绩效数据。</p>;
  return (
    <div>
      <h3>{title}</h3>
      <dl className="investment-metrics">
        <div>
          <dt>累计收益</dt>
          <dd>{(value.cumulativeReturn * 100).toFixed(2)}%</dd>
        </div>
        <div>
          <dt>最大回撤</dt>
          <dd>{(value.maxDrawdown * 100).toFixed(2)}%</dd>
        </div>
        <div>
          <dt>双边换手 / 平均净资产</dt>
          <dd>{(value.turnover * 100).toFixed(2)}%</dd>
        </div>
        <div>
          <dt>年化收益</dt>
          <dd>
            {value.annualReturn === null
              ? "不适用"
              : (value.annualReturn * 100).toFixed(2) + "%"}
          </dd>
          {value.annualReturn === null ? (
            <small>年化收益不适用：不足 252 个交易日</small>
          ) : null}
        </div>
        <div>
          <dt>Sharpe</dt>
          <dd>{value.sharpe === null ? "不适用" : value.sharpe.toFixed(2)}</dd>
          {value.sharpe === null ? (
            <small>
              {value.missingReasons.includes("sharpe_zero_variance")
                ? "Sharpe 不适用：收益方差为零"
                : "Sharpe 不适用：不足 60 个有效日收益"}
            </small>
          ) : null}
        </div>
      </dl>
      <p>已确认 {value.tradingDays} 个交易日；现金收益按 0 计算。</p>
    </div>
  );
}
export function PerformancePanel({
  client = investmentClient,
  resource,
  id,
}: {
  client?: InvestmentClient;
  resource: "account" | "backtest";
  id: string;
}) {
  const { result, retry } = useResource<PerformanceView | BacktestView>(
    client,
    resource === "account"
      ? "/accounts/" + id + "/performance"
      : "/backtests/" + id,
    resource === "account" ? "PerformanceView" : "BacktestView",
    true,
  );
  if (!result) return <p role="status">正在读取绩效…</p>;
  if (!result.ok)
    return (
      <p role="alert">
        {failureText(result.code)} <button onClick={retry}>重试绩效</button>
      </p>
    );
  const v = result.value;
  const backtest = "status" in v;
  const heading = backtest ? "历史回测绩效" : "模拟账户前向绩效";
  return (
    <section className="investment-section">
      <h2>{heading}</h2>
      {backtest ? (
        <>
          <SourceNotice
            data={{
              mode: v.mode,
              feed: v.feed,
              datasetVersion: v.datasetVersion,
              asOf: v.updatedAt,
            }}
          />
          <p>
            研究区间 {v.from.slice(0, 10)} 至 {v.to.slice(0, 10)} · 初始资金{" "}
            {v.initialCash} USD
          </p>
          <p>
            股票池版本 {v.universeVersionId}
            <br />
            策略版本 {v.strategyVersionId}
          </p>
          <p>
            固定股票池存在幸存者偏差；本结果属于研究回放，不是样本外收益验证。
          </p>
          <p>
            撮合规则：下一常规开盘、10bp 滑点、1bp 费用（最低 0.01
            USD），整数股和日成交容量限制。
          </p>
          <details>
            <summary>冻结数据快照</summary>
            <ul>
              {v.snapshotIds.map((s) => (
                <li key={s}>{s}</li>
              ))}
            </ul>
          </details>
        </>
      ) : (
        <p>
          前向绩效来自账户已确认的资金和持仓账本。经济时间与记录时间分开保留。
        </p>
      )}
      {backtest && v.status === "failed" ? (
        <p role="alert">回测失败：{reasonText(v.reason || v.errorCode)}</p>
      ) : backtest && v.status !== "completed" ? (
        <p role="status">
          回测{v.status === "queued" ? "已排队" : "正在运行"}，等待完整结果。
        </p>
      ) : (
        <>
          <Metrics value={v.metrics} title="策略绩效" />
          <PerformanceChart
            sourceLabel={
              (v.mode === "fixture" ? "演示数据" : "真实只读行情") +
              " · " +
              v.feed
            }
            title={heading + "净资产"}
            series={v.curve}
            benchmarkSeries={v.benchmarkReason ? [] : v.benchmarkCurve}
          />
          <DrawdownChart
            series={v.curve}
            initialCash={v.initialCash}
            sourceLabel={
              (v.mode === "fixture" ? "演示数据" : "真实只读行情") +
              " · " +
              v.feed
            }
          />
          {v.benchmarkReason || !v.benchmarkMetrics ? (
            <p>无法比较基准：数据不完整</p>
          ) : (
            <Metrics value={v.benchmarkMetrics} title="SPY 同口径基准" />
          )}
        </>
      )}
      {v.qualityFlags.length ? (
        <ul>
          {v.qualityFlags.map((s, i) => (
            <li key={s + i}>{reasonText(s)}</li>
          ))}
        </ul>
      ) : null}
    </section>
  );
}
export function PositionDistribution({
  client,
  accountId,
}: {
  client: InvestmentClient;
  accountId: string;
}) {
  const { result } = useResource<PositionsPage>(
    client,
    "/accounts/" + accountId + "/positions?limit=100",
    "PositionsPage",
    true,
  );
  if (!result) return null;
  if (!result.ok) return <p role="alert">{failureText(result.code)}</p>;
  const positions = result.value.items;
  const costs = positions.map((p) => cents(p.costBasis));
  if (costs.some((c) => c === null)) return null;
  const total = costs.reduce<bigint>((a, b) => a + (b ?? 0n), 0n);
  return (
    <section className="investment-section">
      <h2>持仓成本分布</h2>
      <p>按剩余买入成本比较持仓，市值风险以账户风控检查为准。</p>
      {total === 0n ? (
        <p>暂无持仓成本分布。</p>
      ) : (
        <div className="investment-distribution">
          {positions.map((p, i) => {
            const share = Number((costs[i]! * 10000n) / total) / 100;
            return (
              <div key={p.instrumentId}>
                <strong>{p.instrumentId}</strong>
                <span>
                  {p.costBasis} USD · {share.toFixed(2)}%
                </span>
                <meter
                  min="0"
                  max="100"
                  value={share}
                  aria-label={p.instrumentId + " 持仓成本占比"}
                />
              </div>
            );
          })}
        </div>
      )}
    </section>
  );
}
