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

it.each([
  { kind: "todo_created", correlationId: "corr-malformed" },
  { kind: "candidates", correlationId: "corr-malformed" },
  { kind: "confirmation_required", correlationId: "corr-malformed" },
  { kind: "todo_deleted", correlationId: "corr-malformed" },
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
