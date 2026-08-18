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
    expect(screen.getByRole("alert")).toHaveTextContent("对话服务暂时不可用"),
  );
  expect(screen.queryByText("boom")).not.toBeInTheDocument();
});
