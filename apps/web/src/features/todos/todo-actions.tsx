"use client";

import { useState } from "react";

import { completeTodo, confirmAction, createConfirmation } from "./fetch-todos";
import type { Todo } from "./fetch-todos";

const errorMessages: Record<string, string> = {
  conflict: "待办已被更新,请刷新后重试。",
  not_found: "待办不存在或已被删除。",
  unavailable: "服务暂时不可用,请稍后再试。",
  validation_error: "请求无效,请刷新后重试。",
};

// TodoActions offers complete plus the two-step confirmation-gated delete.
export function TodoActions({
  todo,
  fetcher = fetch,
  onChanged,
}: {
  todo: Todo;
  fetcher?: typeof fetch;
  onChanged: () => void;
}): React.JSX.Element {
  const [confirming, setConfirming] = useState<string | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);

  async function handleComplete(): Promise<void> {
    setBusy(true);
    setError(null);
    const outcome = await completeTodo("", fetcher, todo.id, todo.version);
    setBusy(false);
    if (outcome.ok) {
      onChanged();
      return;
    }
    setError(errorMessages[outcome.error ?? "unavailable"]);
  }

  async function handleDeleteRequest(): Promise<void> {
    setBusy(true);
    setError(null);
    const outcome = await createConfirmation(
      "",
      fetcher,
      "todo.delete",
      todo.id,
    );
    setBusy(false);
    if (outcome.ok && outcome.confirmationId) {
      setConfirming(outcome.confirmationId);
      return;
    }
    setError(errorMessages[outcome.error ?? "unavailable"]);
  }

  async function handleDeleteConfirm(): Promise<void> {
    if (!confirming) {
      return;
    }
    setBusy(true);
    setError(null);
    const outcome = await confirmAction("", fetcher, confirming);
    setBusy(false);
    if (outcome.ok) {
      setConfirming(null);
      onChanged();
      return;
    }
    setError(errorMessages[outcome.error ?? "unavailable"]);
  }

  return (
    <span className="todo-actions">
      {todo.status === "pending" ? (
        <button
          disabled={busy}
          onClick={() => void handleComplete()}
          type="button"
        >
          完成
        </button>
      ) : null}
      {confirming === null ? (
        <button
          disabled={busy}
          onClick={() => void handleDeleteRequest()}
          type="button"
        >
          删除
        </button>
      ) : (
        <span className="todo-confirm-step">
          <button
            disabled={busy}
            onClick={() => void handleDeleteConfirm()}
            type="button"
          >
            确认删除
          </button>
          <button
            disabled={busy}
            onClick={() => setConfirming(null)}
            type="button"
          >
            取消
          </button>
        </span>
      )}
      {error ? (
        <span aria-live="polite" className="todo-error" role="alert">
          {error}
        </span>
      ) : null}
    </span>
  );
}
