import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { expect, it, vi } from "vitest";

import { ChatPanel } from "./chat-panel";

function json(status: number, body: unknown): Response {
  return new Response(JSON.stringify(body), {
    status,
    headers: { "content-type": "application/json" },
  });
}

it("sends the turn with the browser timezone and echoes the resolved time", async () => {
  const fetcher = vi.fn().mockResolvedValue(
    json(200, {
      kind: "todo_created",
      correlationId: "corr-1",
      todo: { id: "todo-1", title: "提交周报" },
      resolvedDueAtUtc: "2026-08-19T07:00:00Z",
      localEcho: "2026-08-19 15:00",
      timezoneEcho: "Asia/Shanghai",
    }),
  );
  render(
    <ChatPanel
      fetcher={fetcher as unknown as typeof fetch}
      timezoneProvider={() => "Asia/Shanghai"}
    />,
  );

  fireEvent.change(screen.getByLabelText("消息"), {
    target: { value: "明天下午三点提醒我提交周报" },
  });
  fireEvent.click(screen.getByRole("button", { name: "发送" }));

  await waitFor(() =>
    expect(screen.getByText(/已创建待办/)).toBeInTheDocument(),
  );
  const [url, init] = fetcher.mock.calls[0];
  expect(url).toBe("/api/v1/conversation/messages");
  expect(JSON.parse(String(init?.body))).toEqual({
    text: "明天下午三点提醒我提交周报",
    timezone: "Asia/Shanghai",
  });
  expect(screen.getByText(/2026-08-19 15:00/)).toBeInTheDocument();
});

it("walks candidate selection into the confirmation-gated delete", async () => {
  const fetcher = vi
    .fn()
    .mockResolvedValueOnce(
      json(200, {
        kind: "candidates",
        correlationId: "corr-2",
        candidates: [{ todoId: "todo-9", title: "提交周报", version: 4 }],
      }),
    )
    .mockResolvedValueOnce(
      json(201, {
        confirmationId: "conf-1",
        expiresAt: "2026-08-18T12:05:00Z",
      }),
    )
    .mockResolvedValueOnce(
      json(200, {
        kind: "todo_deleted",
        correlationId: "corr-3",
        todoId: "todo-9",
      }),
    );
  render(<ChatPanel fetcher={fetcher as unknown as typeof fetch} />);

  fireEvent.change(screen.getByLabelText("消息"), {
    target: { value: "删除周报" },
  });
  fireEvent.click(screen.getByRole("button", { name: "发送" }));

  await waitFor(() =>
    expect(
      screen.getByRole("button", { name: "提交周报" }),
    ).toBeInTheDocument(),
  );
  fireEvent.click(screen.getByRole("button", { name: "提交周报" }));

  await waitFor(() => {
    const [createUrl, createInit] = fetcher.mock.calls[1];
    expect(createUrl).toBe("/api/v1/confirmations");
    expect(JSON.parse(String(createInit?.body))).toEqual({
      intent: "todo.delete",
      todoId: "todo-9",
    });
  });

  await waitFor(() =>
    expect(
      screen.getByRole("button", { name: "确认删除" }),
    ).toBeInTheDocument(),
  );
  fireEvent.click(screen.getByRole("button", { name: "确认删除" }));

  await waitFor(() =>
    expect(screen.getByText("已删除待办。")).toBeInTheDocument(),
  );
  const [confirmUrl, confirmInit] = fetcher.mock.calls[2];
  expect(confirmUrl).toBe("/api/v1/confirmations/conf-1/confirm");
  expect(JSON.parse(String(confirmInit?.body))).toEqual({});
});

