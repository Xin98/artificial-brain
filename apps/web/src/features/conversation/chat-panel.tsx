"use client";

import { useEffect, useRef, useState, useSyncExternalStore } from "react";

import { confirmAction, createConfirmation } from "../todos/fetch-todos";
import { postConversationMessage } from "./fetch-conversation";
import type {
  ConversationFailureReason,
  ConversationResponse,
} from "./fetch-conversation";
import { fetchSessionMessages } from "./fetch-sessions";
import type { SessionMessage } from "./fetch-sessions";
import { readConversationState, writeConversationState } from "./browser-state";
import { MessageContent } from "./message-content";

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
  title?: string;
  dueAtUtc?: string;
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
  onActivity,
  onBusyChange,
}: {
  fetcher?: typeof fetch;
  timezoneProvider?: () => string;
  sessionId?: string;
  onSessionCreated?: (sessionId: string) => void;
  onActivity?: (sessionId?: string) => void;
  onBusyChange?: (busy: boolean) => void;
}): React.JSX.Element {
  const savedDraft = useSyncExternalStore(
    subscribeStorage,
    () => readConversationState(`draft.${sessionId ?? "new"}`),
    () => "",
  );
  const [draft, setText] = useState<string>();
  const text = draft ?? savedDraft;
  const [turns, setTurns] = useState<ConversationTurn[]>([]);
  const [pending, setPending] = useState<PendingConfirmation | null>(null);
  const [error, setError] = useState<ChatError | null>(null);
  const [busy, setBusy] = useState(false);
  const [loadingHistory, setLoadingHistory] = useState(Boolean(sessionId));
  const [historyError, setHistoryError] = useState(false);
  const [historyVersion, setHistoryVersion] = useState(0);
  const [historyMessages, setHistoryMessages] = useState<SessionMessage[]>([]);
  const [nextBefore, setNextBefore] = useState<string>();
  const [loadingOlder, setLoadingOlder] = useState(false);
  const [sendingText, setSendingText] = useState("");
  const mounted = useRef(true);
  const sending = useRef(false);
  const historyListRef = useRef<HTMLOListElement>(null);
  const liveTurnCount = turns.filter((turn) => turn.id > 0).length;
  const nextTurnID = useRef(1);
  const confirmButtonRef = useRef<HTMLButtonElement>(null);
  // The panel owns the live session id so an auto-created session does not
  // re-key (and therefore remount) the panel mid-conversation.
  const sessionIdRef = useRef<string | undefined>(sessionId);
  const onSessionCreatedRef = useRef(onSessionCreated);
  onSessionCreatedRef.current = onSessionCreated;

  useEffect(() => {
    mounted.current = true;
    return () => {
      mounted.current = false;
    };
  }, [sessionId]);
  useEffect(() => {
    onBusyChange?.(busy);
  }, [busy, onBusyChange]);
  useEffect(() => {
    const list = historyListRef.current;
    if (list) list.scrollTop = list.scrollHeight;
  }, [liveTurnCount, sendingText, loadingHistory]);

  function changeDraft(value: string): void {
    setText(value);
    writeConversationState(`draft.${sessionIdRef.current ?? "new"}`, value);
  }

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
        setHistoryError(true);
        return;
      }
      setHistoryError(false);
      setHistoryMessages(result.history.messages);
      setNextBefore(
        result.history.hasMore ? result.history.nextBefore : undefined,
      );
      const replay = pairHistory(result.history.messages);
      nextTurnID.current = Math.max(nextTurnID.current, replay.nextID);
      setTurns((current) => [...replay.turns, ...current]);
    });
    return () => {
      cancelled = true;
    };
  }, [sessionId, fetcher, historyVersion]);

  async function send(event: React.FormEvent): Promise<void> {
    event.preventDefault();
    await submitMessage(text.trim());
  }

  async function submitMessage(message: string): Promise<void> {
    if (
      sending.current ||
      busy ||
      loadingHistory ||
      historyError ||
      message.length === 0
    ) {
      return;
    }
    setBusy(true);
    sending.current = true;
    setSendingText(message);
    setError(null);
    const result = await postConversationMessage(
      "",
      fetcher,
      message,
      browserTimezone(timezoneProvider),
      sessionIdRef.current,
    );
    if (!mounted.current) return;
    setBusy(false);
    sending.current = false;
    setSendingText("");
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
      writeConversationState(
        `draft.${result.response.sessionId}`,
        readConversationState("draft.new"),
      );
      writeConversationState("draft.new", "");
      onSessionCreatedRef.current?.(result.response.sessionId);
    } else {
      onActivity?.(sessionIdRef.current);
    }
    const turnID = nextTurnID.current++;
    setText((current) => {
      if ((current ?? text).trim() !== message) return current;
      writeConversationState(`draft.${sessionIdRef.current ?? "new"}`, "");
      return "";
    });
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
        title:
          result.response.todo?.title ?? result.response.candidates?.[0]?.title,
        dueAtUtc:
          result.response.todo?.dueAtUtc ??
          result.response.candidates?.[0]?.dueAtUtc,
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
    if (!mounted.current) return;
    setBusy(false);
    if (outcome.ok && outcome.confirmationId) {
      setPending({
        confirmationId: outcome.confirmationId,
        turnID,
        expiresAt: outcome.expiresAt,
        title:
          turns.find((turn) => turn.id === turnID)?.assistant?.source ===
          "response"
            ? (
                turns.find((turn) => turn.id === turnID)!.assistant as {
                  source: "response";
                  response: ConversationResponse;
                }
              ).response.candidates?.find(
                (candidate) => candidate.todoId === todoId,
              )?.title
            : undefined,
        dueAtUtc:
          turns.find((turn) => turn.id === turnID)?.assistant?.source ===
          "response"
            ? (
                turns.find((turn) => turn.id === turnID)!.assistant as {
                  source: "response";
                  response: ConversationResponse;
                }
              ).response.candidates?.find(
                (candidate) => candidate.todoId === todoId,
              )?.dueAtUtc
            : undefined,
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
      5000,
      sessionIdRef.current,
    );
    if (!mounted.current) return;
    setBusy(false);
    if (outcome.ok) {
      setPending(null);
      onActivity?.(sessionIdRef.current);
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
    setPending(null);
    setError({
      message:
        "确认失败，可能已过期或待办已更新。请重新查询并核对待办后再操作。",
    });
  }

  async function loadOlder(): Promise<void> {
    if (!sessionId || !nextBefore || loadingOlder) return;
    setLoadingOlder(true);
    const result = await fetchSessionMessages(
      "",
      fetcher,
      sessionId,
      10000,
      nextBefore,
    );
    if (!mounted.current) return;
    setLoadingOlder(false);
    if (!result.ok) {
      setError({ message: "更早的消息加载失败，请再次点击加载。" });
      return;
    }
    const known = new Set(historyMessages.map((message) => message.id));
    const messages = [
      ...result.history.messages.filter((message) => !known.has(message.id)),
      ...historyMessages,
    ];
    const replay = pairHistory(messages);
    setHistoryMessages(messages);
    setNextBefore(
      result.history.hasMore ? result.history.nextBefore : undefined,
    );
    setTurns((current) => [
      ...replay.turns,
      ...current.filter((turn) => turn.id > 0),
    ]);
  }

  return (
    <section aria-label="对话" className="chat-panel">
      <form aria-busy={busy} className="chat-input" onSubmit={send}>
        <label className="sr-only" htmlFor="chat-text">
          消息
        </label>
        <textarea
          id="chat-text"
          maxLength={1000}
          onChange={(event) => changeDraft(event.target.value)}
          onKeyDown={(event) => {
            if (
              event.key === "Enter" &&
              !event.shiftKey &&
              !event.nativeEvent.isComposing
            ) {
              event.preventDefault();
              void submitMessage(text.trim());
            }
          }}
          placeholder="例如:明天下午三点提醒我提交周报"
          rows={3}
          value={text}
        />
        <button
          className="btn-primary"
          disabled={
            busy || loadingHistory || historyError || text.trim().length === 0
          }
          type="submit"
        >
          {busy ? "发送中…" : "发送"}
        </button>
        <p className="chat-input-hint">
          Enter 发送 · Shift + Enter 换行 · {text.length}/1000
        </p>
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
      {historyError ? (
        <div className="chat-error" role="alert">
          <p>无法加载会话历史，请重试。</p>
          <button
            className="btn-ghost"
            type="button"
            onClick={() => {
              setLoadingHistory(true);
              setHistoryError(false);
              setHistoryVersion((version) => version + 1);
            }}
          >
            重试加载历史
          </button>
        </div>
      ) : null}
      {nextBefore ? (
        <button
          className="btn-ghost"
          disabled={loadingOlder}
          onClick={() => void loadOlder()}
          type="button"
        >
          {loadingOlder ? "加载中…" : "加载更早的消息"}
        </button>
      ) : null}
      {loadingHistory ? (
        <p aria-live="polite" className="chat-history-loading">
          正在加载会话历史…
        </p>
      ) : null}
      {turns.length > 0 || sendingText ? (
        <ol
          aria-label="对话记录"
          aria-live="polite"
          aria-relevant="additions text"
          className="chat-history"
          ref={historyListRef}
          role="log"
        >
          {turns.map((turn) => (
            <li className="chat-turn" key={turn.id}>
              {turn.userText ? (
                <p className="chat-message-user">
                  <span className="sr-only">你:</span>
                  {turn.userText}
                </p>
              ) : null}
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
          {sendingText ? (
            <li className="chat-turn">
              <p className="chat-message-user">{sendingText}</p>
              <p className="chat-pending" role="status">
                正在回复…
              </p>
            </li>
          ) : null}
        </ol>
      ) : null}
      {!loadingHistory &&
      !historyError &&
      turns.length === 0 &&
      !sendingText ? (
        <div className="chat-empty">
          <h2>今天想先处理什么？</h2>
          <p>可以直接聊天，也可以创建、查询和删除待办。</p>
          <div className="chat-suggestions">
            {[
              "明天下午三点提醒我提交周报",
              "查看我的待办",
              "帮我整理今天的思路",
            ].map((example) => (
              <button
                className="btn-ghost"
                key={example}
                type="button"
                onClick={() => changeDraft(example)}
              >
                {example}
              </button>
            ))}
          </div>
        </div>
      ) : null}
      {pending ? (
        <div className="chat-confirm">
          <p>
            {pending.title
              ? `确认删除「${pending.title}」吗？`
              : "删除目标未提供，请取消后到待办页核对。"}
            {pending.dueAtUtc ? (
              <span>
                {" "}
                到期时间：{new Date(pending.dueAtUtc).toLocaleString()}。
              </span>
            ) : null}
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
            disabled={busy || !pending.title}
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

function subscribeStorage(): () => void {
  return () => {};
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
      turns.push({ id: -id++, userText: message.body });
      continue;
    }
    const open = turns[turns.length - 1];
    if (open && open.assistant === undefined) {
      open.assistant = { source: "text", text: message.body };
    } else {
      turns.push({
        id: -id++,
        userText: "",
        assistant: { source: "text", text: message.body },
      });
    }
  }
  return { turns, nextID: 1 };
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
    return (
      <div className="chat-result">
        <MessageContent text={turn.assistant.text} />
      </div>
    );
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
      return (
        <div className="chat-result">
          <MessageContent text={response.reply ?? ""} />
        </div>
      );
    case "todo_created":
      return (
        <div className="chat-result">
          <p>
            已创建待办「{response.todo?.title ?? ""}」
            {response.resolvedDueAtUtc && response.localEcho ? (
              <span>
                ，
                {response.todo?.reminderScheduled === false
                  ? "到期时间"
                  : "提醒时间"}{" "}
                {response.localEcho}
                {response.timezoneEcho ? `(${response.timezoneEcho})` : ""}
              </span>
            ) : null}
          </p>
          {response.todo?.reminderScheduled === false &&
          response.resolvedDueAtUtc ? (
            <p role="status">
              尚未配置可用提醒渠道，本条待办无法发送提醒。
              <a href="/settings">配置提醒渠道</a>
              ，再编辑本条待办的提醒时间以安排投递。
            </p>
          ) : response.todo?.reminderChannels?.length ? (
            <p>
              提醒渠道：
              {response.todo.reminderChannels
                .map((channel) => (channel === "email" ? "邮箱" : "短信"))
                .join("、")}
            </p>
          ) : null}
          <a href="/todos">查看待办</a>
        </div>
      );
    case "clarification":
      if (response.reply) {
        return <p className="chat-result">{response.reply}</p>;
      }
      return (
        <p className="chat-result">
          需要补充信息
          {response.missingFields && response.missingFields.length > 0
            ? `:${response.missingFields.map((field) => (({ title: "待办标题", due_at: "提醒时间", keyword: "查找关键词" }) as Record<string, string>)[field] ?? "任务信息").join("、")}`
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
                  {candidate.dueAtUtc
                    ? ` · ${new Date(candidate.dueAtUtc).toLocaleString()}`
                    : ""}
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
              <li key={todo.id}>
                {todo.title}
                {todo.dueAtUtc
                  ? ` · ${new Date(todo.dueAtUtc).toLocaleString()}`
                  : ""}
                {todo.status
                  ? ` · ${todo.status === "completed" ? "已完成" : "待处理"}`
                  : ""}
                {todo.description ? <p>{todo.description}</p> : null}
              </li>
            ))}
          </ul>
          {!response.todos?.length ? <p>没有符合条件的待办。</p> : null}
          <a href="/todos">打开待办页面</a>
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
