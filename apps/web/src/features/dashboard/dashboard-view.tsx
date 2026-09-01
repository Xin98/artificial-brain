import type { DashboardSummary } from "./fetch-dashboard";
import type { ReminderDelivery, ReminderState } from "./fetch-reminders";

interface StatTile {
  label: string;
  value: number;
  tone: "danger" | "warn" | null;
}

// Reminder states render as badges; terminal failures read as danger, the
// in-flight retry window as warn, everything else as neutral metadata.
const STATE_BADGES: Record<ReminderState, string> = {
  scheduled: "badge badge-muted",
  sending: "badge badge-warn",
  succeeded: "badge badge-ok",
  failed: "badge badge-danger",
  suppressed: "badge badge-muted",
};

function tileClass(tile: StatTile): string {
  if (tile.tone !== null && tile.value > 0) {
    return `stat-tile stat-tile-${tile.tone}`;
  }
  return "stat-tile";
}

// DashboardView renders the todo and reminder clusters and, when the
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
  const todoTiles: StatTile[] = [
    { label: "待处理", value: summary.pendingTotal, tone: null },
    { label: "今日到期", value: summary.dueToday, tone: null },
    { label: "已逾期", value: summary.overdue, tone: "danger" },
    { label: "无到期时间", value: summary.noDue, tone: null },
    { label: "近 7 天完成", value: summary.completedLast7Days, tone: null },
  ];
  const reminderTiles: StatTile[] = [
    { label: "提醒成功", value: summary.reminderSucceeded, tone: null },
    { label: "重试中", value: summary.reminderRetrying, tone: "warn" },
    { label: "失败", value: summary.reminderFailed, tone: "danger" },
    { label: "被抑制", value: summary.reminderSuppressed, tone: null },
  ];

  return (
    <>
      <section aria-label="仪表盘" className="dashboard">
        <div className="dashboard-cluster">
          <h2 className="cluster-title">待办</h2>
          <div className="dashboard-tiles">
            {todoTiles.map((tile) => (
              <article className={tileClass(tile)} key={tile.label}>
                <p className="stat-value">{tile.value}</p>
                <p className="stat-label">{tile.label}</p>
              </article>
            ))}
          </div>
        </div>
        <div className="dashboard-cluster dashboard-cluster-tinted">
          <h2 className="cluster-title">提醒投递</h2>
          <div className="dashboard-tiles">
            {reminderTiles.map((tile) => (
              <article className={tileClass(tile)} key={tile.label}>
                <p className="stat-value">{tile.value}</p>
                <p className="stat-label">{tile.label}</p>
              </article>
            ))}
          </div>
        </div>
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
                  </span>
                  <span className="badge badge-muted">{delivery.channel}</span>
                  <span className={STATE_BADGES[delivery.state]}>
                    {delivery.state}
                  </span>
                  <time dateTime={delivery.scheduledAt}>
                    {new Date(delivery.scheduledAt).toLocaleString()}
                  </time>
                  {delivery.receiptState ? (
                    <span
                      className={
                        delivery.receiptState === "received_ok"
                          ? "badge badge-ok"
                          : "badge badge-danger"
                      }
                    >
                      {delivery.receiptState}
                    </span>
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
