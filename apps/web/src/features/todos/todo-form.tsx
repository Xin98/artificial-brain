"use client";

import { useId, useRef, useState } from "react";

import { formatRFC3339UTC } from "../validation";
import { createTodo, updateTodo } from "./fetch-todos";
import type { Todo } from "./fetch-todos";

const errorMessages: Record<string, string> = {
  validation_error: "内容无效:标题需在 1 到 200 字之间,时间需合法。",
  conflict: "待办已被更新,请刷新后重试。",
  not_found: "待办已被删除，请刷新列表。",
  unavailable: "请求结果暂时无法确认，请刷新列表检查是否已保存，再尝试提交。",
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
  const pad = (value: number) => String(value).padStart(2, "0");
  return `${parsed.getFullYear()}-${pad(parsed.getMonth() + 1)}-${pad(parsed.getDate())}T${pad(parsed.getHours())}:${pad(parsed.getMinutes())}`;
}

// TodoForm creates or edits a todo. The browser timezone travels with the
// request as timezoneAtInput (A1).
export function TodoForm({
  fetcher = fetch,
  editing,
  timezoneProvider,
  onDone,
  onCancel,
}: {
  fetcher?: typeof fetch;
  editing?: Todo;
  timezoneProvider?: () => string;
  onDone: () => void;
  onCancel?: () => void;
}): React.JSX.Element {
  const [title, setTitle] = useState(editing?.title ?? "");
  const [description, setDescription] = useState(editing?.description ?? "");
  const [due, setDue] = useState(toLocalInput(editing?.dueAtUtc));
  const [error, setError] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);
  const submitting = useRef(false);
  const [success, setSuccess] = useState<string | null>(null);
  const id = useId();

  async function submit(event: React.FormEvent): Promise<void> {
    event.preventDefault();
    if (submitting.current) return;
    submitting.current = true;
    setSuccess(null);
    setBusy(true);
    setError(null);
    const timezone = browserTimezone(timezoneProvider);
    const dueAtUtc = dueLocalToUTC(due);
    if (due !== "" && dueAtUtc === null) {
      setError(errorMessages.validation_error);
      setBusy(false);
      submitting.current = false;
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
          dueAtUtc:
            due === toLocalInput(editing.dueAtUtc)
              ? editing.dueAtUtc
              : (dueAtUtc ?? null),
          timezoneAtInput:
            dueAtUtc && due !== toLocalInput(editing.dueAtUtc)
              ? timezone
              : undefined,
        })
      : await createTodo("", fetcher, {
          title,
          description: description === "" ? undefined : description,
          dueAtUtc: dueAtUtc ?? undefined,
          timezoneAtInput: dueAtUtc ? timezone : undefined,
        });
    setBusy(false);
    submitting.current = false;
    if (outcome.ok) {
      if (!editing) {
        setTitle("");
        setDescription("");
        setDue("");
      }
      setSuccess(editing ? "待办已保存。" : "待办已创建。可继续新建下一条。");
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
        <label htmlFor={`${id}-title`}>标题</label>
        <input
          id={`${id}-title`}
          maxLength={200}
          onChange={(event) => setTitle(event.target.value)}
          type="text"
          value={title}
        />
      </div>
      <div className="field">
        <label htmlFor={`${id}-description`}>描述</label>
        <input
          id={`${id}-description`}
          onChange={(event) => setDescription(event.target.value)}
          type="text"
          value={description}
        />
      </div>
      <div className="form-row">
        <div className="field">
          <label htmlFor={`${id}-due`}>到期时间</label>
          <input
            id={`${id}-due`}
            onChange={(event) => setDue(event.target.value)}
            type="datetime-local"
            value={due}
          />
        </div>
        <button className="btn-primary" disabled={busy} type="submit">
          {editing ? "保存" : "新建"}
        </button>
      </div>
      {editing && onCancel ? (
        <button
          className="btn-ghost"
          disabled={busy}
          onClick={onCancel}
          type="button"
        >
          取消编辑
        </button>
      ) : null}
      {success ? <p role="status">{success}</p> : null}
      {error ? (
        <p aria-live="polite" className="todo-error" role="alert">
          {error}
        </p>
      ) : null}
    </form>
  );
}
