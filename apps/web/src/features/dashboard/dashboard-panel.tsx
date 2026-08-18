"use client";

import { useEffect, useState } from "react";

import { fetchDashboardSummary } from "./fetch-dashboard";
import type { DashboardSummary } from "./fetch-dashboard";
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

// DashboardPanel fetches the summary with the browser timezone (A1) and
// renders the deterministic tiles; failures stay fail-closed.
export function DashboardPanel({
  fetcher = fetch,
  timezoneProvider,
}: {
  fetcher?: typeof fetch;
  timezoneProvider?: () => string;
}): React.JSX.Element {
  const [summary, setSummary] = useState<DashboardSummary | null>(null);
  const [failed, setFailed] = useState(false);

  useEffect(() => {
    let cancelled = false;
    const timezone = browserTimezone(timezoneProvider);
    void fetchDashboardSummary("", fetcher, timezone).then((result) => {
      if (cancelled) {
        return;
      }
      if (result === null) {
        setFailed(true);
        return;
      }
      setSummary(result);
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
  return <DashboardView summary={summary} />;
}
