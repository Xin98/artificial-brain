import {
  hasAllowedKeys,
  isNonEmptyString,
  isRecord,
  isRFC3339,
  isStringOrUndefined,
  safeTimeout,
} from "../validation";

export interface SessionView {
  id: string;
  title: string;
  createdAt: string;
  updatedAt: string;
}

export interface SessionMessage {
  id: string;
  role: "user" | "assistant";
  body: string;
  resolvedIntent?: string;
  createdAt: string;
}

export interface SessionHistory {
  sessionId: string;
  title: string;
  messages: SessionMessage[];
}

export type SessionFailureReason =
  | "timeout"
  | "unauthenticated"
  | "not_found"
  | "rejected"
  | "server"
  | "invalid_response"
  | "network";

export type SessionListResult =
  | { ok: true; sessions: SessionView[] }
  | { ok: false; reason: SessionFailureReason; correlationId?: string };

export type SessionResult =
  | { ok: true; session: SessionView }
  | { ok: false; reason: SessionFailureReason; correlationId?: string };

export type SessionHistoryResult =
  | { ok: true; history: SessionHistory }
  | { ok: false; reason: SessionFailureReason; correlationId?: string };

export type SessionDeleteResult =
  | { ok: true }
  | { ok: false; reason: SessionFailureReason; correlationId?: string };

const SESSION_KEYS = ["id", "title", "createdAt", "updatedAt"] as const;
const MESSAGE_KEYS = [
  "id",
  "role",
  "body",
  "resolvedIntent",
  "createdAt",
] as const;

// Session management routes never wait on the model; a short budget keeps
// sidebar interactions snappy even when the API is degraded.
const DEFAULT_SESSION_TIMEOUT_MS = 10_000;

export async function listSessions(
  baseURL: string,
  fetcher: typeof fetch,
  timeoutMs = DEFAULT_SESSION_TIMEOUT_MS,
): Promise<SessionListResult> {
  return requestJSON(
    fetcher,
    `${baseURL}/api/v1/conversation/sessions`,
    { method: "GET" },
    timeoutMs,
    (payload) => {
      if (
        !isRecord(payload) ||
        !hasAllowedKeys(payload, ["sessions"]) ||
        !Array.isArray(payload.sessions) ||
        !payload.sessions.every(isSessionView)
      ) {
        return undefined;
      }
      return { sessions: payload.sessions };
    },
  );
}

export async function createSession(
  baseURL: string,
  fetcher: typeof fetch,
  title?: string,
  timeoutMs = DEFAULT_SESSION_TIMEOUT_MS,
): Promise<SessionResult> {
  const body = title ? JSON.stringify({ title }) : "{}";
  return requestSession(
    fetcher,
    `${baseURL}/api/v1/conversation/sessions`,
    { method: "POST", body },
    timeoutMs,
  );
}

export async function renameSession(
  baseURL: string,
  fetcher: typeof fetch,
  sessionId: string,
  title: string,
  timeoutMs = DEFAULT_SESSION_TIMEOUT_MS,
): Promise<SessionResult> {
  return requestSession(
    fetcher,
    `${baseURL}/api/v1/conversation/sessions/${encodeURIComponent(sessionId)}`,
    { method: "PATCH", body: JSON.stringify({ title }) },
    timeoutMs,
  );
}

export async function deleteSession(
  baseURL: string,
  fetcher: typeof fetch,
  sessionId: string,
  timeoutMs = DEFAULT_SESSION_TIMEOUT_MS,
): Promise<SessionDeleteResult> {
  return requestJSON(
    fetcher,
    `${baseURL}/api/v1/conversation/sessions/${encodeURIComponent(sessionId)}`,
    { method: "DELETE" },
    timeoutMs,
    (_payload, response) => (response.status === 204 ? {} : undefined),
  );
}

