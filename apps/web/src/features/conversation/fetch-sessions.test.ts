import { afterEach, expect, it, vi } from "vitest";

import {
  createSession,
  deleteSession,
  fetchSessionMessages,
  listSessions,
  renameSession,
} from "./fetch-sessions";

afterEach(() => {
  vi.restoreAllMocks();
});

function json(status: number, body: unknown): Response {
  return new Response(JSON.stringify(body), {
    status,
    headers: { "content-type": "application/json" },
  });
}

const sessionView = {
  id: "sess-1",
  title: "周报会话",
  createdAt: "2026-09-27T08:00:00Z",
  updatedAt: "2026-09-27T08:05:00Z",
};

it("lists sessions and strictly validates the envelope", async () => {
  const fetcher = vi
    .fn()
    .mockResolvedValue(json(200, { sessions: [sessionView] }));

  await expect(
    listSessions("", fetcher as unknown as typeof fetch),
  ).resolves.toEqual({ ok: true, sessions: [sessionView] });
  expect(fetcher.mock.calls[0]?.[1]?.method).toBe("GET");

  const unknownField = vi
    .fn()
    .mockResolvedValue(
      json(200, { sessions: [{ ...sessionView, archived: true }] }),
    );
  await expect(
    listSessions("", unknownField as unknown as typeof fetch),
  ).resolves.toEqual({ ok: false, reason: "invalid_response" });

  const notAnArray = vi.fn().mockResolvedValue(json(200, { sessions: {} }));
  await expect(
    listSessions("", notAnArray as unknown as typeof fetch),
  ).resolves.toEqual({ ok: false, reason: "invalid_response" });
});

it("creates a session with or without a title", async () => {
  const fetcher = vi.fn().mockResolvedValue(json(201, sessionView));

  await expect(
    createSession("", fetcher as unknown as typeof fetch, "周报会话"),
  ).resolves.toEqual({ ok: true, session: sessionView });
  expect(JSON.parse(String(fetcher.mock.calls[0]?.[1]?.body))).toEqual({
    title: "周报会话",
  });

  await createSession("", fetcher as unknown as typeof fetch);
  expect(String(fetcher.mock.calls[1]?.[1]?.body)).toBe("{}");
});

it("maps create/rename validation failures to rejected", async () => {
  const fetcher = vi.fn().mockResolvedValue(
    json(422, {
      code: "validation_error",
      message: "x",
      correlationId: "c-1",
    }),
  );

  await expect(
    renameSession("", fetcher as unknown as typeof fetch, "sess-1", ""),
  ).resolves.toEqual({ ok: false, reason: "rejected", correlationId: "c-1" });
  expect(JSON.parse(String(fetcher.mock.calls[0]?.[1]?.body))).toEqual({
    title: "",
  });
  expect(fetcher.mock.calls[0]?.[1]?.method).toBe("PATCH");
});

it("renames a session and maps an unknown id to not_found", async () => {
  const fetcher = vi
    .fn()
    .mockResolvedValue(json(200, { ...sessionView, title: "冲刺计划" }));
  await expect(
    renameSession("", fetcher as unknown as typeof fetch, "sess-1", "冲刺计划"),
  ).resolves.toEqual({
    ok: true,
    session: { ...sessionView, title: "冲刺计划" },
  });

  const missing = vi.fn().mockResolvedValue(
    json(404, {
      code: "session_not_found",
      message: "session not found",
      correlationId: "c-2",
    }),
  );
  await expect(
    renameSession("", missing as unknown as typeof fetch, "gone", "x"),
  ).resolves.toEqual({ ok: false, reason: "not_found", correlationId: "c-2" });
});

