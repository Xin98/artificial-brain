import {
  hasAllowedKeys,
  isInteger,
  isNonEmptyString,
  isRecord,
  isRFC3339,
  isStringOrUndefined,
  safeTimeout,
} from "../validation";
import { recoverExpiredSession } from "../auth/session-recovery";

export const CONVERSATION_KINDS = [
  "todo_created",
  "clarification",
  "candidates",
  "confirmation_required",
  "todo_list",
  "todo_deleted",
  "not_found",
  "unsupported",
  "chat",
] as const;

export type ConversationKind = (typeof CONVERSATION_KINDS)[number];

export interface ConversationCandidate {
  todoId: string;
  title: string;
  dueAtUtc?: string;
  version: number;
}

export interface ConversationResponse {
  kind: ConversationKind;
  correlationId: string;
  reply?: string;
  sessionId?: string;
  todo?: ConversationTodo;
  resolvedDueAtUtc?: string;
  localEcho?: string;
  timezoneEcho?: string;
  missingFields?: string[];
  candidates?: ConversationCandidate[];
  confirmationId?: string;
  expiresAt?: string;
  todos?: ConversationTodo[];
  todoId?: string;
}

export interface ConversationTodo {
  id: string;
  title: string;
  description?: string;
  dueAtUtc?: string;
  status?: "pending" | "completed";
  reminderScheduled?: boolean;
  reminderChannels?: string[];
}

export type ConversationFailureReason =
  | "timeout"
  | "unauthenticated"
  | "rate_limited"
  | "rejected"
  | "server"
  | "invalid_response"
  | "network";

export type ConversationRequestResult =
  | { ok: true; response: ConversationResponse }
  | {
      ok: false;
      reason: ConversationFailureReason;
      correlationId?: string;
    };

const ALLOWED_KEYS = [
  "kind",
  "correlationId",
  "reply",
  "sessionId",
  "todo",
  "resolvedDueAtUtc",
  "localEcho",
  "timezoneEcho",
  "missingFields",
  "candidates",
  "confirmationId",
  "expiresAt",
  "todos",
  "todoId",
] as const;

// The private deployment gives the model request a 30-second total budget.
// Keep a small transport margin so the browser does not cancel first.
const DEFAULT_CONVERSATION_TIMEOUT_MS = 35_000;

export async function postConversationMessage(
  baseURL: string,
  fetcher: typeof fetch,
  text: string,
  timezone: string,
  sessionId?: string,
  timeoutMs = DEFAULT_CONVERSATION_TIMEOUT_MS,
): Promise<ConversationRequestResult> {
  let response: Response;
  try {
    response = await fetcher(`${baseURL}/api/v1/conversation/messages`, {
      method: "POST",
      signal: AbortSignal.timeout(safeTimeout(timeoutMs)),
      cache: "no-store",
      headers: {
        "content-type": "application/json",
        accept: "application/json",
      },
      body: JSON.stringify(
        sessionId ? { text, timezone, sessionId } : { text, timezone },
      ),
    });
  } catch (error) {
    return {
      ok: false,
      reason: isDeadlineError(error) ? "timeout" : "network",
    };
  }

  if (!response.ok) {
    recoverExpiredSession(response);
    return classifyFailureResponse(response);
  }

  try {
    const payload: unknown = await response.json();
    return isConversationResponse(payload)
      ? { ok: true, response: payload }
      : { ok: false, reason: "invalid_response" };
  } catch {
    return { ok: false, reason: "invalid_response" };
  }
}

