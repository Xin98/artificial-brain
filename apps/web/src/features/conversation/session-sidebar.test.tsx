import { fireEvent, render, screen } from "@testing-library/react";
import { expect, it, vi } from "vitest";

import { SessionSidebar } from "./session-sidebar";
import type { SidebarSession } from "./session-sidebar";

const sessions: SidebarSession[] = [
  {
    id: "s2",
    title: "闲聊",
    updatedAt: "2026-09-27T09:00:00Z",
  },
  {
    id: "s1",
    title: "周报会话",
    updatedAt: "2026-09-26T09:00:00Z",
  },
];

function setup(props: Partial<Parameters<typeof SessionSidebar>[0]> = {}) {
  const callbacks = {
    onCreate: vi.fn(),
    onSelect: vi.fn(),
    onRename: vi.fn(),
    onDelete: vi.fn(),
  };
  render(
    <SessionSidebar
      sessions={sessions}
      activeId="s1"
      {...callbacks}
      {...props}
    />,
  );
  return callbacks;
}

it("lists sessions and marks the active one", () => {
  setup();

  expect(screen.getByText("闲聊")).toBeInTheDocument();
  expect(screen.getByText("周报会话")).toBeInTheDocument();
  const items = screen.getAllByRole("listitem");
  expect(items).toHaveLength(2);
  expect(items[0]).not.toHaveAttribute("aria-current");
  expect(items[1]).toHaveAttribute("aria-current", "true");
});

it("selects, creates and deletes through props", () => {
  const callbacks = setup();

  fireEvent.click(screen.getByRole("button", { name: /闲聊/ }));
  expect(callbacks.onSelect).toHaveBeenCalledWith("s2");

  fireEvent.click(screen.getByRole("button", { name: "新建会话" }));
  expect(callbacks.onCreate).toHaveBeenCalledTimes(1);

  const deleteButtons = screen.getAllByRole("button", { name: "删除" });
  fireEvent.click(deleteButtons[0]);
  expect(callbacks.onDelete).toHaveBeenCalledWith("s2");
});

it("renames inline with a trimmed non-empty title", () => {
  const callbacks = setup();

  fireEvent.click(screen.getAllByRole("button", { name: "重命名" })[0]);
  const input = screen.getByLabelText("会话标题");
  expect(input).toHaveValue("闲聊");
  fireEvent.change(input, { target: { value: "  冲刺计划  " } });
  fireEvent.click(screen.getByRole("button", { name: "保存" }));

  expect(callbacks.onRename).toHaveBeenCalledWith("s2", "冲刺计划");
  expect(screen.queryByLabelText("会话标题")).not.toBeInTheDocument();
});

it("keeps the editor open on a blank title and cancels without renaming", () => {
  const callbacks = setup();

  fireEvent.click(screen.getAllByRole("button", { name: "重命名" })[0]);
  fireEvent.change(screen.getByLabelText("会话标题"), {
    target: { value: "   " },
  });
  fireEvent.click(screen.getByRole("button", { name: "保存" }));
  expect(callbacks.onRename).not.toHaveBeenCalled();
  expect(screen.getByLabelText("会话标题")).toBeInTheDocument();

  fireEvent.click(screen.getByRole("button", { name: "取消" }));
  expect(callbacks.onRename).not.toHaveBeenCalled();
  expect(screen.queryByLabelText("会话标题")).not.toBeInTheDocument();
});

it("disables interactions while busy", () => {
  setup({ busy: true });

  expect(screen.getByRole("button", { name: "新建会话" })).toBeDisabled();
  expect(screen.getByRole("button", { name: /闲聊/ })).toBeDisabled();
  for (const button of screen.getAllByRole("button", { name: "重命名" })) {
    expect(button).toBeDisabled();
  }
  for (const button of screen.getAllByRole("button", { name: "删除" })) {
    expect(button).toBeDisabled();
  }
});
