import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, expect, it, vi } from "vitest";
import { useEffect } from "react";

import { WorkbenchShell } from "./workbench-shell";

// The shell reads the active route from the app router; unit tests render it
// outside a router, so the hook is stubbed to a fixed path.
const location = vi.hoisted(() => ({ pathname: "/" }));
afterEach(() => {
  location.pathname = "/";
});
vi.mock("next/navigation", () => ({
  usePathname: () => location.pathname,
}));

it("renders navigation to the workbench areas", () => {
  render(
    <WorkbenchShell>
      <p>page content</p>
    </WorkbenchShell>,
  );

  expect(screen.getByRole("link", { name: "概况" })).toHaveAttribute(
    "href",
    "/",
  );
  expect(screen.getByRole("link", { name: "待办" })).toHaveAttribute(
    "href",
    "/todos",
  );
  expect(screen.getByRole("link", { name: "对话" })).toHaveAttribute(
    "href",
    "/conversation",
  );
  expect(screen.getByRole("link", { name: "设置" })).toHaveAttribute(
    "href",
    "/settings",
  );
  expect(screen.getByRole("link", { name: "数据" })).toHaveAttribute(
    "href",
    "/data",
  );
  expect(screen.getByText("page content")).toBeInTheDocument();
});

it("shows the account and clears owned drafts only after successful logout", async () => {
  sessionStorage.setItem("ab.conversation.draft.new", "private draft");
  const navigate = vi.fn();
  const fetcher = vi
    .fn()
    .mockResolvedValue(new Response("{}", { status: 200 }));
  render(
    <WorkbenchShell
      session={{ userId: "user-1", workspaceId: "ws-1", sessionId: "s-1" }}
      fetcher={fetcher}
      onNavigate={navigate}
    >
      <p>content</p>
    </WorkbenchShell>,
  );
  expect(screen.getByText(/user-1/)).toBeInTheDocument();
  fireEvent.click(screen.getByRole("button", { name: "退出登录" }));
  await waitFor(() => expect(navigate).toHaveBeenCalledWith("/login"));
  expect(sessionStorage.getItem("ab.conversation.draft.new")).toBeNull();
  expect(String(fetcher.mock.calls[0][0])).toBe("/api/v1/auth/logout");
});

it("clears a previous account's drafts before child features restore them", async () => {
  sessionStorage.setItem("ab.conversation.owner", "previous-user");
  sessionStorage.setItem("ab.conversation.draft.new", "private draft");
  const restored = vi.fn();
  function RestoringChild(): React.JSX.Element {
    useEffect(() => {
      restored(sessionStorage.getItem("ab.conversation.draft.new"));
    }, []);
    return <p>child</p>;
  }
  render(
    <WorkbenchShell
      session={{ userId: "new-user", workspaceId: "ws-1", sessionId: "s-1" }}
    >
      <RestoringChild />
    </WorkbenchShell>,
  );
  await waitFor(() => expect(restored).toHaveBeenCalledWith(null));
});

it("does not leak internal URLs or configuration names", () => {
  const { container } = render(
    <WorkbenchShell>
      <p>page content</p>
    </WorkbenchShell>,
  );

  expect(container.innerHTML).not.toContain("http://");
  expect(container.innerHTML).not.toContain("https://");
  expect(container.innerHTML).not.toContain("API_INTERNAL_URL");
});

it.each(["/investment/stocks/fixture-01", "/investment/accounts/one"])(
  "investment subroutes keep navigation active: %s",
  (path) => {
    location.pathname = path;
    render(
      <WorkbenchShell>
        <p>content</p>
      </WorkbenchShell>,
    );
    expect(screen.getByRole("link", { name: "投资" })).toHaveAttribute(
      "aria-current",
      "page",
    );
    expect(screen.getByRole("link", { name: "概况" })).not.toHaveAttribute(
      "aria-current",
    );
  },
);
