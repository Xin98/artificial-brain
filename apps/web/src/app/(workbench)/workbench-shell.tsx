"use client";

import Link from "next/link";
import { usePathname } from "next/navigation";
import { useEffect, useRef, useState } from "react";
import { logout } from "../../features/auth/fetch-auth";
import {
  clearConversationDrafts,
  rememberConversationOwner,
} from "../../features/auth/session-recovery";
import type { SessionContext } from "../../shared/server/session";

const workbenchLinks = [
  { href: "/", label: "概况" },
  { href: "/todos", label: "待办" },
  { href: "/conversation", label: "对话" },
  { href: "/settings", label: "设置" },
  { href: "/data", label: "数据" },
];

// WorkbenchShell is the presentational frame for session-gated pages: a
// sticky single-line nav with the brand mark and the five workbench areas
// (the active route stays highlighted) plus the page content. It renders no
// internal URLs or configuration.
export function WorkbenchShell({
  children,
  session,
  fetcher = fetch,
  onNavigate = (path: string) => window.location.assign(path),
}: {
  children: React.ReactNode;
  session?: SessionContext;
  fetcher?: typeof fetch;
  onNavigate?: (path: string) => void;
}): React.JSX.Element {
  const pathname = usePathname();
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const inFlight = useRef(false);
  const [restoredOwner, setRestoredOwner] = useState<string | null>(null);
  useEffect(() => {
    let cancelled = false;
    if (session)
      void Promise.resolve().then(() => {
        if (cancelled) return;
        rememberConversationOwner(session.userId);
        setRestoredOwner(session.userId);
      });
    return () => {
      cancelled = true;
    };
  }, [session]);
  async function handleLogout(): Promise<void> {
    if (inFlight.current) return;
    inFlight.current = true;
    setBusy(true);
    setError(null);
    const success = await logout("", fetcher);
    inFlight.current = false;
    setBusy(false);
    if (success) {
      clearConversationDrafts();
      onNavigate("/login");
    } else setError("退出登录失败，请重试。");
  }

  return (
    <div className="workbench">
      <nav aria-label="工作台" className="workbench-nav">
        <Link className="workbench-brand" href="/">
          <span aria-hidden="true" className="workbench-mark">
            ab
          </span>
          <span>Artificial Brain</span>
        </Link>
        <span className="workbench-links">
          {workbenchLinks.map((link) => (
            <Link
              aria-current={pathname === link.href ? "page" : undefined}
              className={
                pathname === link.href
                  ? "workbench-link workbench-link-active"
                  : "workbench-link"
              }
              href={link.href}
              key={link.href}
            >
              {link.label}
            </Link>
          ))}
        </span>
        {session ? (
          <span className="workbench-account">
            <span title={session.userId}>账号：{session.userId}</span>
            <button
              className="btn-ghost"
              disabled={busy}
              onClick={() => void handleLogout()}
              type="button"
            >
              退出登录
            </button>
          </span>
        ) : null}
      </nav>
      {error ? (
        <p className="login-error" role="alert">
          {error}
        </p>
      ) : null}
      <div className="workbench-content">
        {!session || restoredOwner === session.userId ? (
          children
        ) : (
          <p role="status">正在恢复工作台…</p>
        )}
      </div>
    </div>
  );
}
