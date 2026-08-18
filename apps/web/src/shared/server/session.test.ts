import { describe, expect, it, vi } from "vitest";

import { authHeaders, fetchSession, SESSION_COOKIE_NAME } from "./session";

const validBody = {
  userId: "user-1",
  workspaceId: "ws-1",
  sessionId: "session-1",
};

function jsonResponse(status: number, body: unknown): Response {
  return new Response(JSON.stringify(body), {
    status,
    headers: { "content-type": "application/json" },
  });
}

describe("authHeaders", () => {
  it("carries only the session cookie", () => {
    expect(authHeaders("token-1")).toEqual({
      cookie: `${SESSION_COOKIE_NAME}=token-1`,
    });
  });
});

describe("fetchSession", () => {
  it("returns the session context for a valid response", async () => {
    const fetcher = vi.fn(async (_input: string, _init?: RequestInit) =>
      jsonResponse(200, validBody),
    );

    const session = await fetchSession(
      "http://internal:8080",
      fetcher as unknown as typeof fetch,
      "token-1",
    );

    expect(session).toEqual(validBody);
    const [url, init] = fetcher.mock.calls[0];
    expect(url).toBe("http://internal:8080/api/v1/auth/session");
    expect(init?.headers).toMatchObject(authHeaders("token-1"));
    expect(init?.cache).toBe("no-store");
  });

  it("returns null when the API rejects the session", async () => {
    const fetcher = vi.fn(async () => jsonResponse(401, {}));

    const session = await fetchSession(
      "http://internal:8080",
      fetcher as unknown as typeof fetch,
      "token-1",
    );

    expect(session).toBeNull();
  });

  it("returns null for malformed payloads", async () => {
    const payloads = [
      { ...validBody, bogus: true },
      { userId: "user-1", workspaceId: "ws-1" },
      { userId: 1, workspaceId: "ws-1", sessionId: "session-1" },
      { userId: "", workspaceId: "ws-1", sessionId: "session-1" },
      [validBody],
    ];
    for (const payload of payloads) {
      const fetcher = vi.fn(async () => jsonResponse(200, payload));
      const session = await fetchSession(
        "http://internal:8080",
        fetcher as unknown as typeof fetch,
        "token-1",
      );
      expect(session, JSON.stringify(payload)).toBeNull();
    }
  });

  it("returns null when the request times out", async () => {
    const fetcher = vi.fn(async () => {
      throw new DOMException("Aborted", "AbortError");
    });

    const session = await fetchSession(
      "http://internal:8080",
      fetcher as unknown as typeof fetch,
      "token-1",
      10,
    );

    expect(session).toBeNull();
  });

  it("returns null when the response body is not JSON", async () => {
    const fetcher = vi.fn(
      async () =>
        new Response("not json", {
          status: 200,
          headers: { "content-type": "text/plain" },
        }),
    );

    const session = await fetchSession(
      "http://internal:8080",
      fetcher as unknown as typeof fetch,
      "token-1",
    );

    expect(session).toBeNull();
  });
});