it("applies an older candidate confirmation to its originating turn", async () => {
  const fetcher = vi
    .fn()
    .mockResolvedValueOnce(
      json(200, {
        kind: "candidates",
        correlationId: "corr-candidates",
        candidates: [{ todoId: "todo-9", title: "提交周报", version: 4 }],
      }),
    )
    .mockResolvedValueOnce(
      json(200, { kind: "unsupported", correlationId: "corr-later" }),
    )
    .mockResolvedValueOnce(
      json(201, {
        confirmationId: "conf-older-turn",
        expiresAt: "2026-08-18T12:05:00Z",
      }),
    )
    .mockResolvedValueOnce(
      json(200, {
        kind: "todo_deleted",
        correlationId: "corr-deleted",
        todoId: "todo-9",
      }),
    );
  render(<ChatPanel fetcher={fetcher as unknown as typeof fetch} />);

  fireEvent.change(screen.getByLabelText("消息"), {
    target: { value: "删除周报" },
  });
  fireEvent.click(screen.getByRole("button", { name: "发送" }));
  await waitFor(() =>
    expect(
      screen.getByRole("button", { name: "提交周报" }),
    ).toBeInTheDocument(),
  );

  fireEvent.change(screen.getByLabelText("消息"), {
    target: { value: "今天天气怎么样" },
  });
  fireEvent.click(screen.getByRole("button", { name: "发送" }));
  await waitFor(() =>
    expect(screen.getByText("这个请求暂时不支持。")).toBeInTheDocument(),
  );

  fireEvent.click(screen.getByRole("button", { name: "提交周报" }));
  await waitFor(() => {
    const confirm = screen.getByRole("button", { name: "确认删除" });
    expect(confirm).toBeInTheDocument();
    expect(confirm).toHaveFocus();
  });
  fireEvent.click(screen.getByRole("button", { name: "确认删除" }));

  await waitFor(() =>
    expect(screen.getByText("已删除待办。")).toBeInTheDocument(),
  );
  const olderTurn = screen.getByText("删除周报").closest(".chat-turn");
  const laterTurn = screen.getByText("今天天气怎么样").closest(".chat-turn");
  expect(olderTurn).toHaveTextContent("已删除待办。");
  expect(laterTurn).toHaveTextContent("这个请求暂时不支持。");
});

it("renders unsupported intents without raw error text", async () => {
  const fetcher = vi
    .fn()
    .mockResolvedValue(
      json(200, { kind: "unsupported", correlationId: "corr-4" }),
    );
  render(<ChatPanel fetcher={fetcher as unknown as typeof fetch} />);

  fireEvent.change(screen.getByLabelText("消息"), {
    target: { value: "今天天气怎么样" },
  });
  fireEvent.click(screen.getByRole("button", { name: "发送" }));

  await waitFor(() =>
    expect(screen.getByText("这个请求暂时不支持。")).toBeInTheDocument(),
  );
});

it("fails closed when the service is unavailable", async () => {
  const fetcher = vi.fn().mockRejectedValue(new Error("boom"));
  render(<ChatPanel fetcher={fetcher as unknown as typeof fetch} />);

  fireEvent.change(screen.getByLabelText("消息"), {
    target: { value: "你好" },
  });
  fireEvent.click(screen.getByRole("button", { name: "发送" }));

  await waitFor(() =>
    expect(screen.getByRole("alert")).toHaveTextContent("先检查待办列表"),
  );
  expect(screen.getByLabelText("消息")).toHaveValue("你好");
  expect(
    screen.queryByRole("button", { name: "重试上一条" }),
  ).not.toBeInTheDocument();
  expect(screen.queryByText("boom")).not.toBeInTheDocument();
});

it("keeps successful turns in session history", async () => {
  const fetcher = vi
    .fn()
    .mockResolvedValueOnce(
      json(200, {
        kind: "todo_list",
        correlationId: "corr-list",
        todos: [{ id: "todo-1", title: "提交周报" }],
      }),
    )
    .mockResolvedValueOnce(
      json(200, { kind: "unsupported", correlationId: "corr-unsupported" }),
    );
  render(<ChatPanel fetcher={fetcher as unknown as typeof fetch} />);

  fireEvent.change(screen.getByLabelText("消息"), {
    target: { value: "列出待办" },
  });
  fireEvent.click(screen.getByRole("button", { name: "发送" }));
  await waitFor(() => expect(screen.getByText("提交周报")).toBeInTheDocument());

  fireEvent.change(screen.getByLabelText("消息"), {
    target: { value: "今天天气怎么样" },
  });
  fireEvent.click(screen.getByRole("button", { name: "发送" }));

  await waitFor(() =>
    expect(screen.getByText("这个请求暂时不支持。")).toBeInTheDocument(),
  );
  expect(screen.getByText("列出待办")).toBeInTheDocument();
  expect(screen.getByText("今天天气怎么样")).toBeInTheDocument();
  expect(screen.getByText("提交周报")).toBeInTheDocument();
  expect(screen.getByRole("log")).toHaveAttribute(
    "aria-relevant",
    "additions text",
  );
});

