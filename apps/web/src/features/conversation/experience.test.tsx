import {
  act,
  fireEvent,
  render,
  screen,
  waitFor,
} from "@testing-library/react";
import { afterEach, beforeEach, expect, it, vi } from "vitest";

import { ChatPanel } from "./chat-panel";
import { ConversationView } from "./conversation-view";
import { fetchSessionMessages, listSessions } from "./fetch-sessions";
import { SessionSidebar } from "./session-sidebar";

beforeEach(() => sessionStorage.clear());
afterEach(() => sessionStorage.clear());
const json = (body: unknown, status = 200) =>
  new Response(JSON.stringify(body), { status });

it("restores the saved session before allowing a first message", async () => {
  sessionStorage.setItem("ab.conversation.active", "saved");
  let resolve!: (response: Response) => void;
  const fetcher = vi
    .fn()
    .mockImplementationOnce(
      () =>
        new Promise<Response>((done) => {
          resolve = done;
        }),
    )
    .mockResolvedValue(
      json({ sessionId: "saved", title: "之前的会话", messages: [] }),
    );
  render(<ConversationView fetcher={fetcher as typeof fetch} />);
  expect(screen.queryByLabelText("消息")).not.toBeInTheDocument();
  expect(screen.getByText("正在恢复会话…")).toBeInTheDocument();
  await act(async () =>
    resolve(
      json({
        sessions: [
          {
            id: "saved",
            title: "之前的会话",
            createdAt: "2026-10-06T01:00:00Z",
            updatedAt: "2026-10-06T01:00:00Z",
          },
        ],
      }),
    ),
  );
  fireEvent.change(screen.getByLabelText("消息"), {
    target: { value: "你好" },
  });
  await waitFor(() =>
    expect(screen.getByRole("button", { name: "发送" })).toBeEnabled(),
  );
  expect(fetcher.mock.calls[1][0]).toContain("/sessions/saved/messages");
});

it("shows a multiline outgoing message immediately while waiting for the reply", async () => {
  let resolve!: (response: Response) => void;
  const fetcher = vi.fn(
    () =>
      new Promise<Response>((done) => {
        resolve = done;
      }),
  );
  render(<ChatPanel fetcher={fetcher as typeof fetch} />);
  const input = screen.getByLabelText("消息");
  expect(input.tagName).toBe("TEXTAREA");
  fireEvent.change(input, { target: { value: "第一行\n第二行" } });
  fireEvent.click(screen.getByRole("button", { name: "发送" }));
  expect(screen.getByRole("log")).toHaveTextContent("第一行");
  expect(screen.getByText("正在回复…")).toBeInTheDocument();
  await act(async () =>
    resolve(json({ kind: "chat", correlationId: "c", reply: "收到" })),
  );
  expect(screen.getByText("收到")).toBeInTheDocument();
});

it("retries history loading in place and blocks sending into unloaded history", async () => {
  const fetcher = vi
    .fn()
    .mockResolvedValueOnce(json({}, 503))
    .mockResolvedValue(json({ sessionId: "s1", title: "测试", messages: [] }));
  render(<ChatPanel sessionId="s1" fetcher={fetcher as typeof fetch} />);
  fireEvent.change(screen.getByLabelText("消息"), {
    target: { value: "你好" },
  });
  expect(screen.getByRole("button", { name: "发送" })).toBeDisabled();
  await waitFor(() =>
    expect(
      screen.getByRole("button", { name: "重试加载历史" }),
    ).toBeInTheDocument(),
  );
  fireEvent.click(screen.getByRole("button", { name: "重试加载历史" }));
  await waitFor(() =>
    expect(screen.getByRole("button", { name: "发送" })).toBeEnabled(),
  );
});

it("identifies the destructive target and sends its session with confirmation", async () => {
  const fetcher = vi
    .fn()
    .mockResolvedValueOnce(
      json({ sessionId: "s1", title: "测试", messages: [] }),
    )
    .mockResolvedValueOnce(
      json({
        kind: "confirmation_required",
        correlationId: "c",
        confirmationId: "delete1",
        expiresAt: "2099-10-06T07:00:00Z",
        todo: { id: "t1", title: "提交周报", dueAtUtc: "2026-10-07T07:00:00Z" },
      }),
    )
    .mockResolvedValueOnce(
      json({ kind: "todo_deleted", correlationId: "c", todoId: "t1" }),
    );
  render(<ChatPanel sessionId="s1" fetcher={fetcher as typeof fetch} />);
  await waitFor(() =>
    expect(screen.queryByText("正在加载会话历史…")).not.toBeInTheDocument(),
  );
  fireEvent.change(screen.getByLabelText("消息"), {
    target: { value: "删除周报" },
  });
  fireEvent.click(screen.getByRole("button", { name: "发送" }));
  await waitFor(() =>
    expect(screen.getByText(/确认删除.*提交周报/)).toBeInTheDocument(),
  );
  fireEvent.click(screen.getByRole("button", { name: "确认删除" }));
  await waitFor(() => expect(fetcher).toHaveBeenCalledTimes(3));
  expect(JSON.parse(fetcher.mock.calls[2][1].body)).toEqual({
    sessionId: "s1",
  });
});

