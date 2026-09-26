"use client";

import { useEffect, useState } from "react";

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
  }, [fetcher, timezoneProvider]);

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
  }, [fetcher, reminderReloadKey, reminderStatus]);

  if (failed) {
    return (
      <p className="todo-error" role="alert">
        仪表盘暂时不可用,请稍后再试。
      </p>
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
    <DashboardView
      deliveries={deliveries}
      onSelectReminderStatus={selectReminderStatus}
      recordsLoading={!recordsFailed && deliveries === undefined}
      recordsUnavailable={recordsFailed}
      selectedReminderStatus={reminderStatus}
      summary={summary}
    />
  );
}
