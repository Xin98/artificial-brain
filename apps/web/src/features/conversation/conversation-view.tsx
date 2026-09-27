"use client";

import { useEffect, useState } from "react";

import { ChatPanel } from "./chat-panel";
import {
  createSession,
  deleteSession,
  listSessions,
  renameSession,
} from "./fetch-sessions";
import { SessionSidebar } from "./session-sidebar";
import type { SidebarSession } from "./session-sidebar";

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

  async function refresh(): Promise<void> {
    const result = await listSessions("", fetcher);
    if (result.ok) {
      setSessions(result.sessions);
    }
  }

  useEffect(() => {
    let cancelled = false;
    void listSessions("", fetcher).then((result) => {
      if (cancelled || !result.ok) {
        return;
      }
      setSessions(result.sessions);
    });
    return () => {
      cancelled = true;
    };
  }, [fetcher]);

  function select(sessionId: string | undefined): void {
    setActiveId(sessionId);
    setPanelKey(sessionId);
  }

  async function handleCreate(): Promise<void> {
    if (busy) {
      return;
    }
    setBusy(true);
    const result = await createSession("", fetcher);
    setBusy(false);
    if (!result.ok) {
      return;
    }
    setSessions((current) => [result.session, ...current]);
    select(result.session.id);
  }

  async function handleRename(sessionId: string, title: string): Promise<void> {
    if (busy) {
      return;
    }
    setBusy(true);
    const result = await renameSession("", fetcher, sessionId, title);
    setBusy(false);
    if (!result.ok) {
      return;
    }
    setSessions((current) =>
      current.map((session) =>
        session.id === sessionId
          ? { ...session, title: result.session.title }
          : session,
      ),
    );
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
    const result = await deleteSession("", fetcher, sessionId);
    setBusy(false);
    if (!result.ok) {
      return;
    }
    const remaining = sessions.filter((session) => session.id !== sessionId);
    setSessions(remaining);
    if (activeId === sessionId || panelKey === sessionId) {
      select(remaining[0]?.id);
    }
  }

  function handleSessionCreated(sessionId: string): void {
    setActiveId(sessionId);
    void refresh();
  }

  return (
    <div className="conversation-layout">
      <SessionSidebar
        activeId={activeId}
        busy={busy}
        onCreate={() => void handleCreate()}
        onDelete={(sessionId) => void handleDelete(sessionId)}
        onRename={(sessionId, title) => void handleRename(sessionId, title)}
        onSelect={select}
        sessions={sessions}
      />
      <ChatPanel
        fetcher={fetcher}
        key={panelKey ?? "new"}
        onSessionCreated={handleSessionCreated}
        sessionId={panelKey}
      />
    </div>
  );
}
