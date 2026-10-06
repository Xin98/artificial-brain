"use client";

import { useCallback, useEffect, useRef, useState } from "react";

import { ChatPanel } from "./chat-panel";
import {
  createSession,
  deleteSession,
  listSessions,
  renameSession,
} from "./fetch-sessions";
import { SessionSidebar } from "./session-sidebar";
import type { SidebarSession } from "./session-sidebar";
import { readConversationState, writeConversationState } from "./browser-state";

// ConversationView owns the session list and which session the chat panel is
// bound to. The panel is keyed by panelKey, which only changes on explicit
// sidebar actions: when the backend auto-creates a session for the first
// message, the sidebar highlight follows (activeId) but the panel keeps its
// live turns instead of remounting.
export function ConversationView({
  fetcher = fetch,
}: {
  fetcher?: typeof fetch;
}): React.JSX.Element {
  const [sessions, setSessions] = useState<SidebarSession[]>([]);
  const [activeId, setActiveId] = useState<string | undefined>();
  const [panelKey, setPanelKey] = useState<string | undefined>();
  const [busy, setBusy] = useState(false);
  const [chatBusy, setChatBusy] = useState(false);
  const [loading, setLoading] = useState(true);
  const [restoring, setRestoring] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [hasMore, setHasMore] = useState(false);
  const [nextOffset, setNextOffset] = useState(0);
  const [expanded, setExpanded] = useState(false);
  const activeRef = useRef<string | undefined>(undefined);
  const mounted = useRef(true);

  async function refresh(offset = 0): Promise<void> {
    const result = await listSessions("", fetcher, 10000, offset);
    if (!mounted.current) return;
    if (result.ok) {
      setError(null);
      setSessions((current) =>
        offset
          ? [
              ...current,
              ...result.sessions.filter(
                (session) => !current.some((item) => item.id === session.id),
              ),
            ]
          : result.sessions,
      );
      setHasMore(result.hasMore ?? false);
      setNextOffset(result.nextOffset ?? 0);
    } else {
      setError("无法加载会话列表，请重试。");
    }
    setLoading(false);
  }

  useEffect(() => {
    let cancelled = false;
    mounted.current = true;
    void listSessions("", fetcher).then((result) => {
      if (cancelled) return;
      setLoading(false);
      setRestoring(false);
      if (!result.ok) {
        setError("无法加载会话列表，请重试。");
        return;
      }
      setSessions(result.sessions);
      setHasMore(result.hasMore ?? false);
      setNextOffset(result.nextOffset ?? 0);
      const saved = readConversationState("active");
      if (saved && !activeRef.current) {
        activeRef.current = saved;
        setActiveId(saved);
        setPanelKey(saved);
      }
    });
    return () => {
      cancelled = true;
      mounted.current = false;
    };
  }, [fetcher]);

  function select(sessionId: string | undefined): void {
    activeRef.current = sessionId;
    writeConversationState("active", sessionId ?? "");
    setActiveId(sessionId);
    setPanelKey(sessionId);
    setExpanded(false);
  }

  async function handleCreate(): Promise<void> {
    if (busy) {
      return;
    }
    setBusy(true);
    setError(null);
    const result = await createSession("", fetcher);
    setBusy(false);
    if (!result.ok) {
      setError("新建会话失败，请重试。");
      return;
    }
    setSessions((current) => [result.session, ...current]);
    select(result.session.id);
  }

  async function handleRename(
    sessionId: string,
    title: string,
  ): Promise<boolean> {
    if (busy) {
      return false;
    }
    setBusy(true);
    setError(null);
    const result = await renameSession("", fetcher, sessionId, title);
    setBusy(false);
    if (!result.ok) {
      setError("重命名失败，标题草稿已保留，请重试。");
      return false;
    }
    setSessions((current) =>
      current.map((session) =>
        session.id === sessionId
          ? { ...session, title: result.session.title }
          : session,
      ),
    );
    return true;
  }

  async function handleDelete(sessionId: string): Promise<void> {
    if (busy) {
      return;
    }
    const target = sessions.find((session) => session.id === sessionId);
    const confirmed = window.confirm(
      `删除会话「${target?.title ?? ""}」及其全部消息?此操作不可恢复。`,
    );
    if (!confirmed) {
      return;
    }
    setBusy(true);
    setError(null);
    const result = await deleteSession("", fetcher, sessionId);
    setBusy(false);
    if (!result.ok) {
      setError("删除会话失败，请重试。");
      return;
    }
    const remaining = sessions.filter((session) => session.id !== sessionId);
    setSessions(remaining);
    if (activeId === sessionId || panelKey === sessionId) {
      select(remaining[0]?.id);
    }
    writeConversationState(`draft.${sessionId}`, "");
  }

  function handleSessionCreated(sessionId: string): void {
    activeRef.current = sessionId;
    writeConversationState("active", sessionId);
    setActiveId(sessionId);
    void refresh();
  }
  const handleBusyChange = useCallback(
    (value: boolean) => setChatBusy(value),
    [],
  );
  function handleActivity(): void {
    void refresh();
  }

  return (
    <div className="conversation-layout">
      <div
        className={
          expanded
            ? "session-container session-container-expanded"
            : "session-container"
        }
      >
        <button
          className="btn-ghost session-toggle"
          aria-expanded={expanded}
          aria-controls="conversation-sessions"
          onClick={() => setExpanded(!expanded)}
          type="button"
        >
          {expanded ? "收起会话列表" : "展开会话列表"}
        </button>
        <div id="conversation-sessions">
          <SessionSidebar
            activeId={activeId}
            busy={busy || chatBusy || restoring}
            onCreate={() => void handleCreate()}
            onDelete={(sessionId) => void handleDelete(sessionId)}
            onRename={handleRename}
            onSelect={select}
            sessions={sessions}
          />
          {loading ? (
            <p className="chat-history-loading" role="status">
              正在加载会话列表…
            </p>
          ) : null}
          {!loading && sessions.length === 0 && !error ? (
            <p className="session-empty">
              还没有会话，发送第一条消息即可自动保存。
            </p>
          ) : null}
          {error ? (
            <div className="chat-error" role="alert">
              <p>{error}</p>
              <button
                className="btn-ghost"
                disabled={loading}
                onClick={() => {
                  setLoading(true);
                  void refresh();
                }}
                type="button"
              >
                重试加载会话
              </button>
            </div>
          ) : null}
          {hasMore ? (
            <button
              className="btn-ghost"
              disabled={loading || busy || chatBusy}
              onClick={() => {
                setLoading(true);
                void refresh(nextOffset);
              }}
              type="button"
            >
              加载更多会话
            </button>
          ) : null}
        </div>
      </div>
      {restoring ? (
        <p className="chat-history-loading" role="status">
          正在恢复会话…
        </p>
      ) : (
        <ChatPanel
          fetcher={fetcher}
          key={panelKey ?? "new"}
          onSessionCreated={handleSessionCreated}
          onActivity={handleActivity}
          onBusyChange={handleBusyChange}
          sessionId={panelKey}
        />
      )}
    </div>
  );
}
