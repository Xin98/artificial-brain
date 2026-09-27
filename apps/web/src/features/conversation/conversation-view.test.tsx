import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, expect, it, vi } from "vitest";

import { ConversationView } from "./conversation-view";

afterEach(() => {
  vi.restoreAllMocks();
});

function json(status: number, body: unknown): Response {
  return new Response(JSON.stringify(body), {
    status,
    headers: { "content-type": "application/json" },
  });
}

interface RouteLog {
  url: string;
  method: string;
  body?: string;
}

// fakeFetcher routes requests through a handler closure and records every
// call so tests can assert both payloads and call counts.
function fakeFetcher(
  handler: (url: string, init?: RequestInit) => Response | undefined,
) {
  const log: RouteLog[] = [];
  const fetcher = vi.fn(
    async (url: string | URL | Request, init?: RequestInit) => {
      const path = String(url);
      log.push({
        url: path,
        method: init?.method ?? "GET",
        body: init?.body === undefined ? undefined : String(init.body),
      });
      const response = handler(path, init);
      if (!response) {
        throw new Error(`unexpected route ${init?.method ?? "GET"} ${path}`);
      }
      return response;
    },
  );
  return { fetcher: fetcher as unknown as typeof fetch, log };
}

const weekly = {
  id: "s1",
  title: "周报会话",
  createdAt: "2026-09-26T08:00:00Z",
  updatedAt: "2026-09-27T09:00:00Z",
};
const casual = {
  id: "s2",
  title: "闲聊",
  createdAt: "2026-09-27T08:00:00Z",
  updatedAt: "2026-09-27T10:00:00Z",
};

it("loads the sidebar and switches the panel to the selected session", async () => {
  const { fetcher, log } = fakeFetcher((url) => {
    if (url === "/api/v1/conversation/sessions") {
      return json(200, { sessions: [casual, weekly] });
    }
    if (url === "/api/v1/conversation/sessions/s2/messages") {
      return json(200, {
        sessionId: "s2",
        title: "闲聊",
        messages: [
          {
            id: "m-1",
            role: "user",
            body: "今天天气怎么样",
            createdAt: "2026-09-27T08:01:00Z",
          },
          {
            id: "m-2",
            role: "assistant",
            body: "你说的是:「今天天气怎么样」。",
            createdAt: "2026-09-27T08:01:01Z",
          },
        ],
      });
    }
    return undefined;
  });
  render(<ConversationView fetcher={fetcher} />);

  await waitFor(() => expect(screen.getByText("闲聊")).toBeInTheDocument());
  expect(screen.getByText("周报会话")).toBeInTheDocument();

  fireEvent.click(screen.getByRole("button", { name: /闲聊/ }));
  await waitFor(() =>
    expect(
      screen.getByText("你说的是:「今天天气怎么样」。"),
    ).toBeInTheDocument(),
  );
  expect(
    log.some(
      (entry) =>
        entry.url === "/api/v1/conversation/sessions/s2/messages" &&
        entry.method === "GET",
    ),
  ).toBe(true);
});

it("creates a session and binds the panel to it", async () => {
  const { fetcher, log } = fakeFetcher((url, init) => {
    if (url === "/api/v1/conversation/sessions" && init?.method === "GET") {
      return json(200, { sessions: [] });
    }
    if (url === "/api/v1/conversation/sessions" && init?.method === "POST") {
      return json(201, weekly);
    }
    if (url === "/api/v1/conversation/sessions/s1/messages") {
      return json(200, { sessionId: "s1", title: "周报会话", messages: [] });
    }
    return undefined;
  });
  render(<ConversationView fetcher={fetcher} />);

  await waitFor(() =>
    expect(
      screen.getByRole("button", { name: "新建会话" }),
    ).toBeInTheDocument(),
  );
  fireEvent.click(screen.getByRole("button", { name: "新建会话" }));

  await waitFor(() => expect(screen.getByText("周报会话")).toBeInTheDocument());
  expect(
    log.some(
      (entry) =>
        entry.url === "/api/v1/conversation/sessions/s1/messages" &&
        entry.method === "GET",
    ),
  ).toBe(true);
  expect(screen.getByRole("listitem")).toHaveAttribute("aria-current", "true");
});

