import Link from "next/link";

const workbenchLinks = [
  { href: "/", label: "Dashboard" },
  { href: "/todos", label: "Todos" },
  { href: "/conversation", label: "Conversation" },
  { href: "/settings", label: "Settings" },
];

// WorkbenchShell is the presentational frame for session-gated pages: a nav
// with the four workbench areas plus the page content. It renders no
// internal URLs or configuration.
export function WorkbenchShell({
  children,
}: {
  children: React.ReactNode;
}): React.JSX.Element {
  return (
    <div className="workbench">
      <nav aria-label="Workbench" className="workbench-nav">
        <span className="workbench-brand">Artificial Brain</span>
        {workbenchLinks.map((link) => (
          <Link href={link.href} key={link.href}>
            {link.label}
          </Link>
        ))}
      </nav>
      <div className="workbench-content">{children}</div>
    </div>
  );
}
