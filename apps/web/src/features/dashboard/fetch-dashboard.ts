import {
  hasExactKeys,
  isInteger,
  isRecord,
  isRFC3339,
  safeTimeout,
} from "../validation";

export interface DashboardSummary {
  pendingTotal: number;
  dueToday: number;
  overdue: number;
  noDue: number;
  completedLast7Days: number;
  reminderSucceeded: number;
  reminderRetrying: number;
  reminderFailed: number;
  reminderSuppressed: number;
  checkedAt: string;
}

export async function fetchDashboardSummary(
  baseURL: string,
  fetcher: typeof fetch,
  timezone: string,
  timeoutMs = 3000,
): Promise<DashboardSummary | null> {
  try {
    const endpoint = `${baseURL}/api/v1/dashboard/summary?timezone=${encodeURIComponent(timezone)}`;
    const response = await fetcher(endpoint, {
      signal: AbortSignal.timeout(safeTimeout(timeoutMs)),
      cache: "no-store",
      headers: { accept: "application/json" },
    });
    if (!response.ok) {
      return null;
    }
    const payload: unknown = await response.json();
    return isDashboardSummary(payload) ? payload : null;
  } catch {
    return null;
  }
}

function isDashboardSummary(value: unknown): value is DashboardSummary {
  if (
    !isRecord(value) ||
    !hasExactKeys(value, [
      "pendingTotal",
      "dueToday",
      "overdue",
      "noDue",
      "completedLast7Days",
      "reminderSucceeded",
      "reminderRetrying",
      "reminderFailed",
      "reminderSuppressed",
      "checkedAt",
    ])
  ) {
    return false;
  }
  return (
    isInteger(value.pendingTotal) &&
    isInteger(value.dueToday) &&
    isInteger(value.overdue) &&
    isInteger(value.noDue) &&
    isInteger(value.completedLast7Days) &&
    isInteger(value.reminderSucceeded) &&
    isInteger(value.reminderRetrying) &&
    isInteger(value.reminderFailed) &&
    isInteger(value.reminderSuppressed) &&
    isRFC3339(value.checkedAt)
  );
}