it("deletes a session on 204 and rejects any body-bearing success", async () => {
  const fetcher = vi
    .fn()
    .mockResolvedValue(new Response(null, { status: 204 }));
  await expect(
    deleteSession("", fetcher as unknown as typeof fetch, "sess-1"),
  ).resolves.toEqual({ ok: true });
  expect(fetcher.mock.calls[0]?.[1]?.method).toBe("DELETE");

  const withBody = vi.fn().mockResolvedValue(json(200, sessionView));
  await expect(
    deleteSession("", withBody as unknown as typeof fetch, "sess-1"),
  ).resolves.toEqual({ ok: false, reason: "invalid_response" });

  const missing = vi.fn().mockResolvedValue(
    json(404, {
      code: "session_not_found",
      message: "x",
      correlationId: "c",
    }),
  );
  await expect(
    deleteSession("", missing as unknown as typeof fetch, "gone"),
  ).resolves.toEqual({ ok: false, reason: "not_found", correlationId: "c" });
});

it("loads session history with strict message validation", async () => {
  const messages = [
    {
      id: "m-1",
      role: "user",
      body: "明天下午三点提醒我提交周报",
      resolvedIntent: "todo.create",
      createdAt: "2026-09-27T08:01:00Z",
    },
    {
      id: "m-2",
      role: "assistant",
      body: "已创建待办「提交周报」。",
      createdAt: "2026-09-27T08:01:01Z",
    },
  ];
  const fetcher = vi
    .fn()
    .mockResolvedValue(
      json(200, { sessionId: "sess-1", title: "周报会话", messages }),
    );

  await expect(
    fetchSessionMessages("", fetcher as unknown as typeof fetch, "sess-1"),
  ).resolves.toEqual({
    ok: true,
    history: { sessionId: "sess-1", title: "周报会话", messages },
  });

  const badRole = vi.fn().mockResolvedValue(
    json(200, {
      sessionId: "sess-1",
      title: "周报会话",
      messages: [{ ...messages[0], role: "system" }],
    }),
  );
  await expect(
    fetchSessionMessages("", badRole as unknown as typeof fetch, "sess-1"),
  ).resolves.toEqual({ ok: false, reason: "invalid_response" });

  const missing = vi.fn().mockResolvedValue(
    json(404, {
      code: "session_not_found",
      message: "x",
      correlationId: "c",
    }),
  );
  await expect(
    fetchSessionMessages("", missing as unknown as typeof fetch, "gone"),
  ).resolves.toEqual({ ok: false, reason: "not_found", correlationId: "c" });
});

it("classifies auth, server, timeout and network failures", async () => {
  const unauthenticated = vi
    .fn()
    .mockResolvedValue(
      json(401, { code: "unauthenticated", message: "x", correlationId: "c" }),
    );
  await expect(
    listSessions("", unauthenticated as unknown as typeof fetch),
  ).resolves.toEqual({
    ok: false,
    reason: "unauthenticated",
    correlationId: "c",
  });

  const server = vi.fn().mockResolvedValue(
    json(503, {
      code: "internal_error",
      message: "sensitive",
      correlationId: "c",
    }),
  );
  const result = await listSessions("", server as unknown as typeof fetch);
  expect(result).toEqual({ ok: false, reason: "server", correlationId: "c" });
  expect(JSON.stringify(result)).not.toContain("sensitive");

  const timeoutFetcher = vi
    .fn()
    .mockRejectedValue(new DOMException("deadline", "TimeoutError"));
  await expect(
    listSessions("", timeoutFetcher as unknown as typeof fetch),
  ).resolves.toEqual({ ok: false, reason: "timeout" });

  const networkFetcher = vi.fn().mockRejectedValue(new TypeError("offline"));
  await expect(
    listSessions("", networkFetcher as unknown as typeof fetch),
  ).resolves.toEqual({ ok: false, reason: "network" });
});

it("keeps the session budget below the model deadline", async () => {
  const signal = new AbortController().signal;
  const timeout = vi.spyOn(AbortSignal, "timeout").mockReturnValue(signal);
  const fetcher = vi.fn().mockResolvedValue(json(200, { sessions: [] }));

  await listSessions("", fetcher as unknown as typeof fetch);

  expect(timeout).toHaveBeenCalledWith(10_000);
});
