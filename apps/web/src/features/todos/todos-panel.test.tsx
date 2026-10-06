import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { expect, it, vi } from "vitest";
import { TodosPanel } from "./todos-panel";

it("preserves the applied keyword when creating a new todo", async () => {
  const todo = {
    id: "1",
    title: "周报",
    status: "pending",
    overdue: false,
    reminderVersion: 1,
    version: 1,
    createdAt: "2026-08-18T00:00:00Z",
    updatedAt: "2026-08-18T00:00:00Z",
  };
  const urls: string[] = [];
  const fetcher = vi.fn(async (url: RequestInfo | URL, init?: RequestInit) => {
    urls.push(String(url));
    return new Response(
      JSON.stringify(init?.method === "POST" ? todo : { todos: [todo] }),
      { status: init?.method === "POST" ? 201 : 200 },
    );
  });
  render(<TodosPanel fetcher={fetcher} />);
  await screen.findByText("周报");
  fireEvent.change(screen.getByLabelText("关键词"), {
    target: { value: "周报" },
  });
  fireEvent.click(screen.getByRole("button", { name: "筛选" }));
  await waitFor(() => expect(urls).toHaveLength(2));
  fireEvent.change(screen.getByLabelText("标题"), {
    target: { value: "周报" },
  });
  fireEvent.click(screen.getByRole("button", { name: "新建" }));
  await waitFor(() => expect(urls).toHaveLength(4));
  expect(urls[3]).toBe("/api/v1/todos?keyword=%E5%91%A8%E6%8A%A5");
  expect(screen.getByLabelText("关键词")).toHaveValue("周报");
});
