import type { DashboardSummary } from "./fetch-dashboard";
import type { ReminderDelivery } from "./fetch-reminders";

// DashboardView renders the nine deterministic stat tiles and, when the
// delivery records are provided, the reminder records list. It stays
// presentational: all data arrives via props, and an absent deliveries prop
// means the records section is omitted (degraded summary-only view).
export function DashboardView({
  summary,
  deliveries,
}: {
  summary: DashboardSummary;
  deliveries?: ReminderDelivery[];
}): React.JSX.Element {
  const tiles = [
    { label: "待处理", value: String(summary.pendingTotal) },
    { label: "今日到期", value: String(summary.dueToday) },
    { label: "已逾期", value: String(summary.overdue) },
    { label: "无到期时间", value: String(summary.noDue) },
    { label: "近 7 天完成", value: String(summary.completedLast7Days) },
    { label: "提醒成功", value: String(summary.reminderSucceeded) },
    { label: "重试中", value: String(summary.reminderRetrying) },
    { label: "失败", value: String(summary.reminderFailed) },
    { label: "被抑制", value: String(summary.reminderSuppressed) },
  ];

  return (
    <>
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
      {deliveries !== undefined ? (
        <section aria-label="提醒记录" className="reminder-records">
          <h2>提醒记录</h2>
          {deliveries.length === 0 ? (
            <p className="reminder-records-empty">暂无提醒记录</p>
          ) : (
            <ul>
              {deliveries.map((delivery) => (
                <li className="reminder-record" key={delivery.id}>
                  <span className="reminder-record-title">
                    《{delivery.todoTitle}》
                  </span>{" "}
                  ·{" "}
                  <span className="reminder-record-channel">
                    {delivery.channel}
                  </span>{" "}
                  ·{" "}
                  <span className="reminder-record-state">
                    {delivery.state}
                  </span>{" "}
                  ·{" "}
                  <time dateTime={delivery.scheduledAt}>
                    {new Date(delivery.scheduledAt).toLocaleString()}
                  </time>
                  {delivery.receiptState ? (
                    <>
                      {" "}
                      ·{" "}
                      <span className="reminder-record-receipt">
                        {delivery.receiptState}
                      </span>
                    </>
                  ) : null}
                </li>
              ))}
            </ul>
          )}
        </section>
      ) : null}
    </>
  );
}
