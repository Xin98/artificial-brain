import Link from "next/link";

import type { DashboardSummary } from "./fetch-dashboard";
import type {
  ReminderDelivery,
  ReminderState,
  ReminderStatusFilter,
} from "./fetch-reminders";

interface StatTile {
  label: string;
  value: number;
  tone: "danger" | "warn" | null;
}

interface ReminderStatTile extends StatTile {
  status: ReminderStatusFilter;
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

// DashboardView stays presentational: data and reminder-record request state
// arrive via props. The records region remains mounted while loading or
// unavailable so every reminder card keeps a valid aria-controls target.
export function DashboardView({
  summary,
  deliveries,
  recordsLoading,
  recordsUnavailable = false,
  selectedReminderStatus = null,
  onSelectReminderStatus = () => undefined,
}: {
  summary: DashboardSummary;
  deliveries?: ReminderDelivery[];
  recordsLoading?: boolean;
  recordsUnavailable?: boolean;
  selectedReminderStatus?: ReminderStatusFilter | null;
  onSelectReminderStatus?: (status: ReminderStatusFilter | null) => void;
}): React.JSX.Element {
  const todoTiles: StatTile[] = [
    { label: "待处理", value: summary.pendingTotal, tone: null },
    { label: "今日到期", value: summary.dueToday, tone: null },
    { label: "已逾期", value: summary.overdue, tone: "danger" },
    { label: "无到期时间", value: summary.noDue, tone: null },
    { label: "近 7 天完成", value: summary.completedLast7Days, tone: null },
  ];
  const reminderTiles: ReminderStatTile[] = [
    {
      label: "提醒成功",
      value: summary.reminderSucceeded,
      tone: null,
      status: "succeeded",
    },
    {
      label: "重试中",
      value: summary.reminderRetrying,
      tone: "warn",
      status: "retrying",
    },
    {
      label: "失败",
      value: summary.reminderFailed,
      tone: "danger",
      status: "failed",
    },
    {
      label: "被抑制",
      value: summary.reminderSuppressed,
      tone: null,
      status: "suppressed",
    },
  ];
  const isRecordsLoading =
    !recordsUnavailable && (recordsLoading ?? deliveries === undefined);
  const visibleDeliveries = deliveries ?? [];

  return (
    <>
      <section aria-label="仪表盘" className="dashboard">
        <div className="dashboard-cluster">
          <h2 className="cluster-title">待办</h2>
          <div className="dashboard-tiles">
            {todoTiles.map((tile) => (
              <Link
                aria-label={`打开待办页面，${tile.label} ${tile.value} 项`}
                className={`${tileClass(tile)} stat-tile-action`}
                href="/todos"
                key={tile.label}
              >
                <span className="stat-value">{tile.value}</span>
                <span className="stat-label">{tile.label}</span>
                <span aria-hidden="true" className="stat-tile-cue">
                  打开待办 →
                </span>
              </Link>
            ))}
          </div>
        </div>
        <div className="dashboard-cluster dashboard-cluster-tinted">
          <h2 className="cluster-title">提醒投递</h2>
          <div className="dashboard-tiles">
            {reminderTiles.map((tile) => {
              const selected = selectedReminderStatus === tile.status;
              return (
                <button
                  aria-controls="reminder-records"
                  aria-label={`筛选${tile.label}提醒记录，共 ${tile.value} 条`}
                  aria-pressed={selected}
                  className={`${tileClass(tile)} stat-tile-action ${
                    selected ? "stat-tile-selected" : ""
                  }`}
                  key={tile.label}
                  onClick={() => onSelectReminderStatus(tile.status)}
                  type="button"
                >
                  <span className="stat-value">{tile.value}</span>
                  <span className="stat-label">{tile.label}</span>
                  <span aria-hidden="true" className="stat-tile-cue">
                    {selected ? "已筛选" : "查看记录 →"}
                  </span>
                </button>
              );
            })}
          </div>
          {selectedReminderStatus !== null ? (
            <button
              className="reminder-filter-reset"
              onClick={() => onSelectReminderStatus(null)}
              type="button"
            >
              全部提醒
            </button>
          ) : null}
        </div>
        <p className="dashboard-checked">
          统计时间{" "}
          <time dateTime={summary.checkedAt}>
            {new Date(summary.checkedAt).toLocaleString()}
          </time>
        </p>
      </section>
      <section
        aria-label="提醒记录"
        aria-busy={isRecordsLoading}
        className="reminder-records"
        id="reminder-records"
      >
        <h2>提醒记录</h2>
        <p
          className={
            isRecordsLoading
              ? "reminder-records-empty"
              : recordsUnavailable
                ? "dashboard-records-note"
                : "sr-only"
          }
          role="status"
        >
          {isRecordsLoading
            ? "提醒记录加载中…"
            : recordsUnavailable
              ? "提醒记录暂时不可用，请稍后再试。"
              : `已加载 ${visibleDeliveries.length} 条提醒记录`}
        </p>
        {!isRecordsLoading &&
        !recordsUnavailable &&
        visibleDeliveries.length === 0 ? (
          <p className="reminder-records-empty">暂无提醒记录</p>
        ) : null}
        {!isRecordsLoading &&
        !recordsUnavailable &&
        visibleDeliveries.length > 0 ? (
          <ul>
            {visibleDeliveries.map((delivery) => (
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
        ) : null}
      </section>
    </>
  );
}
