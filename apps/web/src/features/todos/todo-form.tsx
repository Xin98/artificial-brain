"use client";

import { useState } from "react";

import { formatRFC3339UTC } from "../validation";
import { createTodo, updateTodo } from "./fetch-todos";
import type { Todo } from "./fetch-todos";

const errorMessages: Record<string, string> = {
  validation_error: "内容无效:标题需在 1 到 200 字之间,时间需合法。",
  conflict: "待办已被更新,请刷新后重试。",
  unavailable: "服务暂时不可用,请稍后再试。",
};

function browserTimezone(provider?: () => string): string {
  if (provider) {
    return provider();
  }
  try {
    return Intl.DateTimeFormat().resolvedOptions().timeZone;
  } catch {
    return "UTC";
  }
}

// dueLocalToUTC converts a datetime-local value to a second-precision UTC
// RFC3339 instant, or null when the field is empty.
function dueLocalToUTC(value: string): string | null {
  if (value === "") {
    return null;
  }
  const parsed = new Date(value);
  if (Number.isNaN(parsed.getTime())) {
    return null;
  }
  return formatRFC3339UTC(parsed);
}

function toLocalInput(dueAtUtc?: string): string {
  if (!dueAtUtc) {
    return "";
  }
  const parsed = new Date(dueAtUtc);
  if (Number.isNaN(parsed.getTime())) {
    return "";
  }
  return parsed.toISOString().slice(0, 16);
}

// TodoForm creates or edits a todo. The browser timezone travels with the
// request as timezoneAtInput (A1).
export function TodoForm({
  fetcher = fetch,
  editing,
  timezoneProvider,
  onDone,
}: {
  fetcher?: typeof fetch;
  editing?: Todo;
  timezoneProvider?: () => string;
  onDone: () => void;
}): React.JSX.Element {
  const [title, setTitle] = useState(editing?.title ?? "");
  const [description, setDescription] = useState(editing?.description ?? "");
  const [due, setDue] = useState(toLocalInput(editing?.dueAtUtc));
  const [error, setError] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);

  async function submit(event: React.FormEvent): Promise<void> {
    event.preventDefault();
    setBusy(true);
    setError(null);
    const timezone = browserTimezone(timezoneProvider);
    const dueAtUtc = dueLocalToUTC(due);
    if (due !== "" && dueAtUtc === null) {
      setError(errorMessages.validation_error);
      setBusy(false);
      return;
    }

    const outcome = editing
      ? await updateTodo("", fetcher, editing.id, {
          version: editing.version,
          title: title !== editing.title ? title : undefined,
          description:
            description !== (editing.description ?? "")
              ? description
              : undefined,
          dueAtUtc: dueAtUtc ?? null,
          timezoneAtInput: dueAtUtc ? timezone : undefined,
        })
      : await createTodo("", fetcher, {
          title,
          description: description === "" ? undefined : description,
          dueAtUtc: dueAtUtc ?? undefined,
          timezoneAtInput: dueAtUtc ? timezone : undefined,
        });
    setBusy(false);
    if (outcome.ok) {
      onDone();
      return;
    }
    setError(errorMessages[outcome.error ?? "unavailable"]);
  }

  return (
    <form
      aria-label={editing ? "编辑待办" : "新建待办"}
      className="todo-form"
      onSubmit={submit}
    >
      <div className="field">
        <label htmlFor="todo-title">标题</label>
        <input
          id="todo-title"
          maxLength={200}
          onChange={(event) => setTitle(event.target.value)}
          type="text"
          value={title}
        />
      </div>
      <div className="field">
        <label htmlFor="todo-description">描述</label>
        <input
          id="todo-description"
          onChange={(event) => setDescription(event.target.value)}
          type="text"
          value={description}
        />
      </div>
      <div className="form-row">
        <div className="field">
          <label htmlFor="todo-due">到期时间</label>
          <input
            id="todo-due"
            onChange={(event) => setDue(event.target.value)}
            type="datetime-local"
            value={due}
          />
        </div>
        <button className="btn-primary" disabled={busy} type="submit">
          {editing ? "保存" : "新建"}
        </button>
      </div>
      {error ? (
        <p aria-live="polite" className="todo-error" role="alert">
          {error}
        </p>
      ) : null}
    </form>
  );
}
