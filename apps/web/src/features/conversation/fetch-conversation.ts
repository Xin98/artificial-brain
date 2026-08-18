import {
  hasAllowedKeys,
  isInteger,
  isNonEmptyString,
  isRecord,
  isRFC3339,
  isStringOrUndefined,
  safeTimeout,
} from "../validation";

export const CONVERSATION_KINDS = [
  "todo_created",
  "clarification",
  "candidates",
  "confirmation_required",
  "todo_list",
  "todo_deleted",
  "not_found",
  "unsupported",
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
  todo?: { id: string; title: string };
  resolvedDueAtUtc?: string;
  localEcho?: string;
  timezoneEcho?: string;
  missingFields?: string[];
  candidates?: ConversationCandidate[];
  confirmationId?: string;
  expiresAt?: string;
  todos?: Array<{ id: string; title: string }>;
  todoId?: string;
}

const ALLOWED_KEYS = [
  "kind",
  "correlationId",
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

export async function postConversationMessage(
  baseURL: string,
  fetcher: typeof fetch,
  text: string,
  timezone: string,
  timeoutMs = 15000,
): Promise<ConversationResponse | null> {
  try {
    const response = await fetcher(`${baseURL}/api/v1/conversation/messages`, {
      method: "POST",
      signal: AbortSignal.timeout(safeTimeout(timeoutMs)),
      cache: "no-store",
      headers: {
        "content-type": "application/json",
        accept: "application/json",
      },
      body: JSON.stringify({ text, timezone }),
    });
    if (!response.ok) {
      return null;
    }
    const payload: unknown = await response.json();
    return isConversationResponse(payload) ? payload : null;
  } catch {
    return null;
  }
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
    !isStringOrUndefined(value.localEcho) ||
    !isStringOrUndefined(value.timezoneEcho)
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
      !value.todos.every(
        (item) =>
          isRecord(item) &&
          isNonEmptyString(item.id) &&
          typeof item.title === "string",
      )
    ) {
      return false;
    }
  }
  if (value.todo !== undefined) {
    if (
      !isRecord(value.todo) ||
      !isNonEmptyString(value.todo.id) ||
      typeof value.todo.title !== "string"
    ) {
      return false;
    }
  }
  return true;
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
