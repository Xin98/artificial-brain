import { describe, expect, it, vi } from "vitest";

import { fetchSystemHealth } from "./fetch-system-health";

const validPayload = {
  status: "healthy",
  checkedAt: "2026-08-13T04:00:00Z",
  correlationId: "01hzzzzzzzzzzzzzzzzzzzzzzz",
  components: {
    api: { status: "healthy", checkedAt: "2026-08-13T04:00:00Z" },
    database: { status: "healthy", checkedAt: "2026-08-13T04:00:00Z" },
    worker: { status: "healthy", checkedAt: "2026-08-13T04:00:00Z" },
  },
};

describe("fetchSystemHealth", () => {
  it("returns a complete valid API health report", async () => {
    const result = await fetchSystemHealth(
      "http://api.internal:8080",
      vi.fn().mockResolvedValue(new Response(JSON.stringify(validPayload))),
    );

    expect(result).toEqual(validPayload);
  });

  it.each([
    ["a non-success response", () => new Response("nope", { status: 503 })],
    ["malformed JSON", () => new Response("{")],
    [
      "an aborted request",
      () => Promise.reject(new DOMException("Aborted", "AbortError")),
    ],
    [
      "a raw network error",
      () => Promise.reject(new Error("ECONNREFUSED database-password")),
    ],
  ])(
    "returns a redacted unavailable report for %s",
    async (_description, response) => {
      const result = await fetchSystemHealth(
        "http://api.internal:8080",
        vi.fn().mockImplementation(response),
      );

      expect(result.status).toBe("unavailable");
      expect(result.components.api.status).toBe("unavailable");
      expect(JSON.stringify(result)).not.toContain("ECONNREFUSED");
      expect(JSON.stringify(result)).not.toContain("database-password");
    },
  );

  it("rejects malformed contract fields with an unavailable report", async () => {
    const result = await fetchSystemHealth(
      "http://api.internal:8080",
      vi
        .fn()
        .mockResolvedValue(
          new Response(
            JSON.stringify({ ...validPayload, checkedAt: "not-a-timestamp" }),
          ),
        ),
    );

    expect(result.status).toBe("unavailable");
  });

  it.each([
    "2026-02-30T04:00:00Z",
    "2026-13-01T04:00:00Z",
    "2026-08-13T24:00:00Z",
    "2026-08-13T04:00:00+12:60",
  ])("rejects an impossible RFC3339 timestamp: %s", async (checkedAt) => {
    const result = await fetchSystemHealth(
      "http://api.internal:8080",
      vi
        .fn()
        .mockResolvedValue(
          new Response(JSON.stringify({ ...validPayload, checkedAt })),
        ),
    );

    expect(result.status).toBe("unavailable");
  });

  it("uses the API health path when the base URL has a path, query, and fragment", async () => {
    const fetcher = vi
      .fn()
      .mockResolvedValue(new Response(JSON.stringify(validPayload)));

    await fetchSystemHealth(
      "https://api.example.test/prefix?old=query#fragment",
      fetcher,
    );

    expect(fetcher).toHaveBeenCalledWith(
      "https://api.example.test/api/v1/system/health",
      expect.any(Object),
    );
  });

  it("returns an unavailable report for an invalid base URL", async () => {
    const result = await fetchSystemHealth("not an absolute URL", vi.fn());

    expect(result.status).toBe("unavailable");
  });

  it("uses the timeout signal to return an unavailable report", async () => {
    const fetcher = vi.fn(
      (_input: RequestInfo | URL, init?: RequestInit): Promise<Response> =>
        new Promise((_, reject) => {
          init?.signal?.addEventListener("abort", () =>
            reject(new DOMException("Aborted", "AbortError")),
          );
        }),
    );

    const result = await fetchSystemHealth(
      "http://api.internal:8080",
      fetcher,
      5,
    );

    expect(result.status).toBe("unavailable");
    expect(result.components.api.status).toBe("unavailable");
  }, 250);
});