it("warns when the todo has no usable reminder channel", async () => {
  const fetcher = vi.fn().mockResolvedValue(
    json({
      kind: "todo_created",
      correlationId: "c",
      todo: { id: "t1", title: "周报", reminderScheduled: false },
      resolvedDueAtUtc: "2026-10-07T07:00:00Z",
      localEcho: "明天15:00",
    }),
  );
  render(<ChatPanel fetcher={fetcher as typeof fetch} />);
  fireEvent.change(screen.getByLabelText("消息"), {
    target: { value: "明天提醒我周报" },
  });
  fireEvent.click(screen.getByRole("button", { name: "发送" }));
  await waitFor(() =>
    expect(screen.getByText(/尚未配置可用提醒渠道/)).toBeInTheDocument(),
  );
  expect(screen.getByRole("link", { name: "配置提醒渠道" })).toHaveAttribute(
    "href",
    "/settings",
  );
});

it("retains the rename draft after the save fails", async () => {
  render(
    <SessionSidebar
      sessions={[
        { id: "s1", title: "原标题", updatedAt: "2026-10-06T00:00:00Z" },
      ]}
      onCreate={vi.fn()}
      onDelete={vi.fn()}
      onSelect={vi.fn()}
      onRename={async () => false}
    />,
  );
  fireEvent.click(screen.getByRole("button", { name: "重命名" }));
  fireEvent.change(screen.getByLabelText("会话标题"), {
    target: { value: "新标题" },
  });
  fireEvent.click(screen.getByRole("button", { name: "保存" }));
  await waitFor(() =>
    expect(screen.getByLabelText("会话标题")).toHaveValue("新标题"),
  );
});

it("exposes session list errors with a usable retry", async () => {
  const fetcher = vi
    .fn()
    .mockResolvedValueOnce(json({}, 503))
    .mockResolvedValue(json({ sessions: [] }));
  render(<ConversationView fetcher={fetcher as typeof fetch} />);
  await waitFor(() =>
    expect(screen.getByRole("alert")).toHaveTextContent("无法加载会话列表"),
  );
  fireEvent.click(screen.getByRole("button", { name: "重试加载会话" }));
  await waitFor(() =>
    expect(screen.queryByRole("alert")).not.toBeInTheDocument(),
  );
});

it("accepts pagination metadata and sends the next cursor without dropping messages", async () => {
  const fetcher = vi
    .fn()
    .mockResolvedValueOnce(
      json({ sessions: [], hasMore: true, nextOffset: 100 }),
    )
    .mockResolvedValueOnce(
      json({
        sessionId: "s1",
        title: "历史",
        messages: [],
        hasMore: true,
        nextBefore: "42",
      }),
    );
  const sessions = await listSessions("", fetcher as typeof fetch);
  expect(sessions).toMatchObject({ ok: true, hasMore: true, nextOffset: 100 });
  const history = await fetchSessionMessages(
    "",
    fetcher as typeof fetch,
    "s1",
    10000,
    "42",
  );
  expect(history).toMatchObject({
    ok: true,
    history: { hasMore: true, nextBefore: "42" },
  });
  expect(fetcher.mock.calls[1][0]).toBe(
    "/api/v1/conversation/sessions/s1/messages?before=42",
  );
});

it("does not notify a new panel when an unmounted request completes", async () => {
  let resolve!: (response: Response) => void;
  const fetcher = vi.fn(
    () =>
      new Promise<Response>((done) => {
        resolve = done;
      }),
  );
  const onSessionCreated = vi.fn();
  const panel = render(
    <ChatPanel
      fetcher={fetcher as typeof fetch}
      onSessionCreated={onSessionCreated}
    />,
  );
  fireEvent.change(screen.getByLabelText("消息"), {
    target: { value: "旧请求" },
  });
  fireEvent.click(screen.getByRole("button", { name: "发送" }));
  panel.unmount();
  await act(async () =>
    resolve(
      json({
        kind: "chat",
        correlationId: "c",
        reply: "旧回复",
        sessionId: "old",
      }),
    ),
  );
  expect(onSessionCreated).not.toHaveBeenCalled();
});

it("keeps a completed live reply when an older history request finishes later", async () => {
  let resolveOlder!: (response: Response) => void;
  const fetcher = vi.fn((url: string) => {
    if (url.includes("?before="))
      return new Promise<Response>((done) => {
        resolveOlder = done;
      });
    if (url.endsWith("/messages") && url.includes("sessions"))
      return Promise.resolve(
        json({
          sessionId: "s1",
          title: "测试",
          messages: [],
          hasMore: true,
          nextBefore: "42",
        }),
      );
    return Promise.resolve(
      json({ kind: "chat", correlationId: "c", reply: "保留这条回复" }),
    );
  });
  render(<ChatPanel sessionId="s1" fetcher={fetcher as typeof fetch} />);
  await waitFor(() =>
    expect(
      screen.getByRole("button", { name: "加载更早的消息" }),
    ).toBeInTheDocument(),
  );
  fireEvent.click(screen.getByRole("button", { name: "加载更早的消息" }));
  fireEvent.change(screen.getByLabelText("消息"), {
    target: { value: "新消息" },
  });
  fireEvent.click(screen.getByRole("button", { name: "发送" }));
  await waitFor(() =>
    expect(screen.getByText("保留这条回复")).toBeInTheDocument(),
  );
  await act(async () =>
    resolveOlder(
      json({
        sessionId: "s1",
        title: "测试",
        messages: [
          {
            id: "1",
            role: "assistant",
            body: "更早的记录",
            createdAt: "2026-10-01T00:00:00Z",
          },
        ],
        hasMore: false,
      }),
    ),
  );
  expect(screen.getByText("保留这条回复")).toBeInTheDocument();
});
