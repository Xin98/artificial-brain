// Shared fail-closed validators for feature fetchers. They mirror the Go
// contracts: exact keys, enums, and RFC3339 timestamps. Anything unexpected
// fails closed (null) instead of degrading into partial data.

const RFC3339_PATTERN =
  /^(\d{4})-(\d{2})-(\d{2})T(\d{2}):(\d{2}):(\d{2})(?:\.\d+)?(?:Z|[+-](\d{2}):(\d{2}))$/;

export function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === "object" && value !== null && !Array.isArray(value);
}

export function hasExactKeys(
  value: Record<string, unknown>,
  keys: readonly string[],
): boolean {
  const own = Object.keys(value);
  return own.length === keys.length && keys.every((key) => key in value);
}

export function hasAllowedKeys(
  value: Record<string, unknown>,
  keys: readonly string[],
): boolean {
  return Object.keys(value).every((key) => keys.includes(key));
}

export function isNonEmptyString(value: unknown): value is string {
  return typeof value === "string" && value.length > 0;
}

export function isInteger(value: unknown): value is number {
  return typeof value === "number" && Number.isInteger(value) && value >= 0;
}

export function isBoolean(value: unknown): value is boolean {
  return typeof value === "boolean";
}

// formatRFC3339UTC renders a Date as a second-precision UTC RFC3339 string
// (matching the Go time.RFC3339 wire format).
export function formatRFC3339UTC(date: Date): string {
  const pad = (value: number): string => String(value).padStart(2, "0");
  return (
    `${date.getUTCFullYear()}-${pad(date.getUTCMonth() + 1)}-${pad(date.getUTCDate())}` +
    `T${pad(date.getUTCHours())}:${pad(date.getUTCMinutes())}:${pad(date.getUTCSeconds())}Z`
  );
}

export function isStringOrUndefined(
  value: unknown,
): value is string | undefined {
  return value === undefined || typeof value === "string";
}

export function isRFC3339(value: unknown): value is string {
  if (typeof value !== "string") {
    return false;
  }
  const match = RFC3339_PATTERN.exec(value);
  if (match === null) {
    return false;
  }
  // Group 1 is the year; it is range-checked implicitly by Date.parse below.
  const [, , monthText, dayText, hourText, minuteText, secondText] = match;
  return (
    Number(monthText) >= 1 &&
    Number(monthText) <= 12 &&
    Number(dayText) >= 1 &&
    Number(dayText) <= 31 &&
    Number(hourText) <= 23 &&
    Number(minuteText) <= 59 &&
    Number(secondText) <= 59 &&
    !Number.isNaN(Date.parse(value))
  );
}

export function safeTimeout(timeoutMs: number): number {
  return Number.isFinite(timeoutMs) && timeoutMs > 0 ? timeoutMs : 1;
}

export interface ErrorKind {
  code: "validation_error" | "rate_limited" | "conflict" | "other";
}

export function classifyErrorPayload(payload: unknown): ErrorKind {
  if (isRecord(payload) && typeof payload.code === "string") {
    if (payload.code === "validation_error") {
      return { code: "validation_error" };
    }
    if (payload.code === "rate_limited") {
      return { code: "rate_limited" };
    }
    if (payload.code === "conflict") {
      return { code: "conflict" };
    }
  }
  return { code: "other" };
}

export async function readErrorPayload(
  response: Response,
): Promise<unknown | null> {
  try {
    return (await response.json()) as unknown;
  } catch {
    return null;
  }
}
