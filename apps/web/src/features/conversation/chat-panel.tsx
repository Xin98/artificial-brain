"use client";

import { useEffect, useRef, useState } from "react";

import { confirmAction, createConfirmation } from "../todos/fetch-todos";
import { postConversationMessage } from "./fetch-conversation";
import type {
  ConversationFailureReason,
  ConversationResponse,
} from "./fetch-conversation";

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
  turnID: number;
  expiresAt?: string;
}

interface ConversationTurn {
  id: number;
  text: string;
  response: ConversationResponse;
}

interface ChatError {
  message: string;
  retryText?: string;
  correlationId?: string;
}

export function ChatPanel({
  fetcher = fetch,
  timezoneProvider,
}: {
  fetcher?: typeof fetch;
  timezoneProvider?: () => string;
}): React.JSX.Element {
  const [text, setText] = useState("");
  const [turns, setTurns] = useState<ConversationTurn[]>([]);
  const [pending, setPending] = useState<PendingConfirmation | null>(null);
  const [error, setError] = useState<ChatError | null>(null);
  const [busy, setBusy] = useState(false);
  const nextTurnID = useRef(1);
  const confirmButtonRef = useRef<HTMLButtonElement>(null);

  useEffect(() => {
    if (pending) {
      confirmButtonRef.current?.focus();
    }
  }, [pending]);

  async function send(event: React.FormEvent): Promise<void> {
    event.preventDefault();
    await submitMessage(text.trim());
  }

  async function submitMessage(message: string): Promise<void> {
    if (busy || message.length === 0) {
      return;
    }
    setBusy(true);
    setError(null);
    const result = await postConversationMessage(
      "",
      fetcher,
      message,
      browserTimezone(timezoneProvider),
    );
    setBusy(false);
    if (!result.ok) {
      setError({
        message: failureCopy(result.reason),
        retryText: isSafeToRetry(result.reason) ? message : undefined,
        correlationId: result.correlationId,
      });
      return;
    }
    const turnID = nextTurnID.current++;
    setText((current) => (current.trim() === message ? "" : current));
    setTurns((current) => [
      ...current,
      { id: turnID, text: message, response: result.response },
    ]);
    if (
      result.response.kind === "confirmation_required" &&
      result.response.confirmationId
    ) {
      setPending({
        confirmationId: result.response.confirmationId,
        turnID,
        expiresAt: result.response.expiresAt,
      });
    }
  }

  async function pickCandidate(turnID: number, todoId: string): Promise<void> {
    if (busy) {
      return;
    }
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
        turnID,
        expiresAt: outcome.expiresAt,
      });
      return;
    }
    setError({ message: "确认请求失败，请稍后再试。" });
  }

  async function confirmPending(): Promise<void> {
    if (!pending || busy) {
      return;
    }
    const confirmation = pending;
    setBusy(true);
    setError(null);
    const outcome = await confirmAction(
      "",
      fetcher,
      confirmation.confirmationId,
    );
    setBusy(false);
    if (outcome.ok) {
      setPending(null);
      setTurns((current) =>
        current.map((turn) =>
          turn.id === confirmation.turnID
            ? {
                ...turn,
                response: {
                  kind: "todo_deleted",
                  correlationId: turn.response.correlationId,
                  todoId: outcome.todoId,
                },
              }
            : turn,
        ),
      );
      return;
    }
    setError({ message: "确认失败，可能已过期或已被使用。" });
  }

  return (
    <section aria-label="对话" className="chat-panel">
      <form aria-busy={busy} className="chat-input" onSubmit={send}>
        <label className="sr-only" htmlFor="chat-text">
          消息
        </label>
        <input
          id="chat-text"
          maxLength={1000}
          onChange={(event) => setText(event.target.value)}
          placeholder="例如:明天下午三点提醒我提交周报"
          type="text"
          value={text}
        />
        <button
          className="btn-primary"
          disabled={busy || text.trim().length === 0}
          type="submit"
        >
          {busy ? "发送中…" : "发送"}
        </button>
      </form>
      {error ? (
        <div aria-live="polite" className="chat-error" role="alert">
          <p>{error.message}</p>
          {error.correlationId ? (
            <p className="chat-error-reference">
              参考编号：<code>{error.correlationId}</code>
            </p>
          ) : null}
          {error.retryText ? (
            <button
              className="btn-ghost"
              disabled={busy}
              onClick={() => void submitMessage(error.retryText ?? "")}
              type="button"
            >
              重试上一条
            </button>
          ) : null}
        </div>
      ) : null}
      {turns.length > 0 ? (
        <ol
          aria-label="对话记录"
          aria-live="polite"
          aria-relevant="additions text"
          className="chat-history"
          role="log"
        >
          {turns.map((turn) => (
            <li className="chat-turn" key={turn.id}>
              <p className="chat-message-user">
                <span className="sr-only">你：</span>
                {turn.text}
              </p>
              <div className="chat-message-assistant">
                <span className="sr-only">助手：</span>
                {renderResponse(
                  turn.response,
                  (todoId) => void pickCandidate(turn.id, todoId),
                  busy,
                )}
              </div>
            </li>
          ))}
        </ol>
      ) : null}
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
            className="btn-danger"
            disabled={busy}
            onClick={() => void confirmPending()}
            ref={confirmButtonRef}
            type="button"
          >
            确认删除
          </button>
          <button
            className="btn-ghost"
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

function failureCopy(reason: ConversationFailureReason): string {
  switch (reason) {
    case "timeout":
      return "请求可能仍已完成。原消息已保留，请先检查待办列表，避免重复创建。";
    case "unauthenticated":
      return "登录状态已失效，请重新登录后再试。";
    case "rate_limited":
      return "当前对话请求较多，请稍后重试。";
    case "rejected":
      return "这条消息未被服务接受，请检查后重试。";
    case "server":
      return "对话服务处理失败，请重试。";
    case "invalid_response":
      return "对话服务返回异常，但请求可能已完成。请先检查待办列表，避免重复创建。";
    case "network":
      return "网络连接中断，请先检查待办列表，避免重复创建。原消息已保留。";
  }
}

function isSafeToRetry(reason: ConversationFailureReason): boolean {
  return reason === "rate_limited" || reason === "server";
}

function renderResponse(
  response: ConversationResponse,
  onPickCandidate: (todoId: string) => void,
  disabled: boolean,
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
                  className="chat-candidate"
                  disabled={disabled}
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