it("renames a session through the inline editor", async () => {
  const { fetcher, log } = fakeFetcher((url, init) => {
    if (url === "/api/v1/conversation/sessions" && init?.method === "GET") {
      return json(200, { sessions: [weekly] });
    }
    if (
      url === "/api/v1/conversation/sessions/s1" &&
      init?.method === "PATCH"
    ) {
      return json(200, { ...weekly, title: "冲刺计划" });
    }
    return undefined;
  });
  render(<ConversationView fetcher={fetcher} />);

  await waitFor(() => expect(screen.getByText("周报会话")).toBeInTheDocument());
  fireEvent.click(screen.getByRole("button", { name: "重命名" }));
  fireEvent.change(screen.getByLabelText("会话标题"), {
    target: { value: "冲刺计划" },
  });
  fireEvent.click(screen.getByRole("button", { name: "保存" }));

  await waitFor(() => expect(screen.getByText("冲刺计划")).toBeInTheDocument());
  const patch = log.find((entry) => entry.method === "PATCH");
  expect(patch?.url).toBe("/api/v1/conversation/sessions/s1");
  expect(JSON.parse(patch?.body ?? "{}")).toEqual({ title: "冲刺计划" });
});

it("deletes the active session only after confirmation and falls back", async () => {
  const confirm = vi.spyOn(window, "confirm").mockReturnValue(false);
  const { fetcher, log } = fakeFetcher((url, init) => {
    if (url === "/api/v1/conversation/sessions" && init?.method === "GET") {
      return json(200, { sessions: [casual, weekly] });
    }
    if (
      url === "/api/v1/conversation/sessions/s2" &&
      init?.method === "DELETE"
    ) {
      return new Response(null, { status: 204 });
    }
    if (url === "/api/v1/conversation/sessions/s2/messages") {
      return json(200, { sessionId: "s2", title: "闲聊", messages: [] });
    }
    if (url === "/api/v1/conversation/sessions/s1/messages") {
      return json(200, { sessionId: "s1", title: "周报会话", messages: [] });
    }
    return undefined;
  });
  render(<ConversationView fetcher={fetcher} />);

  await waitFor(() => expect(screen.getByText("闲聊")).toBeInTheDocument());
  // Bind the panel to 闲聊 so the delete has to fall back to the next session.
  fireEvent.click(screen.getByRole("button", { name: /闲聊/ }));
  await waitFor(() =>
    expect(
      log.some(
        (entry) =>
          entry.url === "/api/v1/conversation/sessions/s2/messages" &&
          entry.method === "GET",
      ),
    ).toBe(true),
  );
  const deleteButtons = screen.getAllByRole("button", { name: "删除" });
  fireEvent.click(deleteButtons[0]);
  expect(confirm).toHaveBeenCalledWith(
    expect.stringContaining("删除会话「闲聊」"),
  );
  expect(log.some((entry) => entry.method === "DELETE")).toBe(false);

  confirm.mockReturnValue(true);
  fireEvent.click(screen.getAllByRole("button", { name: "删除" })[0]);

  await waitFor(() =>
    expect(screen.queryByText("闲聊")).not.toBeInTheDocument(),
  );
  expect(log.some((entry) => entry.method === "DELETE")).toBe(true);
  // The panel falls back to the first remaining session.
  await waitFor(() =>
    expect(
      log.some(
        (entry) =>
          entry.url === "/api/v1/conversation/sessions/s1/messages" &&
          entry.method === "GET",
      ),
    ).toBe(true),
  );
});

it("refreshes the sidebar without remounting the panel on auto-create", async () => {
  let created = false;
  const { fetcher, log } = fakeFetcher((url, init) => {
    if (url === "/api/v1/conversation/sessions" && init?.method === "GET") {
      return json(200, { sessions: created ? [weekly] : [] });
    }
    if (url === "/api/v1/conversation/messages" && init?.method === "POST") {
      created = true;
      return json(200, {
        kind: "chat",
        correlationId: "c-1",
        reply: "你好!",
        sessionId: "s1",
      });
    }
    return undefined;
  });
  render(<ConversationView fetcher={fetcher} />);

  await waitFor(() =>
    expect(
      screen.getByRole("button", { name: "新建会话" }),
    ).toBeInTheDocument(),
  );
  fireEvent.change(screen.getByLabelText("消息"), {
    target: { value: "你好" },
  });
  fireEvent.click(screen.getByRole("button", { name: "发送" }));

  await waitFor(() => expect(screen.getByText("你好!")).toBeInTheDocument());
  await waitFor(() => expect(screen.getByText("周报会话")).toBeInTheDocument());
  expect(
    log.filter(
      (entry) =>
        entry.url === "/api/v1/conversation/sessions" && entry.method === "GET",
    ),
  ).toHaveLength(2);
  // The live turn survives: no history reload for the auto-created session.
  expect(
    log.some((entry) =>
      entry.url.startsWith("/api/v1/conversation/sessions/s1/messages"),
    ),
  ).toBe(false);
});
