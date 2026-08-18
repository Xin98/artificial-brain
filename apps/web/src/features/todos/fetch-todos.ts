import {
  hasAllowedKeys,
  hasExactKeys,
  isBoolean,
  isInteger,
  isNonEmptyString,
  isRecord,
  isRFC3339,
  isStringOrUndefined,
  readErrorPayload,
  safeTimeout,
  classifyErrorPayload,
} from "../validation";

export interface Todo {
  id: string;
  title: string;
  description?: string;
  dueAtUtc?: string;
  timezoneAtInput?: string;
  status: "pending" | "completed";
  overdue: boolean;
  reminderVersion: number;
  version: number;
  createdAt: string;
  updatedAt: string;
  completedAt?: string;
  deletedAt?: string;
}

export interface TodoFilters {
  keyword?: string;
  status?: string;
  dueFrom?: string;
  dueTo?: string;
  noDue?: boolean;
}

export type TodoErrorCode =
  "validation_error" | "conflict" | "not_found" | "unavailable";

export interface TodoOutcome {
  ok: boolean;
  todo?: Todo;
  error?: TodoErrorCode;
}

export interface ConfirmationOutcome {
  ok: boolean;
  confirmationId?: string;
  expiresAt?: string;
  error?: TodoErrorCode;
}

export interface ConfirmActionOutcome {
  ok: boolean;
  todoId?: string;
  error?: TodoErrorCode;
}

const TODO_REQUIRED = [
  "id",
  "title",
  "status",
  "overdue",
  "reminderVersion",
  "version",
  "createdAt",
  "updatedAt",
] as const;
const TODO_OPTIONAL = [
  "description",
  "dueAtUtc",
  "timezoneAtInput",
  "completedAt",
  "deletedAt",
] as const;

export async function listTodos(
  baseURL: string,
  fetcher: typeof fetch,
  filters: TodoFilters,
  timeoutMs = 5000,
): Promise<Todo[] | null> {
  try {
    const query = new URLSearchParams();
    if (filters.keyword) {
      query.set("keyword", filters.keyword);
    }
    if (filters.status) {
      query.set("status", filters.status);
    }
    if (filters.dueFrom) {
      query.set("dueFrom", filters.dueFrom);
    }
    if (filters.dueTo) {
      query.set("dueTo", filters.dueTo);
    }
    if (filters.noDue) {
      query.set("noDue", "true");
    }
    const suffix = query.size > 0 ? `?${query.toString()}` : "";
    const response = await fetcher(`${baseURL}/api/v1/todos${suffix}`, {
      signal: AbortSignal.timeout(safeTimeout(timeoutMs)),
      cache: "no-store",
      headers: { accept: "application/json" },
    });
    if (!response.ok) {
      return null;
    }
    const payload: unknown = await response.json();
    if (
      !isRecord(payload) ||
      !hasExactKeys(payload, ["todos"]) ||
      !Array.isArray(payload.todos)
    ) {
      return null;
    }
    const todos: Todo[] = [];
    for (const item of payload.todos) {
      if (!isTodo(item)) {
        return null;
      }
      todos.push(item);
    }
    return todos;
  } catch {
    return null;
  }
}

export async function createTodo(
  baseURL: string,
  fetcher: typeof fetch,
  input: {
    title: string;
    description?: string;
    dueAtUtc?: string;
    timezoneAtInput?: string;
  },
  timeoutMs = 5000,
): Promise<TodoOutcome> {
  return mutateTodo(
    () =>
      fetcher(`${baseURL}/api/v1/todos`, {
        method: "POST",
        signal: AbortSignal.timeout(safeTimeout(timeoutMs)),
        cache: "no-store",
        headers: {
          "content-type": "application/json",
          accept: "application/json",
        },
        body: JSON.stringify(input),
      }),
    201,
  );
}

export interface UpdateTodoBody {
  version: number;
  title?: string;
  description?: string;
  dueAtUtc?: string | null;
  timezoneAtInput?: string;
}

export async function updateTodo(
  baseURL: string,
  fetcher: typeof fetch,
  todoId: string,
  body: UpdateTodoBody,
  timeoutMs = 5000,
): Promise<TodoOutcome> {
  return mutateTodo(
    () =>
      fetcher(`${baseURL}/api/v1/todos/${encodeURIComponent(todoId)}`, {
        method: "PATCH",
        signal: AbortSignal.timeout(safeTimeout(timeoutMs)),
        cache: "no-store",
        headers: {
          "content-type": "application/json",
          accept: "application/json",
        },
        body: JSON.stringify(body),
      }),
    200,
  );
}

