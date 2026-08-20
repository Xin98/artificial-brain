"use client";

import { useEffect, useState } from "react";

import { fetchDashboardSummary } from "./fetch-dashboard";
import type { DashboardSummary } from "./fetch-dashboard";
import { fetchReminderDeliveries } from "./fetch-reminders";
import type { ReminderDelivery } from "./fetch-reminders";
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

  useEffect(() => {
    let cancelled = false;
    const timezone = browserTimezone(timezoneProvider);
    void Promise.all([
      fetchDashboardSummary("", fetcher, timezone),
      fetchReminderDeliveries("", fetcher),
    ]).then(([summaryResult, deliveriesResult]) => {
      if (cancelled) {
        return;
      }
      if (summaryResult === null) {
        setFailed(true);
        return;
      }
      setSummary(summaryResult);
      if (deliveriesResult === null) {
        setRecordsFailed(true);
        return;
      }
      setDeliveries(deliveriesResult);
    });
    return () => {
      cancelled = true;
    };
  }, [fetcher, timezoneProvider]);

  if (failed) {
    return <p role="alert">仪表盘暂时不可用,请稍后再试。</p>;
  }
  if (!summary) {
    return <p>加载中…</p>;
  }
  return (
    <>
      <DashboardView deliveries={deliveries} summary={summary} />
      {recordsFailed ? (
        <p className="dashboard-records-note" role="status">
          提醒记录暂时不可用,请稍后再试。
        </p>
      ) : null}
    </>
  );
}