it("does not clear a newer draft when an older request completes", async () => {
  let resolveRequest: ((response: Response) => void) | undefined;
  const request = new Promise<Response>((resolve) => {
    resolveRequest = resolve;
  });
  const fetcher = vi.fn(() => request);
  render(<ChatPanel fetcher={fetcher as unknown as typeof fetch} />);

  fireEvent.change(screen.getByLabelText("消息"), {
    target: { value: "第一条" },
  });
  fireEvent.click(screen.getByRole("button", { name: "发送" }));
  fireEvent.change(screen.getByLabelText("消息"), {
    target: { value: "下一条草稿" },
  });

  resolveRequest?.(
    json(200, { kind: "unsupported", correlationId: "corr-first" }),
  );

  await waitFor(() =>
    expect(screen.getByText("这个请求暂时不支持。")).toBeInTheDocument(),
  );
  expect(screen.getByLabelText("消息")).toHaveValue("下一条草稿");
});

it("preserves but does not blindly retry an ambiguously timed-out message", async () => {
  const fetcher = vi
    .fn()
    .mockRejectedValueOnce(new DOMException("deadline", "TimeoutError"));
  render(<ChatPanel fetcher={fetcher as unknown as typeof fetch} />);

  fireEvent.change(screen.getByLabelText("消息"), {
    target: { value: "你好" },
  });
  fireEvent.click(screen.getByRole("button", { name: "发送" }));

  await waitFor(() =>
    expect(screen.getByRole("alert")).toHaveTextContent("先检查待办列表"),
  );
  expect(
    screen.queryByRole("button", { name: "重试上一条" }),
  ).not.toBeInTheDocument();
  expect(fetcher).toHaveBeenCalledTimes(1);
  expect(screen.getByLabelText("消息")).toHaveValue("你好");
});

it("retries an explicit server failure without rendering its message", async () => {
  const fetcher = vi
    .fn()
    .mockResolvedValueOnce(
      json(503, {
        code: "internal_error",
        message: "upstream secret should stay hidden",
        correlationId: "corr-support-1",
      }),
    )
    .mockResolvedValueOnce(
      json(200, { kind: "unsupported", correlationId: "corr-retry" }),
    );
  render(<ChatPanel fetcher={fetcher as unknown as typeof fetch} />);

  fireEvent.change(screen.getByLabelText("消息"), {
    target: { value: "你好" },
  });
  fireEvent.click(screen.getByRole("button", { name: "发送" }));

  await waitFor(() =>
    expect(screen.getByRole("alert")).toHaveTextContent("corr-support-1"),
  );
  expect(screen.getByRole("alert")).not.toHaveTextContent("upstream secret");
  fireEvent.click(screen.getByRole("button", { name: "重试上一条" }));
  await waitFor(() =>
    expect(screen.getByText("这个请求暂时不支持。")).toBeInTheDocument(),
  );
  expect(fetcher).toHaveBeenCalledTimes(2);
});

it("disables sending until a non-empty message is present", () => {
  render(<ChatPanel fetcher={vi.fn() as unknown as typeof fetch} />);

  expect(screen.getByRole("button", { name: "发送" })).toBeDisabled();
  fireEvent.change(screen.getByLabelText("消息"), {
    target: { value: "   " },
  });
  expect(screen.getByRole("button", { name: "发送" })).toBeDisabled();
});