async function classifyFailureResponse(
  response: Response,
): Promise<ConversationRequestResult> {
  let correlationId: string | undefined;
  try {
    const payload: unknown = await response.json();
    if (isRecord(payload) && isNonEmptyString(payload.correlationId)) {
      correlationId = payload.correlationId;
    }
  } catch {
    // A malformed error body must not hide the useful HTTP classification.
  }

  let reason: ConversationFailureReason;
  if (response.status === 401) {
    reason = "unauthenticated";
  } else if (response.status === 429) {
    reason = "rate_limited";
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

function isConversationResponse(value: unknown): value is ConversationResponse {
  if (!isRecord(value) || !hasAllowedKeys(value, [...ALLOWED_KEYS])) {
    return false;
  }
  if (
    typeof value.kind !== "string" ||
    !CONVERSATION_KINDS.includes(value.kind as ConversationKind) ||
    typeof value.correlationId !== "string"
  ) {
    return false;
  }
  if (
    value.resolvedDueAtUtc !== undefined &&
    !isRFC3339(value.resolvedDueAtUtc)
  ) {
    return false;
  }
  if (value.expiresAt !== undefined && !isRFC3339(value.expiresAt)) {
    return false;
  }
  if (
    (value.confirmationId !== undefined &&
      !isNonEmptyString(value.confirmationId)) ||
    (value.todoId !== undefined && !isNonEmptyString(value.todoId)) ||
    (value.sessionId !== undefined && !isNonEmptyString(value.sessionId))
  ) {
    return false;
  }
  if (
    !isStringOrUndefined(value.localEcho) ||
    !isStringOrUndefined(value.timezoneEcho) ||
    !isStringOrUndefined(value.reply)
  ) {
    return false;
  }
  if (value.missingFields !== undefined) {
    if (
      !Array.isArray(value.missingFields) ||
      value.missingFields.some((item) => typeof item !== "string")
    ) {
      return false;
    }
  }
  if (value.candidates !== undefined) {
    if (
      !Array.isArray(value.candidates) ||
      !value.candidates.every(isCandidate)
    ) {
      return false;
    }
  }
  if (value.todos !== undefined) {
    if (
      !Array.isArray(value.todos) ||
      !value.todos.every((item) => isConversationTodo(item))
    ) {
      return false;
    }
  }
  if (value.todo !== undefined) {
    if (!isConversationTodo(value.todo)) {
      return false;
    }
  }
  return hasKindSpecificShape(value, value.kind as ConversationKind);
}

function hasKindSpecificShape(
  value: Record<string, unknown>,
  kind: ConversationKind,
): boolean {
  const base = ["kind", "correlationId", "sessionId"];
  switch (kind) {
    case "todo_created":
      return (
        hasAllowedKeys(value, [
          ...base,
          "todo",
          "resolvedDueAtUtc",
          "localEcho",
          "timezoneEcho",
        ]) && value.todo !== undefined
      );
    case "chat":
      return (
        hasAllowedKeys(value, [...base, "reply"]) &&
        isNonEmptyString(value.reply)
      );
    case "clarification":
      return hasAllowedKeys(value, [...base, "missingFields", "reply"]);
    case "candidates":
      return (
        hasAllowedKeys(value, [...base, "candidates"]) &&
        Array.isArray(value.candidates) &&
        value.candidates.length > 0
      );
    case "confirmation_required":
      return (
        hasAllowedKeys(value, [
          ...base,
          "confirmationId",
          "expiresAt",
          "todo",
          "candidates",
        ]) &&
        isNonEmptyString(value.confirmationId) &&
        isRFC3339(value.expiresAt)
      );
    case "todo_list":
      return hasAllowedKeys(value, [...base, "todos"]);
    case "todo_deleted":
      return (
        hasAllowedKeys(value, [...base, "todoId"]) &&
        isNonEmptyString(value.todoId)
      );
    case "not_found":
    case "unsupported":
      return hasAllowedKeys(value, base);
  }
}

function isConversationTodo(value: unknown): value is ConversationTodo {
  return (
    isRecord(value) &&
    isNonEmptyString(value.id) &&
    typeof value.title === "string" &&
    isStringOrUndefined(value.description) &&
    (value.dueAtUtc === undefined || isRFC3339(value.dueAtUtc)) &&
    (value.status === undefined ||
      value.status === "pending" ||
      value.status === "completed") &&
    (value.reminderScheduled === undefined ||
      typeof value.reminderScheduled === "boolean") &&
    (value.reminderChannels === undefined ||
      (Array.isArray(value.reminderChannels) &&
        value.reminderChannels.every(
          (channel) => channel === "email" || channel === "sms",
        )))
  );
}

function isCandidate(value: unknown): value is ConversationCandidate {
  return (
    isRecord(value) &&
    isNonEmptyString(value.todoId) &&
    typeof value.title === "string" &&
    isInteger(value.version) &&
    (value.dueAtUtc === undefined || isRFC3339(value.dueAtUtc))
  );
}
