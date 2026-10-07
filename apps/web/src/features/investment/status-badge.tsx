import type { DataStatus } from "./types";
export function StatusBadge({
  kind,
  value,
}: {
  kind: "risk" | "potential";
  value: string;
}) {
  const risk: Record<string, string> = {
    low: "风险低",
    medium: "风险中",
    high: "风险高",
    unknown: "风险未知",
  };
  const potential: Record<string, string> = {
    high: "潜力较高",
    observe: "潜力待观察",
    weak: "潜力偏弱",
    unknown: "潜力未知",
  };
  const label = (kind === "risk" ? risk : potential)[value] ?? "未知";
  return (
    <span className={"investment-badge investment-" + kind + "-" + value}>
      {label}
    </span>
  );
}
export function SourceNotice({
  data,
}: {
  data: Pick<DataStatus, "mode" | "feed" | "asOf" | "datasetVersion">;
}) {
  return (
    <aside className="investment-source" aria-label="数据来源">
      <strong>{data.mode === "fixture" ? "演示数据" : "美股只读行情"}</strong>
      <span>
        {data.feed} · 数据截止 {data.asOf}
      </span>
      <small>
        数据版本 {data.datasetVersion}。
        {data.mode === "fixture"
          ? "合成数据仅用于验证系统，收益没有真实投资含义。"
          : "日线在收盘后确认，账户绑定当前行情源。"}
      </small>
    </aside>
  );
}
export const reasonText = (reason: string) =>
  (
    ({
      nonpositive_earnings: "盈利非正",
      financial_report_stale: "财报过旧",
      price_risk_metrics_incomplete: "价格风险证据不足",
      industry_unknown: "行业信息缺失",
      data_stale: "行情尚未确认",
      news_permission_not_verified: "新闻权限未验证",
      news_not_configured: "新闻未配置",
      news_unavailable: "新闻暂不可用",
      insufficient_history: "历史不足",
      annual_return_requires_252_days: "年化收益需要 252 个交易日",
      sharpe_requires_60_returns: "Sharpe 需要 60 个有效收益样本",
      zero_return_variance: "收益波动为零，Sharpe 不适用",
      drawdown_pause: "账户回撤达到限制，买入暂停",
    }) as Record<string, string>
  )[reason] ?? reason;
