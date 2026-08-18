import "server-only";

import { cookies } from "next/headers";

// SESSION_COOKIE_NAME mirrors the API's bearer cookie.
export const SESSION_COOKIE_NAME = "ab_session";

// SessionContext is the fail-closed view of /api/v1/auth/session.
export interface SessionContext {
  userId: string;
  workspaceId: string;
  sessionId: string;
}

// readSessionCookie returns the raw session cookie value, or null when
// absent. It never validates the session itself.
export async function readSessionCookie(): Promise<string | null> {
  const store = await cookies();
  const value = store.get(SESSION_COOKIE_NAME)?.value;
  return value !== undefined && value !== "" ? value : null;
}

// authHeaders forwards the session cookie to internal API calls.
export function authHeaders(cookie: string): Record<string, string> {
  return { cookie: `${SESSION_COOKIE_NAME}=${cookie}` };
}

// fetchSession validates the session against the API and fails closed: any
// non-2xx response, malformed payload, or timeout yields null.
export async function fetchSession(
  baseURL: string,
  fetcher: typeof fetch,
  cookie: string,
  timeoutMs = 1500,
): Promise<SessionContext | null> {
  try {
    const endpoint = new URL("/api/v1/auth/session", baseURL).toString();
    const response = await fetcher(endpoint, {
      signal: AbortSignal.timeout(safeTimeout(timeoutMs)),
      cache: "no-store",
      headers: { accept: "application/json", ...authHeaders(cookie) },
    });

    if (!response.ok) {
      return null;
    }

    const payload: unknown = await response.json();
    return isSessionContext(payload) ? payload : null;
  } catch {
    return null;
  }
}

function safeTimeout(timeoutMs: number): number {
  return Number.isFinite(timeoutMs) && timeoutMs > 0 ? timeoutMs : 1;
}

function isSessionContext(value: unknown): value is SessionContext {
  if (
    typeof value !== "object" ||
    value === null ||
    Array.isArray(value) ||
    !hasExactKeys(value, ["userId", "workspaceId", "sessionId"])
  ) {
    return false;
  }

  return (
    isNonEmptyString(value.userId) &&
    isNonEmptyString(value.workspaceId) &&
    isNonEmptyString(value.sessionId)
  );
}

function hasExactKeys(
  value: object,
  keys: readonly string[],
): value is Record<string, unknown> {
  const record = value as Record<string, unknown>;
  const ownKeys = Object.keys(record);
  return ownKeys.length === keys.length && keys.every((key) => key in record);
}

function isNonEmptyString(value: unknown): value is string {
  return typeof value === "string" && value.length > 0;
}