export async function completeTodo(
  baseURL: string,
  fetcher: typeof fetch,
  todoId: string,
  version: number,
  timeoutMs = 5000,
): Promise<TodoOutcome> {
  return mutateTodo(
    () =>
      fetcher(
        `${baseURL}/api/v1/todos/${encodeURIComponent(todoId)}/complete`,
        {
          method: "POST",
          signal: AbortSignal.timeout(safeTimeout(timeoutMs)),
          cache: "no-store",
          headers: {
            "content-type": "application/json",
            accept: "application/json",
          },
          body: JSON.stringify({ version }),
        },
      ),
    200,
  );
}

export async function createConfirmation(
  baseURL: string,
  fetcher: typeof fetch,
  intent: string,
  todoId: string,
  timeoutMs = 5000,
): Promise<ConfirmationOutcome> {
  try {
    const response = await fetcher(`${baseURL}/api/v1/confirmations`, {
      method: "POST",
      signal: AbortSignal.timeout(safeTimeout(timeoutMs)),
      cache: "no-store",
      headers: {
        "content-type": "application/json",
        accept: "application/json",
      },
      body: JSON.stringify({ intent, todoId }),
    });
    if (response.status === 201) {
      const payload: unknown = await response.json();
      if (
        isRecord(payload) &&
        hasExactKeys(payload, ["confirmationId", "expiresAt"]) &&
        isNonEmptyString(payload.confirmationId) &&
        isRFC3339(payload.expiresAt)
      ) {
        return {
          ok: true,
          confirmationId: payload.confirmationId,
          expiresAt: payload.expiresAt,
        };
      }
      return { ok: false, error: "unavailable" };
    }
    return { ok: false, error: await classifyResponse(response) };
  } catch {
    return { ok: false, error: "unavailable" };
  }
}

export async function confirmAction(
  baseURL: string,
  fetcher: typeof fetch,
  confirmationId: string,
  timeoutMs = 5000,
): Promise<ConfirmActionOutcome> {
  try {
    const response = await fetcher(
      `${baseURL}/api/v1/confirmations/${encodeURIComponent(confirmationId)}/confirm`,
      {
        method: "POST",
        signal: AbortSignal.timeout(safeTimeout(timeoutMs)),
        cache: "no-store",
        headers: {
          "content-type": "application/json",
          accept: "application/json",
        },
        body: JSON.stringify({}),
      },
    );
    if (response.status === 200) {
      const payload: unknown = await response.json();
      if (
        isRecord(payload) &&
        payload.kind === "todo_deleted" &&
        isNonEmptyString(payload.todoId)
      ) {
        return { ok: true, todoId: payload.todoId };
      }
      return { ok: false, error: "unavailable" };
    }
    return { ok: false, error: await classifyResponse(response) };
  } catch {
    return { ok: false, error: "unavailable" };
  }
}

async function mutateTodo(
  request: () => Promise<Response>,
  successStatus: number,
): Promise<TodoOutcome> {
  try {
    const response = await request();
    if (response.status === successStatus) {
      const payload: unknown = await response.json();
      return isTodo(payload)
        ? { ok: true, todo: payload }
        : { ok: false, error: "unavailable" };
    }
    return { ok: false, error: await classifyResponse(response) };
  } catch {
    return { ok: false, error: "unavailable" };
  }
}

async function classifyResponse(response: Response): Promise<TodoErrorCode> {
  if (response.status === 404) {
    return "not_found";
  }
  const payload = await readErrorPayload(response);
  const classified = classifyErrorPayload(payload);
  if (classified.code === "validation_error") {
    return "validation_error";
  }
  if (classified.code === "conflict") {
    return "conflict";
  }
  return "unavailable";
}

function isTodo(value: unknown): value is Todo {
  if (!isRecord(value)) {
    return false;
  }
  const keys = [...TODO_REQUIRED, ...TODO_OPTIONAL];
  if (!hasAllowedKeys(value, keys)) {
    return false;
  }
  for (const key of TODO_REQUIRED) {
    if (!(key in value)) {
      return false;
    }
  }
  if (
    !isNonEmptyString(value.id) ||
    typeof value.title !== "string" ||
    value.title.length === 0 ||
    value.title.length > 200 ||
    (value.status !== "pending" && value.status !== "completed") ||
    !isBoolean(value.overdue) ||
    !isInteger(value.reminderVersion) ||
    !isInteger(value.version) ||
    !isRFC3339(value.createdAt) ||
    !isRFC3339(value.updatedAt) ||
    !isStringOrUndefined(value.description) ||
    !isStringOrUndefined(value.timezoneAtInput)
  ) {
    return false;
  }
  for (const key of ["dueAtUtc", "completedAt", "deletedAt"] as const) {
    const candidate = value[key];
    if (candidate !== undefined && !isRFC3339(candidate)) {
      return false;
    }
  }
  return true;
}