export async function fetchSessionMessages(
  baseURL: string,
  fetcher: typeof fetch,
  sessionId: string,
  timeoutMs = DEFAULT_SESSION_TIMEOUT_MS,
): Promise<SessionHistoryResult> {
  return requestJSON(
    fetcher,
    `${baseURL}/api/v1/conversation/sessions/${encodeURIComponent(sessionId)}/messages`,
    { method: "GET" },
    timeoutMs,
    (payload) => {
      if (
        !isRecord(payload) ||
        !hasAllowedKeys(payload, ["sessionId", "title", "messages"]) ||
        !isNonEmptyString(payload.sessionId) ||
        !isNonEmptyString(payload.title) ||
        !Array.isArray(payload.messages) ||
        !payload.messages.every(isSessionMessage)
      ) {
        return undefined;
      }
      return {
        history: {
          sessionId: payload.sessionId,
          title: payload.title,
          messages: payload.messages,
        },
      };
    },
  );
}

async function requestSession(
  fetcher: typeof fetch,
  url: string,
  init: { method: string; body: string },
  timeoutMs: number,
): Promise<SessionResult> {
  return requestJSON(fetcher, url, init, timeoutMs, (payload) =>
    isSessionView(payload) ? { session: payload } : undefined,
  );
}

// requestJSON performs one fetch and maps the outcome onto the shared
// success/failure envelope. `accept` parses a successful payload; returning
// undefined classifies the body as invalid_response.
async function requestJSON<T extends object>(
  fetcher: typeof fetch,
  url: string,
  init: { method: string; body?: string },
  timeoutMs: number,
  accept: (payload: unknown, response: Response) => T | undefined,
): Promise<
  | ({ ok: true } & T)
  | { ok: false; reason: SessionFailureReason; correlationId?: string }
> {
  let response: Response;
  try {
    response = await fetcher(url, {
      ...init,
      signal: AbortSignal.timeout(safeTimeout(timeoutMs)),
      cache: "no-store",
      headers: {
        accept: "application/json",
        ...(init.body !== undefined
          ? { "content-type": "application/json" }
          : {}),
      },
    });
  } catch (error) {
    return {
      ok: false,
      reason: isDeadlineError(error) ? "timeout" : "network",
    };
  }

  if (!response.ok) {
    return classifyFailureResponse(response);
  }

  try {
    const payload: unknown =
      response.status === 204 ? null : await response.json();
    const accepted = accept(payload, response);
    return accepted === undefined
      ? { ok: false, reason: "invalid_response" }
      : { ok: true, ...accepted };
  } catch {
    return { ok: false, reason: "invalid_response" };
  }
}

async function classifyFailureResponse(response: Response): Promise<{
  ok: false;
  reason: SessionFailureReason;
  correlationId?: string;
}> {
  let correlationId: string | undefined;
  try {
    const payload: unknown = await response.json();
    if (isRecord(payload) && isNonEmptyString(payload.correlationId)) {
      correlationId = payload.correlationId;
    }
  } catch {
    // A malformed error body must not hide the useful HTTP classification.
  }

  let reason: SessionFailureReason;
  if (response.status === 401) {
    reason = "unauthenticated";
  } else if (response.status === 404) {
    reason = "not_found";
  } else if (response.status >= 500) {
    reason = "server";
  } else {
    reason = "rejected";
  }

  return correlationId
    ? { ok: false, reason, correlationId }
    : { ok: false, reason };
}

function isDeadlineError(error: unknown): boolean {
  return (
    error instanceof DOMException &&
    (error.name === "TimeoutError" || error.name === "AbortError")
  );
}

function isSessionView(value: unknown): value is SessionView {
  return (
    isRecord(value) &&
    hasAllowedKeys(value, [...SESSION_KEYS]) &&
    isNonEmptyString(value.id) &&
    isNonEmptyString(value.title) &&
    isRFC3339(value.createdAt) &&
    isRFC3339(value.updatedAt)
  );
}

function isSessionMessage(value: unknown): value is SessionMessage {
  return (
    isRecord(value) &&
    hasAllowedKeys(value, [...MESSAGE_KEYS]) &&
    isNonEmptyString(value.id) &&
    (value.role === "user" || value.role === "assistant") &&
    typeof value.body === "string" &&
    isStringOrUndefined(value.resolvedIntent) &&
    isRFC3339(value.createdAt)
  );
}
