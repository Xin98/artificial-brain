import { afterEach, expect, it, vi } from "vitest";

import { postConversationMessage } from "./fetch-conversation";

afterEach(() => {
  vi.restoreAllMocks();
});

function json(status: number, body: unknown): Response {
  return new Response(JSON.stringify(body), {
    status,
    headers: { "content-type": "application/json" },
  });
}

it("keeps the default browser deadline beyond the private model deadline", async () => {
  const signal = new AbortController().signal;
  const timeout = vi.spyOn(AbortSignal, "timeout").mockReturnValue(signal);
  const fetcher = vi
    .fn()
    .mockResolvedValue(
      new Response(
        JSON.stringify({ kind: "unsupported", correlationId: "corr-1" }),
        { status: 200, headers: { "content-type": "application/json" } },
      ),
    );

  await postConversationMessage(
    "",
    fetcher as unknown as typeof fetch,
    "你好",
    "Asia/Shanghai",
  );

  expect(timeout).toHaveBeenCalledWith(35_000);
  expect(fetcher.mock.calls[0]?.[1]?.signal).toBe(signal);
});

it.each([
  [401, "unauthenticated"],
  [429, "rate_limited"],
  [503, "server"],
] as const)(
  "classifies HTTP %s without exposing the server message",
  async (status, reason) => {
    const fetcher = vi.fn().mockResolvedValue(
      json(status, {
        code: "internal_error",
        message: "sensitive upstream detail",
        correlationId: "corr-failure",
      }),
    );

    const result = await postConversationMessage(
      "",
      fetcher as unknown as typeof fetch,
      "你好",
      "Asia/Shanghai",
    );

    expect(result).toEqual({
      ok: false,
      reason,
      correlationId: "corr-failure",
    });
    expect(JSON.stringify(result)).not.toContain("sensitive upstream detail");
  },
);

it("distinguishes a browser deadline from other network failures", async () => {
  const timeoutFetcher = vi
    .fn()
    .mockRejectedValue(new DOMException("deadline", "TimeoutError"));
  const networkFetcher = vi.fn().mockRejectedValue(new TypeError("offline"));

  await expect(
    postConversationMessage(
      "",
      timeoutFetcher as unknown as typeof fetch,
      "你好",
      "Asia/Shanghai",
    ),
  ).resolves.toEqual({ ok: false, reason: "timeout" });
  await expect(
    postConversationMessage(
      "",
      networkFetcher as unknown as typeof fetch,
      "你好",
      "Asia/Shanghai",
    ),
  ).resolves.toEqual({ ok: false, reason: "network" });
});

it("accepts a chat turn carrying the model reply and session id", async () => {
  const fetcher = vi.fn().mockResolvedValue(
    json(200, {
      kind: "chat",
      correlationId: "corr-chat",
      reply: "你说的是：「今天天气怎么样」。这个我暂时不能直接执行。",
      sessionId: "sess-1",
    }),
  );

  await expect(
    postConversationMessage(
      "",
      fetcher as unknown as typeof fetch,
      "今天天气怎么样",
      "Asia/Shanghai",
      "sess-1",
    ),
  ).resolves.toEqual({
    ok: true,
    response: {
      kind: "chat",
      correlationId: "corr-chat",
      reply: "你说的是：「今天天气怎么样」。这个我暂时不能直接执行。",
      sessionId: "sess-1",
    },
  });
});

it("accepts a clarification that carries an optional model reply", async () => {
  const fetcher = vi.fn().mockResolvedValue(
    json(200, {
      kind: "clarification",
      correlationId: "corr-clarify",
      missingFields: ["due_at"],
      reply: "好的，请问「提交周报」要在什么时间提醒？",
    }),
  );

  await expect(
    postConversationMessage(
      "",
      fetcher as unknown as typeof fetch,
      "提醒我提交周报",
      "Asia/Shanghai",
    ),
  ).resolves.toMatchObject({ ok: true });
});

it("includes sessionId in the body only when provided", async () => {
  const fetcher = vi
    .fn()
    .mockResolvedValue(
      json(200, { kind: "chat", correlationId: "c", reply: "r" }),
    );

  await postConversationMessage(
    "",
    fetcher as unknown as typeof fetch,
    "你好",
    "Asia/Shanghai",
    "sess-9",
  );
  expect(JSON.parse(String(fetcher.mock.calls[0]?.[1]?.body))).toEqual({
    text: "你好",
    timezone: "Asia/Shanghai",
    sessionId: "sess-9",
  });

  await postConversationMessage(
    "",
    fetcher as unknown as typeof fetch,
    "你好",
    "Asia/Shanghai",
  );
  expect(JSON.parse(String(fetcher.mock.calls[1]?.[1]?.body))).toEqual({
    text: "你好",
    timezone: "Asia/Shanghai",
  });
});

it.each([
  { kind: "todo_created", correlationId: "corr-malformed" },
  { kind: "candidates", correlationId: "corr-malformed" },
  { kind: "confirmation_required", correlationId: "corr-malformed" },
  { kind: "todo_deleted", correlationId: "corr-malformed" },
  { kind: "chat", correlationId: "corr-malformed" },
  { kind: "chat", correlationId: "corr-malformed", reply: "" },
])(
  "rejects a $kind success payload missing kind-specific fields",
  async (body) => {
    const fetcher = vi.fn().mockResolvedValue(json(200, body));

    await expect(
      postConversationMessage(
        "",
        fetcher as unknown as typeof fetch,
        "你好",
        "Asia/Shanghai",
      ),
    ).resolves.toEqual({ ok: false, reason: "invalid_response" });
  },
);

it("rejects fields that belong to a different response kind", async () => {
  const fetcher = vi.fn().mockResolvedValue(
    json(200, {
      kind: "unsupported",
      correlationId: "corr-cross-kind",
      confirmationId: "conf-should-not-be-here",
      reply: "reply is not allowed on unsupported",
    }),
  );

  await expect(
    postConversationMessage(
      "",
      fetcher as unknown as typeof fetch,
      "你好",
      "Asia/Shanghai",
    ),
  ).resolves.toEqual({ ok: false, reason: "invalid_response" });
});

it("accepts an empty todo list when the omitted todos field is absent", async () => {
  const fetcher = vi.fn().mockResolvedValue(
    json(200, {
      kind: "todo_list",
      correlationId: "corr-empty-list",
    }),
  );

  await expect(
    postConversationMessage(
      "",
      fetcher as unknown as typeof fetch,
      "列出待办",
      "Asia/Shanghai",
    ),
  ).resolves.toEqual({
    ok: true,
    response: {
      kind: "todo_list",
      correlationId: "corr-empty-list",
    },
  });
});
