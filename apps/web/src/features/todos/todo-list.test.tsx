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

it("offers description details and a cancellable edit without losing filters", async () => {
  const fetcher = vi
    .fn()
    .mockImplementation(async () =>
      listResponse([{ ...pendingTodo, description: "准备数据" }]),
    );
  render(<TodoList fetcher={fetcher} />);
  await screen.findByText("提交周报");
  expect(screen.getByText("准备数据")).toBeInTheDocument();
  fireEvent.change(screen.getByLabelText("关键词"), {
    target: { value: "周报" },
  });
  fireEvent.click(screen.getByRole("button", { name: "筛选" }));
  await waitFor(() => expect(fetcher).toHaveBeenCalledTimes(2));
  fireEvent.click(screen.getByRole("button", { name: "编辑" }));
  expect(screen.getByLabelText("标题")).toHaveValue("提交周报");
  fireEvent.click(screen.getByRole("button", { name: "取消编辑" }));
  expect(screen.queryByLabelText("标题")).not.toBeInTheDocument();
  expect(screen.getByLabelText("关键词")).toHaveValue("周报");
});

it("requests completion filtering before the API limit so recent completed todos are visible", async () => {
  const recent = {
    ...pendingTodo,
    status: "completed",
    completedAt: new Date().toISOString(),
  };
  const stored = [
    ...Array.from({ length: 201 }, (_unused, index) => ({
      ...recent,
      id: `old-${index}`,
      title: `旧完成记录 ${index}`,
      completedAt: "2020-01-01T00:00:00Z",
    })),
    recent,
  ];
  const fetcher = vi.fn(async (input: RequestInfo | URL) => {
    const cutoff = new URL(
      String(input),
      "https://workbench.test",
    ).searchParams.get("completedSince");
    const matches = cutoff
      ? stored.filter(
          (todo) => Date.parse(todo.completedAt) >= Date.parse(cutoff),
        )
      : stored;
    return listResponse(matches.slice(0, 200));
  });
  render(<TodoList fetcher={fetcher} initialView="completed7d" />);
  await screen.findByText("提交周报");
  expect(screen.queryByText(/旧完成记录/)).not.toBeInTheDocument();
  const query = new URL(
    String(fetcher.mock.calls[0][0]),
    "https://workbench.test",
  ).searchParams;
  expect(query.get("status")).toBe("completed");
  const since = Date.parse(query.get("completedSince") ?? "");
  expect(since).toBeGreaterThan(Date.now() - 7 * 86400000 - 2000);
  expect(since).toBeLessThanOrEqual(Date.now() - 7 * 86400000);
});
