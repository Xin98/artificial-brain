"use client";

import Link from "next/link";
import { usePathname } from "next/navigation";

const workbenchLinks = [
  { href: "/", label: "Dashboard" },
  { href: "/todos", label: "Todos" },
  { href: "/conversation", label: "Conversation" },
  { href: "/settings", label: "Settings" },
  { href: "/data", label: "Data" },
];

// WorkbenchShell is the presentational frame for session-gated pages: a
// sticky single-line nav with the brand mark and the five workbench areas
// (the active route stays highlighted) plus the page content. It renders no
// internal URLs or configuration.
export function WorkbenchShell({
  children,
}: {
  children: React.ReactNode;
}): React.JSX.Element {
  const pathname = usePathname();

  return (
    <div className="workbench">
      <nav aria-label="Workbench" className="workbench-nav">
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
      </nav>
      <div className="workbench-content">{children}</div>
    </div>
  );
}
