"use client";

import { useState } from "react";

import { confirmAction, createConfirmation } from "../todos/fetch-todos";
import { postConversationMessage } from "./fetch-conversation";
import type { ConversationResponse } from "./fetch-conversation";

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

interface PendingConfirmation {
  confirmationId: string;
  expiresAt?: string;
}

// ChatPanel sends one turn with the browser timezone and renders the
// resolved kind. Candidate selection and confirmation stay two-step; the
// panel never renders raw errors or internal URLs.
export function ChatPanel({
  fetcher = fetch,
  timezoneProvider,
}: {
  fetcher?: typeof fetch;
  timezoneProvider?: () => string;
}): React.JSX.Element {
  const [text, setText] = useState("");
  const [response, setResponse] = useState<ConversationResponse | null>(null);
  const [pending, setPending] = useState<PendingConfirmation | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);

  async function send(event: React.FormEvent): Promise<void> {
    event.preventDefault();
    setBusy(true);
    setError(null);
    const result = await postConversationMessage(
      "",
      fetcher,
      text,
      browserTimezone(timezoneProvider),
    );
    setBusy(false);
    if (result === null) {
      setResponse(null);
      setError("对话服务暂时不可用,请稍后再试。");
      return;
    }
    setText("");
    setResponse(result);
    if (result.kind === "confirmation_required" && result.confirmationId) {
      setPending({
        confirmationId: result.confirmationId,
        expiresAt: result.expiresAt,
      });
    } else {
      setPending(null);
    }
  }

  async function pickCandidate(todoId: string): Promise<void> {
    setBusy(true);
    setError(null);
    const outcome = await createConfirmation(
      "",
      fetcher,
      "todo.delete",
      todoId,
    );
    setBusy(false);
    if (outcome.ok && outcome.confirmationId) {
      setPending({
        confirmationId: outcome.confirmationId,
        expiresAt: outcome.expiresAt,
      });
      return;
    }
    setError("确认请求失败,请稍后再试。");
  }

  async function confirmPending(): Promise<void> {
    if (!pending) {
      return;
    }
    setBusy(true);
    setError(null);
    const outcome = await confirmAction("", fetcher, pending.confirmationId);
    setBusy(false);
    if (outcome.ok) {
      setPending(null);
      setResponse({
        kind: "todo_deleted",
        correlationId: "",
        todoId: outcome.todoId,
      });
      return;
    }
    setError("确认失败,可能已过期或被使用。");
  }

  return (
    <section aria-label="对话" className="chat-panel">
      <form className="chat-input" onSubmit={send}>
        <label htmlFor="chat-text">消息</label>
        <input
          id="chat-text"
          maxLength={1000}
          onChange={(event) => setText(event.target.value)}
          type="text"
          value={text}
        />
        <button disabled={busy} type="submit">
          发送
        </button>
      </form>
      {error ? (
        <p aria-live="polite" className="chat-error" role="alert">
          {error}
        </p>
      ) : null}
      {response
        ? renderResponse(response, (todoId) => void pickCandidate(todoId))
        : null}
      {pending ? (
        <div className="chat-confirm">
          <p>
            确认删除该待办吗?
            {pending.expiresAt ? (
              <span>
                确认在{" "}
                <time dateTime={pending.expiresAt}>
                  {new Date(pending.expiresAt).toLocaleString()}
                </time>{" "}
                前有效。
              </span>
            ) : null}
          </p>
          <button
            disabled={busy}
            onClick={() => void confirmPending()}
            type="button"
          >
            确认删除
          </button>
          <button
            disabled={busy}
            onClick={() => setPending(null)}
            type="button"
          >
            取消
          </button>
        </div>
      ) : null}
    </section>
  );
}

function renderResponse(
  response: ConversationResponse,
  onPickCandidate: (todoId: string) => void,
): React.JSX.Element {
  switch (response.kind) {
    case "todo_created":
      return (
        <p className="chat-result">
          已创建待办「{response.todo?.title ?? ""}」
          {response.resolvedDueAtUtc && response.localEcho ? (
            <span>
              ,提醒时间 {response.localEcho}
              {response.timezoneEcho ? `(${response.timezoneEcho})` : ""}
            </span>
          ) : null}
        </p>
      );
    case "clarification":
      return (
        <p className="chat-result">
          需要补充信息
          {response.missingFields && response.missingFields.length > 0
            ? `:${response.missingFields.join("、")}`
            : "。"}
        </p>
      );
    case "candidates":
      return (
        <div className="chat-result">
          <p>找到多个待办,请选择:</p>
          <ul>
            {(response.candidates ?? []).map((candidate) => (
              <li key={candidate.todoId}>
                <button
                  onClick={() => onPickCandidate(candidate.todoId)}
                  type="button"
                >
                  {candidate.title}
                </button>
              </li>
            ))}
          </ul>
        </div>
      );
    case "confirmation_required":
      return <p className="chat-result">请在下方确认删除。</p>;
    case "todo_list":
      return (
        <div className="chat-result">
          <p>待办列表:</p>
          <ul>
            {(response.todos ?? []).map((todo) => (
              <li key={todo.id}>{todo.title}</li>
            ))}
          </ul>
        </div>
      );
    case "todo_deleted":
      return <p className="chat-result">已删除待办。</p>;
    case "not_found":
      return <p className="chat-result">没有找到匹配的待办。</p>;
    case "unsupported":
      return <p className="chat-result">这个请求暂时不支持。</p>;
  }
}
