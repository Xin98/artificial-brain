import type { DashboardSummary } from "./fetch-dashboard";

// DashboardView renders the six deterministic stat tiles. The reminder
// retry/fail counters stay zero until delivery lands in ITER-0003.
export function DashboardView({
  summary,
}: {
  summary: DashboardSummary;
}): React.JSX.Element {
  const tiles = [
    { label: "待处理", value: String(summary.pendingTotal) },
    { label: "今日到期", value: String(summary.dueToday) },
    { label: "已逾期", value: String(summary.overdue) },
    { label: "无到期时间", value: String(summary.noDue) },
    { label: "近 7 天完成", value: String(summary.completedLast7Days) },
    {
      label: "提醒重试/失败",
      value: `${summary.reminderRetrying} / ${summary.reminderFailed}`,
    },
  ];

  return (
    <section aria-label="仪表盘" className="dashboard-grid">
      {tiles.map((tile) => (
        <article className="stat-tile" key={tile.label}>
          <h2>{tile.label}</h2>
          <p className="stat-value">{tile.value}</p>
        </article>
      ))}
      <p className="dashboard-checked">
        统计时间{" "}
        <time dateTime={summary.checkedAt}>
          {new Date(summary.checkedAt).toLocaleString()}
        </time>
      </p>
    </section>
  );
}
