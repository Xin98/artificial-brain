import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { expect, it, vi } from "vitest";

import { TodoList } from "./todo-list";

const pendingTodo = {
  id: "todo-1",
  title: "提交周报",
  status: "pending",
  overdue: false,
  reminderVersion: 1,
  version: 3,
  createdAt: "2026-08-18T00:00:00Z",
  updatedAt: "2026-08-18T00:00:00Z",
  dueAtUtc: "2026-08-19T07:00:00Z",
};

function listResponse(todos: unknown[]): Response {
  return new Response(JSON.stringify({ todos }), {
    status: 200,
    headers: { "content-type": "application/json" },
  });
}

it("loads todos and applies combinable filters", async () => {
  const fetcher = vi
    .fn()
    .mockResolvedValueOnce(listResponse([pendingTodo]))
    .mockResolvedValueOnce(listResponse([]));
  render(<TodoList fetcher={fetcher as unknown as typeof fetch} />);

  await waitFor(() => expect(screen.getByText("提交周报")).toBeInTheDocument());
  expect(fetcher.mock.calls[0][0]).toBe("/api/v1/todos");

  fireEvent.change(screen.getByLabelText("关键词"), {
    target: { value: "周报" },
  });
  fireEvent.change(screen.getByLabelText("状态"), {
    target: { value: "pending" },
  });
  fireEvent.click(screen.getByRole("button", { name: "筛选" }));

  await waitFor(() => expect(fetcher).toHaveBeenCalledTimes(2));
  const url = String(fetcher.mock.calls[1][0]);
  expect(url).toContain("keyword=%E5%91%A8%E6%8A%A5");
  expect(url).toContain("status=pending");
});

it("shows a fail-closed message when the list cannot load", async () => {
  const fetcher = vi
    .fn()
    .mockResolvedValue(new Response("{}", { status: 500 }));
  render(<TodoList fetcher={fetcher as unknown as typeof fetch} />);

  await waitFor(() =>
    expect(screen.getByRole("alert")).toHaveTextContent("待办加载失败"),
  );
});
