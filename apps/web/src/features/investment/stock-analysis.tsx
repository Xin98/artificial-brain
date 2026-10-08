"use client";
import Link from "next/link";
import {
  investmentClient,
  failureText,
  type InvestmentClient,
} from "./fetch-investment";
import { useResource } from "./hooks";
import { PriceChart } from "./price-chart";
import type { AnalysisView, Metric } from "./types";
import { reasonText, SourceNotice, StatusBadge } from "./status-badge";
function MetricRow({ name, metric }: { name: string; metric?: Metric }) {
  return (
    <div>
      <dt>{name}</dt>
      <dd>{metric?.value ?? "不适用"}</dd>
      {metric?.value === null ? (
        <small>
          {name}不适用：{reasonText(metric.reason)}
        </small>
      ) : null}
      {metric?.factRefs.length ? (
        <details>
          <summary>事实来源</summary>
          <ul>
            {metric.factRefs.map((r, i) => (
              <li key={r + i}>{r}</li>
            ))}
          </ul>
        </details>
      ) : null}
    </div>
  );
}
export function StockAnalysis({
  client = investmentClient,
  instrumentId,
  accountId,
  returnQuery = "",
}: {
  client?: InvestmentClient;
  instrumentId: string;
  accountId?: string;
  returnQuery?: string;
}) {
  const query = accountId ? "?accountId=" + encodeURIComponent(accountId) : "";
  const { result, retry } = useResource<AnalysisView>(
    client,
    "/instruments/" + encodeURIComponent(instrumentId) + "/analysis" + query,
    "AnalysisView",
  );
  if (!result) return <p role="status">正在读取分析证据…</p>;
  if (!result.ok)
    return (
      <p role="alert">
        {failureText(result.code)} <button onClick={retry}>重试分析</button>
      </p>
    );
  const v = result.value;
  return (
    <div className="investment-layout">
      <Link href={returnQuery ? "/investment?" + returnQuery : "/investment"}>
        返回股票研究
      </Link>
      <SourceNotice data={v} />
      <header>
        <h1>
          {v.instrument.ticker} · {v.instrument.name}
        </h1>
        <div className="investment-toolbar">
          <StatusBadge kind="potential" value={v.recommendation.potential} />
          <StatusBadge kind="risk" value={v.risk.level} />
          <span>相对评分 {v.signal?.score.toFixed(1) ?? "证据不足"}</span>
          {accountId ? (
            <Link
              href={
                "/investment/accounts/" +
                encodeURIComponent(accountId) +
                "?instrument=" +
                encodeURIComponent(instrumentId)
              }
            >
              前往账户下单
            </Link>
          ) : (
            <Link
              href={
                "/investment/accounts?instrument=" +
                encodeURIComponent(instrumentId)
              }
            >
              前往模拟账户下单
            </Link>
          )}
        </div>
      </header>
      <section className="investment-section">
        <h2>价格走势</h2>
        <PriceChart series={v.prices} sourceLabel={v.datasetVersion} />
      </section>
      <section className="investment-section">
        <h2>量化结论</h2>
        <p>
          {v.recommendation.action === "reduce_holding"
            ? "当前持仓触发减仓规则，请查看具体原因及待执行订单。"
            : v.recommendation.action === "pause_automation"
              ? "账户回撤触发暂停规则，应先核查持仓与账户风险。"
              : v.recommendation.action === "avoid_new_automatic_buy"
                ? "风险较高，自动策略避免新买入。"
                : v.recommendation.action === "account_buy_blocked"
                  ? "当前账户风控不允许买入。"
                  : v.recommendation.potential === "high"
                    ? "相对潜力较高，仍需满足账户风控。"
                    : "观察或补充证据后再评估。"}
        </p>
        <p>
          采用可解释的研究多因子方法；未接入幻方私有模型，权重尚未通过实盘验证。
        </p>
        {v.accountRisk ? (
          <p>
            账户最多可买 {v.accountRisk.maxQuantity} 股 ·{" "}
            {reasonText(v.accountRisk.reasonCode)}
          </p>
        ) : null}
        <ul>
          {[
            ...v.risk.reasons,
            ...v.recommendation.evidence,
            ...v.recommendation.unknowns,
            ...v.qualityFlags,
          ].map((s, i) => (
            <li key={s + i}>{reasonText(s)}</li>
          ))}
        </ul>
        {v.signal ? (
          <div className="investment-table-wrap">
            <table className="investment-table">
              <caption>因子贡献与可知时点证据</caption>
              <thead>
                <tr>
                  <th>因子</th>
                  <th>权重</th>
                  <th>贡献</th>
                  <th>证据</th>
                </tr>
              </thead>
              <tbody>
                {v.signal.factorEvidence.map((f) => (
                  <tr key={f.group}>
                    <td>{f.group}</td>
                    <td>{f.weight}</td>
                    <td>{f.contribution}</td>
                    <td>{f.factRefs.join(" · ")}</td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        ) : null}
      </section>
      <section className="investment-section">
        <h2>财报与常用指标</h2>
        <dl className="investment-metrics">
          <MetricRow name="市盈率" metric={v.metrics.valuation.pe} />
          <MetricRow name="市净率" metric={v.metrics.valuation.pb} />
          <MetricRow
            name="盈利收益率"
            metric={v.metrics.valuation.earningsYield}
          />
          <MetricRow
            name="自由现金流收益率"
            metric={v.metrics.valuation.freeCashFlowYield}
          />
          <MetricRow name="ROE" metric={v.metrics.roe} />
          <MetricRow
            name="年化波动"
            metric={v.metrics.indicators.annualVolatility}
          />
          <MetricRow
            name="历史最大回撤"
            metric={v.metrics.indicators.maxDrawdown}
          />
          <MetricRow name="RSI" metric={v.metrics.indicators.rsi14} />
        </dl>
      </section>
      <section className="investment-section">
        <h2>热点与新闻</h2>
        {v.newsStatus !== "available" ? (
          <p>{reasonText(v.newsReason || v.newsStatus)}</p>
        ) : v.topics.length === 0 ? (
          <p>当前可知窗口没有相关新闻。</p>
        ) : (
          v.topics.map((t) => (
            <article key={t.name}>
              <h3>{t.name}</h3>
              {t.articles.map((n) => (
                <p key={n.id}>
                  <a
                    href={/^https?:\/\//.test(n.url) ? n.url : "#"}
                    target="_blank"
                    rel="noreferrer"
                  >
                    {n.title}
                  </a>
                  <small>{n.summary}</small>
                </p>
              ))}
            </article>
          ))
        )}
        <p>热点仅作补充信息，不改变量化分数。</p>
      </section>
    </div>
  );
}
