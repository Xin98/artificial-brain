import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { expect, it, vi } from "vitest";

import { TodoActions } from "./todo-actions";

const pendingTodo = {
  id: "todo-1",
  title: "提交周报",
  status: "pending" as const,
  overdue: false,
  reminderVersion: 1,
  version: 3,
  createdAt: "2026-08-18T00:00:00Z",
  updatedAt: "2026-08-18T00:00:00Z",
};

function json(status: number, body: unknown): Response {
  return new Response(JSON.stringify(body), {
    status,
    headers: { "content-type": "application/json" },
  });
}

it("completes with the exact version in the body", async () => {
  const fetcher = vi
    .fn()
    .mockResolvedValue(
      json(200, { ...pendingTodo, status: "completed", version: 4 }),
    );
  const onChanged = vi.fn();
  render(
    <TodoActions
      fetcher={fetcher as unknown as typeof fetch}
      onChanged={onChanged}
      todo={pendingTodo}
    />,
  );

  fireEvent.click(screen.getByRole("button", { name: "完成" }));

  await waitFor(() => expect(onChanged).toHaveBeenCalled());
  const [url, init] = fetcher.mock.calls[0];
  expect(url).toBe("/api/v1/todos/todo-1/complete");
  expect(JSON.parse(String(init?.body))).toEqual({ version: 3 });
});

it("deletes through the two-step confirmation flow", async () => {
  const fetcher = vi
    .fn()
    .mockResolvedValueOnce(
      json(201, {
        confirmationId: "conf-1",
        expiresAt: "2026-08-18T12:05:00Z",
      }),
    )
    .mockResolvedValueOnce(
      json(200, { kind: "todo_deleted", todoId: "todo-1" }),
    );
  const onChanged = vi.fn();
  render(
    <TodoActions
      fetcher={fetcher as unknown as typeof fetch}
      onChanged={onChanged}
      todo={pendingTodo}
    />,
  );

  fireEvent.click(screen.getByRole("button", { name: "删除" }));
  await waitFor(() =>
    expect(
      screen.getByRole("button", { name: "确认删除" }),
    ).toBeInTheDocument(),
  );
  const [createUrl, createInit] = fetcher.mock.calls[0];
  expect(createUrl).toBe("/api/v1/confirmations");
  expect(JSON.parse(String(createInit?.body))).toEqual({
    intent: "todo.delete",
    todoId: "todo-1",
  });

  fireEvent.click(screen.getByRole("button", { name: "确认删除" }));
  await waitFor(() => expect(onChanged).toHaveBeenCalled());
  const [confirmUrl, confirmInit] = fetcher.mock.calls[1];
  expect(confirmUrl).toBe("/api/v1/confirmations/conf-1/confirm");
  expect(JSON.parse(String(confirmInit?.body))).toEqual({});
});

it("surfaces conflict errors without raw payloads", async () => {
  const fetcher = vi
    .fn()
    .mockResolvedValue(
      json(409, { code: "conflict", message: "internal detail" }),
    );
  render(
    <TodoActions
      fetcher={fetcher as unknown as typeof fetch}
      onChanged={vi.fn()}
      todo={pendingTodo}
    />,
  );

  fireEvent.click(screen.getByRole("button", { name: "完成" }));

  await waitFor(() =>
    expect(screen.getByRole("alert")).toHaveTextContent("待办已被更新"),
  );
  expect(screen.getByRole("alert")).not.toHaveTextContent("internal detail");
});
