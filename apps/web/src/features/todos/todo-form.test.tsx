import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { expect, it, vi } from "vitest";

import { formatRFC3339UTC } from "../validation";
import { TodoForm } from "./todo-form";

function json(status: number, body: unknown): Response {
  return new Response(JSON.stringify(body), {
    status,
    headers: { "content-type": "application/json" },
  });
}

const createdTodo = {
  id: "todo-1",
  title: "提交周报",
  status: "pending" as const,
  overdue: false,
  reminderVersion: 1,
  version: 1,
  createdAt: "2026-08-18T00:00:00Z",
  updatedAt: "2026-08-18T00:00:00Z",
};

it("creates a todo with the browser timezone and UTC due", async () => {
  const fetcher = vi.fn().mockResolvedValue(json(201, createdTodo));
  const onDone = vi.fn();
  render(
    <TodoForm
      fetcher={fetcher as unknown as typeof fetch}
      onDone={onDone}
      timezoneProvider={() => "Asia/Shanghai"}
    />,
  );

  fireEvent.change(screen.getByLabelText("标题"), {
    target: { value: "提交周报" },
  });
  fireEvent.change(screen.getByLabelText("到期时间"), {
    target: { value: "2026-08-19T15:00" },
  });
  fireEvent.click(screen.getByRole("button", { name: "新建" }));

  await waitFor(() => expect(onDone).toHaveBeenCalled());
  const [url, init] = fetcher.mock.calls[0];
  expect(url).toBe("/api/v1/todos");
  const expectedDue = formatRFC3339UTC(new Date("2026-08-19T15:00"));
  expect(JSON.parse(String(init?.body))).toEqual({
    title: "提交周报",
    dueAtUtc: expectedDue,
    timezoneAtInput: "Asia/Shanghai",
  });
});

it("edits a todo carrying the version", async () => {
  const fetcher = vi
    .fn()
    .mockResolvedValue(
      json(200, { ...createdTodo, title: "新标题", version: 4 }),
    );
  const onDone = vi.fn();
  render(
    <TodoForm
      editing={{ ...createdTodo, version: 3 }}
      fetcher={fetcher as unknown as typeof fetch}
      onDone={onDone}
      timezoneProvider={() => "Asia/Shanghai"}
    />,
  );

  fireEvent.change(screen.getByLabelText("标题"), {
    target: { value: "新标题" },
  });
  fireEvent.click(screen.getByRole("button", { name: "保存" }));

  await waitFor(() => expect(onDone).toHaveBeenCalled());
  const [url, init] = fetcher.mock.calls[0];
  expect(url).toBe("/api/v1/todos/todo-1");
  const body = JSON.parse(String(init?.body)) as Record<string, unknown>;
  expect(body.version).toBe(3);
  expect(body.title).toBe("新标题");
});

it("rejects invalid input from the API with a stable message", async () => {
  const fetcher = vi
    .fn()
    .mockResolvedValue(
      json(422, { code: "validation_error", message: "raw details" }),
    );
  render(
    <TodoForm
      fetcher={fetcher as unknown as typeof fetch}
      onDone={vi.fn()}
      timezoneProvider={() => "UTC"}
    />,
  );

  fireEvent.change(screen.getByLabelText("标题"), { target: { value: "x" } });
  fireEvent.click(screen.getByRole("button", { name: "新建" }));

  await waitFor(() =>
    expect(screen.getByRole("alert")).toHaveTextContent("内容无效"),
  );
  expect(screen.getByRole("alert")).not.toHaveTextContent("raw details");
});

it("resets a successful create and announces the result", async () => {
  render(
    <TodoForm
      fetcher={vi.fn().mockResolvedValue(json(201, createdTodo))}
      onDone={vi.fn()}
    />,
  );
  fireEvent.change(screen.getByLabelText("标题"), {
    target: { value: "提交周报" },
  });
  fireEvent.click(screen.getByRole("button", { name: "新建" }));
  await waitFor(() => expect(screen.getByLabelText("标题")).toHaveValue(""));
  expect(screen.getByRole("status")).toHaveTextContent("已创建");
});

it("keeps the same instant when an edit saves an unchanged local due time", async () => {
  const fetcher = vi.fn().mockResolvedValue(json(200, createdTodo));
  const dueAtUtc = "2026-08-19T07:00:00Z";
  render(
    <TodoForm
      editing={{ ...createdTodo, dueAtUtc }}
      fetcher={fetcher}
      onDone={vi.fn()}
    />,
  );
  const due = new Date(dueAtUtc);
  const local = `${due.getFullYear()}-${String(due.getMonth() + 1).padStart(2, "0")}-${String(due.getDate()).padStart(2, "0")}T${String(due.getHours()).padStart(2, "0")}:${String(due.getMinutes()).padStart(2, "0")}`;
  expect(screen.getByLabelText("到期时间")).toHaveValue(local);
  fireEvent.click(screen.getByRole("button", { name: "保存" }));
  await waitFor(() => expect(fetcher).toHaveBeenCalled());
  expect(JSON.parse(String(fetcher.mock.calls[0][1]?.body)).dueAtUtc).toBe(
    dueAtUtc,
  );
});

it("ignores duplicate form submissions while creation is pending", async () => {
  let resolve: ((response: Response) => void) | undefined;
  const fetcher = vi.fn(
    () =>
      new Promise<Response>((done) => {
        resolve = done;
      }),
  );
  render(<TodoForm fetcher={fetcher} onDone={vi.fn()} />);
  fireEvent.change(screen.getByLabelText("标题"), {
    target: { value: "提交周报" },
  });
  fireEvent.submit(screen.getByRole("form", { name: "新建待办" }));
  fireEvent.submit(screen.getByRole("form", { name: "新建待办" }));
  expect(fetcher).toHaveBeenCalledTimes(1);
  resolve?.(json(201, createdTodo));
  await waitFor(() =>
    expect(screen.getByRole("status")).toHaveTextContent("已创建"),
  );
});
