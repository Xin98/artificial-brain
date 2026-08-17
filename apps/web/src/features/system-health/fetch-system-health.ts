import type { HealthComponent, SystemHealthReport } from "./types";

const RFC3339_PATTERN =
  /^(\d{4})-(\d{2})-(\d{2})T(\d{2}):(\d{2}):(\d{2})(?:\.\d+)?(?:Z|[+-](\d{2}):(\d{2}))$/;
const CORRELATION_ID_PATTERN = /^[A-Za-z0-9._-]{1,128}$/;
const componentNames = ["api", "database", "worker"] as const;

export function unavailableReport(
  now: Date,
  correlationId = "",
): SystemHealthReport {
  const checkedAt = now.toISOString();

  return {
    status: "unavailable",
    checkedAt,
    correlationId: isCorrelationId(correlationId) ? correlationId : "",
    components: {
      api: { status: "unavailable", checkedAt },
      database: { status: "unavailable", checkedAt },
      worker: { status: "unavailable", checkedAt },
    },
  };
}

export async function fetchSystemHealth(
  baseURL: string,
  fetcher: typeof fetch,
  timeoutMs = 1500,
): Promise<SystemHealthReport> {
  try {
    const endpoint = new URL("/api/v1/system/health", baseURL).toString();
    const response = await fetcher(endpoint, {
      signal: AbortSignal.timeout(safeTimeout(timeoutMs)),
      cache: "no-store",
      headers: { accept: "application/json" },
    });

    if (!response.ok) {
      return unavailableReport(new Date());
    }

    const payload: unknown = await response.json();
    return isSystemHealthReport(payload)
      ? payload
      : unavailableReport(new Date());
  } catch {
    return unavailableReport(new Date());
  }
}

function safeTimeout(timeoutMs: number): number {
  return Number.isFinite(timeoutMs) && timeoutMs > 0 ? timeoutMs : 1;
}

function isSystemHealthReport(value: unknown): value is SystemHealthReport {
  if (
    !isRecord(value) ||
    !hasExactKeys(value, ["status", "checkedAt", "correlationId", "components"])
  ) {
    return false;
  }

  if (
    (value.status !== "healthy" && value.status !== "degraded") ||
    !isTimestamp(value.checkedAt)
  ) {
    return false;
  }

  if (
    typeof value.correlationId !== "string" ||
    !isCorrelationId(value.correlationId)
  ) {
    return false;
  }

  const components = value.components;
  if (!isRecord(components) || !hasExactKeys(components, componentNames)) {
    return false;
  }

  return componentNames.every((name) => isHealthComponent(components[name]));
}

function isHealthComponent(value: unknown): value is HealthComponent {
  if (
    !isRecord(value) ||
    !hasAllowedKeys(value, ["status", "checkedAt", "detail"])
  ) {
    return false;
  }

  return (
    (value.status === "healthy" || value.status === "unavailable") &&
    isTimestamp(value.checkedAt) &&
    (value.detail === undefined ||
      (typeof value.detail === "string" && value.detail.length <= 200))
  );
}

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === "object" && value !== null && !Array.isArray(value);
}

function hasExactKeys(
  value: Record<string, unknown>,
  keys: readonly string[],
): boolean {
  return hasAllowedKeys(value, keys) && keys.every((key) => key in value);
}

function hasAllowedKeys(
  value: Record<string, unknown>,
  keys: readonly string[],
): boolean {
  return Object.keys(value).every((key) => keys.includes(key));
}

function isTimestamp(value: unknown): value is string {
  if (typeof value !== "string") {
    return false;
  }

  const match = RFC3339_PATTERN.exec(value);
  if (match === null) {
    return false;
  }

  const [
    ,
    yearText,
    monthText,
    dayText,
    hourText,
    minuteText,
    secondText,
    offsetHourText,
    offsetMinuteText,
  ] = match;
  const year = Number(yearText);
  const month = Number(monthText);
  const day = Number(dayText);
  const hour = Number(hourText);
  const minute = Number(minuteText);
  const second = Number(secondText);
  const offsetHour = offsetHourText === undefined ? 0 : Number(offsetHourText);
  const offsetMinute =
    offsetMinuteText === undefined ? 0 : Number(offsetMinuteText);

  return (
    month >= 1 &&
    month <= 12 &&
    day >= 1 &&
    day <= daysInMonth(year, month) &&
    hour <= 23 &&
    minute <= 59 &&
    second <= 59 &&
    offsetHour <= 23 &&
    offsetMinute <= 59 &&
    !Number.isNaN(Date.parse(value))
  );
}

function daysInMonth(year: number, month: number): number {
  if (month === 2) {
    return isLeapYear(year) ? 29 : 28;
  }

  return [4, 6, 9, 11].includes(month) ? 30 : 31;
}

function isLeapYear(year: number): boolean {
  return year % 4 === 0 && (year % 100 !== 0 || year % 400 === 0);
}

function isCorrelationId(value: string): boolean {
  return CORRELATION_ID_PATTERN.test(value);
}
