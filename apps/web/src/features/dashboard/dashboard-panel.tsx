"use client";

import { useEffect, useState } from "react";
import Link from "next/link";
import { listTodos } from "../todos/fetch-todos";
import type { Todo } from "../todos/fetch-todos";
import { filtersForView } from "../todos/todo-list";

import { fetchDashboardSummary } from "./fetch-dashboard";
import type { DashboardSummary } from "./fetch-dashboard";
import { fetchReminderDeliveries } from "./fetch-reminders";
import type { ReminderDelivery, ReminderStatusFilter } from "./fetch-reminders";
import { DashboardView } from "./dashboard-view";

function browserTimezone(provider?: () => string): string {
  if (provider) {
    return provider();
  }
  try {
    return Intl.DateTimeFormat().resolvedOptions().timeZone || "UTC";
  } catch {
    return "UTC";
  }
}

// DashboardPanel fetches the summary and the reminder delivery records with
// the browser timezone (A1). A summary failure stays fail-closed; a records
// failure degrades gracefully to the summary-only view with an inline note.
export function DashboardPanel({
  fetcher = fetch,
  timezoneProvider,
}: {
  fetcher?: typeof fetch;
  timezoneProvider?: () => string;
}): React.JSX.Element {
  const [summary, setSummary] = useState<DashboardSummary | null>(null);
  const [deliveries, setDeliveries] = useState<ReminderDelivery[] | undefined>(
    undefined,
  );
  const [recordsFailed, setRecordsFailed] = useState(false);
  const [failed, setFailed] = useState(false);
  const [reminderStatus, setReminderStatus] =
    useState<ReminderStatusFilter | null>(null);
  const [reminderReloadKey, setReminderReloadKey] = useState(0);
  const [reloadKey, setReloadKey] = useState(0);
  const [todayTodos, setTodayTodos] = useState<Todo[] | null>(null);
  const [todayLoading, setTodayLoading] = useState(true);
  function refresh(): void {
    setFailed(false);
    setRecordsFailed(false);
    setDeliveries(undefined);
    setTodayLoading(true);
    setReloadKey((key) => key + 1);
  }

  function selectReminderStatus(status: ReminderStatusFilter | null): void {
    if (status === reminderStatus) {
      setDeliveries(undefined);
      setRecordsFailed(false);
      setReminderReloadKey((key) => key + 1);
      return;
    }
    setDeliveries(undefined);
    setRecordsFailed(false);
    setReminderStatus(status);
  }

  useEffect(() => {
    let cancelled = false;
    const timezone = browserTimezone(timezoneProvider);
    void fetchDashboardSummary("", fetcher, timezone).then((summaryResult) => {
      if (cancelled) {
        return;
      }
      if (summaryResult === null) {
        setFailed(true);
        return;
      }
      setSummary(summaryResult);
    });
    return () => {
      cancelled = true;
    };
  }, [fetcher, timezoneProvider, reloadKey]);

  useEffect(() => {
    let cancelled = false;
    void listTodos("", fetcher, filtersForView("today")).then((todos) => {
      if (!cancelled) {
        setTodayTodos(todos);
        setTodayLoading(false);
      }
    });
    return () => {
      cancelled = true;
    };
  }, [fetcher, reloadKey]);

  useEffect(() => {
    let cancelled = false;
    void fetchReminderDeliveries(
      "",
      fetcher,
      3000,
      reminderStatus ?? undefined,
    ).then((deliveriesResult) => {
      if (cancelled) {
        return;
      }
      if (deliveriesResult === null) {
        setRecordsFailed(true);
        return;
      }
      setDeliveries(deliveriesResult);
    });
    return () => {
      cancelled = true;
    };
  }, [fetcher, reminderReloadKey, reminderStatus, reloadKey]);

  if (failed) {
    return (
      <div>
        <p className="todo-error" role="alert">
          仪表盘暂时不可用,请稍后再试。
        </p>
        <button className="btn-ghost" onClick={refresh} type="button">
          刷新概况
        </button>
      </div>
    );
  }
  if (!summary) {
    return (
      <div className="dashboard-skeleton" role="status">
        <span className="sr-only">加载中…</span>
        <div className="dashboard-tiles">
          {Array.from({ length: 5 }, (_unused, index) => (
            <div className="stat-tile" key={index}>
              <span className="skeleton skeleton-value" />
              <span className="skeleton skeleton-line" />
            </div>
          ))}
        </div>
      </div>
    );
  }
  return (
    <>
      <button className="btn-ghost" onClick={refresh} type="button">
        刷新概况
      </button>
      <section aria-label="今日待办" className="dashboard-today">
        <h2>今日待办</h2>
        {todayLoading ? (
          <p>今日待办加载中…</p>
        ) : todayTodos === null ? (
          <p>
            今日待办暂时不可用。
            <Link href="/todos?view=today">打开今日待办</Link>
          </p>
        ) : todayTodos.length ? (
          <ul>
            {todayTodos.map((todo) => (
              <li key={todo.id}>
                <Link href="/todos?view=today">{todo.title}</Link>
                {todo.dueAtUtc ? (
                  <time dateTime={todo.dueAtUtc}>
                    {new Date(todo.dueAtUtc).toLocaleTimeString([], {
                      hour: "2-digit",
                      minute: "2-digit",
                    })}
                  </time>
                ) : null}
              </li>
            ))}
          </ul>
        ) : (
          <p>
            今天暂无到期事项。<Link href="/todos">新建待办</Link>，或
            <Link href="/conversation">用对话记录下一件事</Link>。
          </p>
        )}
        {todayTodos === null ? <Link href="/todos">新建待办</Link> : null}
      </section>
      <DashboardView
        deliveries={deliveries}
        onSelectReminderStatus={selectReminderStatus}
        recordsLoading={!recordsFailed && deliveries === undefined}
        recordsUnavailable={recordsFailed}
        selectedReminderStatus={reminderStatus}
        summary={summary}
      />
    </>
  );
}
