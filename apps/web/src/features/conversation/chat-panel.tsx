"use client";

import { useEffect, useRef, useState } from "react";

import { confirmAction, createConfirmation } from "../todos/fetch-todos";
import { postConversationMessage } from "./fetch-conversation";
import type {
  ConversationFailureReason,
  ConversationResponse,
} from "./fetch-conversation";
import { fetchSessionMessages } from "./fetch-sessions";
import type { SessionMessage } from "./fetch-sessions";

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

// A live turn keeps the structured response (so candidates and confirmation
// stay interactive); a replayed history turn is text-only by design.
type AssistantTurn =
  | { source: "response"; response: ConversationResponse }
  | { source: "text"; text: string };

interface ConversationTurn {
  id: number;
  userText: string;
  assistant?: AssistantTurn;
}

interface ChatError {
  message: string;
  retryText?: string;
  correlationId?: string;
}

export function ChatPanel({
  fetcher = fetch,
  timezoneProvider,
  sessionId,
  onSessionCreated,
}: {
  fetcher?: typeof fetch;
  timezoneProvider?: () => string;
  sessionId?: string;
  onSessionCreated?: (sessionId: string) => void;
}): React.JSX.Element {
  const [text, setText] = useState("");
  const [turns, setTurns] = useState<ConversationTurn[]>([]);
  const [pending, setPending] = useState<PendingConfirmation | null>(null);
  const [error, setError] = useState<ChatError | null>(null);
  const [busy, setBusy] = useState(false);
  const [loadingHistory, setLoadingHistory] = useState(Boolean(sessionId));
  const nextTurnID = useRef(1);
  const confirmButtonRef = useRef<HTMLButtonElement>(null);
  // The panel owns the live session id so an auto-created session does not
  // re-key (and therefore remount) the panel mid-conversation.
  const sessionIdRef = useRef<string | undefined>(sessionId);
  const onSessionCreatedRef = useRef(onSessionCreated);
  onSessionCreatedRef.current = onSessionCreated;

  useEffect(() => {
    if (pending) {
      confirmButtonRef.current?.focus();
    }
  }, [pending]);

  // The panel is remounted (keyed) whenever the bound session changes, so
  // this effect only ever runs for the initial sessionId; loadingHistory is
  // seeded from the prop instead of being flipped inside the effect.
  useEffect(() => {
    if (!sessionId) {
      return;
    }
    let cancelled = false;
    void fetchSessionMessages("", fetcher, sessionId).then((result) => {
      if (cancelled) {
        return;
      }
      setLoadingHistory(false);
      if (!result.ok) {
        setError({ message: "无法加载会话历史,请重新选择会话。" });
        return;
      }
      const replay = pairHistory(result.history.messages);
      nextTurnID.current = Math.max(nextTurnID.current, replay.nextID);
      setTurns((current) => [...replay.turns, ...current]);
    });
    return () => {
      cancelled = true;
    };
  }, [sessionId, fetcher]);

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
      sessionIdRef.current,
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
    if (result.response.sessionId && !sessionIdRef.current) {
      sessionIdRef.current = result.response.sessionId;
      onSessionCreatedRef.current?.(result.response.sessionId);
    }
    const turnID = nextTurnID.current++;
    setText((current) => (current.trim() === message ? "" : current));
    setTurns((current) => [
      ...current,
      {
        id: turnID,
        userText: message,
        assistant: { source: "response", response: result.response },
      },
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
    setError({ message: "确认请求失败,请稍后再试。" });
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
                assistant: {
                  source: "response",
                  response: {
                    kind: "todo_deleted",
                    correlationId:
                      turn.assistant?.source === "response"
                        ? turn.assistant.response.correlationId
                        : "",
                    todoId: outcome.todoId,
                  },
                },
              }
            : turn,
        ),
      );
      return;
    }
    setError({ message: "确认失败,可能已过期或已被使用。" });
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
              参考编号:<code>{error.correlationId}</code>
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
      {loadingHistory ? (
        <p aria-live="polite" className="chat-history-loading">
          正在加载会话历史…
        </p>
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
                <span className="sr-only">你:</span>
                {turn.userText}
              </p>
              <div className="chat-message-assistant">
                <span className="sr-only">助手:</span>
                {renderAssistant(
                  turn,
                  busy,
                  (todoId) => void pickCandidate(turn.id, todoId),
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

// pairHistory replays persisted transcript rows as text-only turns: a user
// row opens a turn and the next assistant row completes it. Interactive
// components (candidate buttons, confirmation countdowns) are deliberately
// not recreated from history.
function pairHistory(messages: SessionMessage[]): {
  turns: ConversationTurn[];
  nextID: number;
} {
  const turns: ConversationTurn[] = [];
  let id = 1;
  for (const message of messages) {
    if (message.role === "user") {
      turns.push({ id: id++, userText: message.body });
      continue;
    }
    const open = turns[turns.length - 1];
    if (open && open.assistant === undefined) {
      open.assistant = { source: "text", text: message.body };
    } else {
      turns.push({
        id: id++,
        userText: "",
        assistant: { source: "text", text: message.body },
      });
    }
  }
  return { turns, nextID: id };
}

function renderAssistant(
  turn: ConversationTurn,
  busy: boolean,
  onPickCandidate: (todoId: string) => void,
): React.JSX.Element | null {
  if (turn.assistant === undefined) {
    return null;
  }
  if (turn.assistant.source === "text") {
    return <p className="chat-result">{turn.assistant.text}</p>;
  }
  return renderResponse(turn.assistant.response, onPickCandidate, busy);
}

function failureCopy(reason: ConversationFailureReason): string {
  switch (reason) {
    case "timeout":
      return "请求可能仍已完成。原消息已保留,请先检查待办列表,避免重复创建。";
    case "unauthenticated":
      return "登录状态已失效,请重新登录后再试。";
    case "rate_limited":
      return "当前对话请求较多,请稍后重试。";
    case "rejected":
      return "这条消息未被服务接受,请检查后重试。";
    case "server":
      return "对话服务处理失败,请重试。";
    case "invalid_response":
      return "对话服务返回异常,但请求可能已完成。请先检查待办列表,避免重复创建。";
    case "network":
      return "网络连接中断,请先检查待办列表,避免重复创建。原消息已保留。";
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
    case "chat":
      return <p className="chat-result">{response.reply ?? ""}</p>;
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
      if (response.reply) {
        return <p className="chat-result">{response.reply}</p>;
      }
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
