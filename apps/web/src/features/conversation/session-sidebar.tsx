"use client";

import { useState } from "react";

export interface SidebarSession {
  id: string;
  title: string;
  updatedAt: string;
}

const MAX_TITLE_LENGTH = 50;

function formatSessionTime(iso: string): string {
  const date = new Date(iso);
  return Number.isNaN(date.getTime()) ? "" : date.toLocaleString();
}

// SessionSidebar is presentational: every mutation goes through the props,
// and only the inline rename editor keeps local state.
export function SessionSidebar({
  sessions,
  activeId,
  busy = false,
  onCreate,
  onSelect,
  onRename,
  onDelete,
}: {
  sessions: SidebarSession[];
  activeId?: string;
  busy?: boolean;
  onCreate: () => void;
  onSelect: (sessionId: string) => void;
  onRename: (sessionId: string, title: string) => void;
  onDelete: (sessionId: string) => void;
}): React.JSX.Element {
  const [editingId, setEditingId] = useState<string | undefined>();
  const [draft, setDraft] = useState("");

  function startRename(session: SidebarSession): void {
    setEditingId(session.id);
    setDraft(session.title);
  }

  function cancelRename(): void {
    setEditingId(undefined);
    setDraft("");
  }

  function submitRename(sessionId: string): void {
    const title = draft.trim();
    if (title.length === 0 || [...title].length > MAX_TITLE_LENGTH) {
      return;
    }
    cancelRename();
    onRename(sessionId, title);
  }

  return (
    <aside aria-label="会话列表" className="session-sidebar">
      <button
        className="btn-primary session-new"
        disabled={busy}
        onClick={onCreate}
        type="button"
      >
        新建会话
      </button>
      <ul className="session-list">
        {sessions.map((session) => (
          <li
            aria-current={session.id === activeId ? "true" : undefined}
            className={
              session.id === activeId
                ? "session-item session-item-active"
                : "session-item"
            }
            key={session.id}
          >
            {editingId === session.id ? (
              <form
                className="session-rename"
                onSubmit={(event) => {
                  event.preventDefault();
                  submitRename(session.id);
                }}
              >
                <label
                  className="sr-only"
                  htmlFor={`session-title-${session.id}`}
                >
                  会话标题
                </label>
                <input
                  autoFocus
                  id={`session-title-${session.id}`}
                  maxLength={MAX_TITLE_LENGTH}
                  onChange={(event) => setDraft(event.target.value)}
                  type="text"
                  value={draft}
                />
                <button className="btn-ghost" type="submit">
                  保存
                </button>
                <button
                  className="btn-ghost"
                  onClick={cancelRename}
                  type="button"
                >
                  取消
                </button>
              </form>
            ) : (
              <div className="session-row">
                <button
                  className="session-open"
                  disabled={busy}
                  onClick={() => onSelect(session.id)}
                  type="button"
                >
                  <span className="session-title">{session.title}</span>
                  <span className="session-meta">
                    {formatSessionTime(session.updatedAt)}
                  </span>
                </button>
                <span className="session-actions">
                  <button
                    className="btn-ghost"
                    disabled={busy}
                    onClick={() => startRename(session)}
                    type="button"
                  >
                    重命名
                  </button>
                  <button
                    className="btn-ghost"
                    disabled={busy}
                    onClick={() => onDelete(session.id)}
                    type="button"
                  >
                    删除
                  </button>
                </span>
              </div>
            )}
          </li>
        ))}
      </ul>
    </aside>
  );
}
