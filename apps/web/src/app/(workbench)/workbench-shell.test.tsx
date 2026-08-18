import { render, screen } from "@testing-library/react";
import { expect, it } from "vitest";

import { WorkbenchShell } from "./workbench-shell";

it("renders navigation to the workbench areas", () => {
  render(
    <WorkbenchShell>
      <p>page content</p>
    </WorkbenchShell>,
  );

  expect(screen.getByRole("link", { name: "Dashboard" })).toHaveAttribute(
    "href",
    "/",
  );
  expect(screen.getByRole("link", { name: "Todos" })).toHaveAttribute(
    "href",
    "/todos",
  );
  expect(screen.getByRole("link", { name: "Conversation" })).toHaveAttribute(
    "href",
    "/conversation",
  );
  expect(screen.getByRole("link", { name: "Settings" })).toHaveAttribute(
    "href",
    "/settings",
  );
  expect(screen.getByText("page content")).toBeInTheDocument();
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
