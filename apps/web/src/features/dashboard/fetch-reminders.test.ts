import { describe, expect, it, vi } from "vitest";

import { fetchReminderDeliveries } from "./fetch-reminders";

const minimalDelivery = {
  id: "rd_01",
  todoId: "todo_01",
  todoTitle: "每日站会",
  channel: "email",
  state: "succeeded",
  attemptCount: 1,
  scheduledAt: "2026-08-19T01:00:00Z",
  createdAt: "2026-08-18T12:00:00Z",
};

const suppressedDelivery = {
  ...minimalDelivery,
  id: "rd_02",
  channel: "sms",
  state: "suppressed",
  attemptCount: 0,
  suppressionReason: "todo_completed",
};

const receiptedDelivery = {
  ...minimalDelivery,
  id: "rd_03",
  channel: "sms",
  state: "succeeded",
  attemptCount: 2,
  providerMessageId: "sms-provider-42",
  lastErrorCode: "isv.BUSINESS_LIMIT_CONTROL",
  submittedAt: "2026-08-19T01:00:01Z",
  finalizedAt: "2026-08-19T01:00:02Z",
  receiptState: "received_ok",
  receiptAt: "2026-08-19T01:00:03Z",
  receiptErrorCode: "",
};

function jsonResponse(body: unknown): Response {
  return new Response(JSON.stringify(body), {
    status: 200,
    headers: { "content-type": "application/json" },
  });
}

describe("fetchReminderDeliveries", () => {
  it("returns deliveries for a valid list", async () => {
    const result = await fetchReminderDeliveries(
      "",
      vi.fn().mockResolvedValue(
        jsonResponse({
          deliveries: [minimalDelivery, suppressedDelivery, receiptedDelivery],
        }),
      ),
    );

    expect(result).toEqual([
      minimalDelivery,
      suppressedDelivery,
      receiptedDelivery,
    ]);
  });

  it("returns an empty list when the workspace has no deliveries", async () => {
    const result = await fetchReminderDeliveries(
      "",
      vi.fn().mockResolvedValue(jsonResponse({ deliveries: [] })),
    );

    expect(result).toEqual([]);
  });

  it("queries the reminders endpoint on the base URL", async () => {
    const fetcher = vi.fn().mockResolvedValue(jsonResponse({ deliveries: [] }));

    await fetchReminderDeliveries("http://api.internal:8080", fetcher);

    expect(fetcher).toHaveBeenCalledWith(
      "http://api.internal:8080/api/v1/reminders",
      expect.any(Object),
    );
  });

  it("fails closed when a required field is missing", async () => {
    const { todoTitle: _todoTitle, ...missingRequired } = minimalDelivery;
    const result = await fetchReminderDeliveries(
      "",
      vi
        .fn()
        .mockResolvedValue(jsonResponse({ deliveries: [missingRequired] })),
    );

    expect(result).toBeNull();
  });

  it("fails closed when a record carries an unexpected key", async () => {
    const result = await fetchReminderDeliveries(
      "",
      vi.fn().mockResolvedValue(
        jsonResponse({
          deliveries: [{ ...minimalDelivery, extraField: true }],
        }),
      ),
    );

    expect(result).toBeNull();
  });

  it.each(["mail", "push", ""])(
    "fails closed for an unknown channel %j",
    async (channel) => {
      const result = await fetchReminderDeliveries(
        "",
        vi
          .fn()
          .mockResolvedValue(
            jsonResponse({ deliveries: [{ ...minimalDelivery, channel }] }),
          ),
      );

      expect(result).toBeNull();
    },
  );

  it.each(["sent", "retrying", ""])(
    "fails closed for an unknown state %j",
    async (state) => {
      const result = await fetchReminderDeliveries(
        "",
        vi
          .fn()
          .mockResolvedValue(
            jsonResponse({ deliveries: [{ ...minimalDelivery, state }] }),
          ),
      );

      expect(result).toBeNull();
    },
  );

  it("fails closed for an unknown receiptState", async () => {
    const result = await fetchReminderDeliveries(
      "",
      vi.fn().mockResolvedValue(
        jsonResponse({
          deliveries: [{ ...minimalDelivery, receiptState: "delivered" }],
        }),
      ),
    );

    expect(result).toBeNull();
  });

  it("fails closed for an unknown suppressionReason", async () => {
    const result = await fetchReminderDeliveries(
      "",
      vi.fn().mockResolvedValue(
        jsonResponse({
          deliveries: [
            { ...minimalDelivery, suppressionReason: "user_opted_out" },
          ],
        }),
      ),
    );

    expect(result).toBeNull();
  });

  it("fails closed for a malformed timestamp", async () => {
    const result = await fetchReminderDeliveries(
      "",
      vi.fn().mockResolvedValue(
        jsonResponse({
          deliveries: [
            { ...minimalDelivery, scheduledAt: "2026-13-01T00:00:00Z" },
          ],
        }),
      ),
    );

    expect(result).toBeNull();
  });

  it("fails closed when the wrapper is a bare array", async () => {
    const result = await fetchReminderDeliveries(
      "",
      vi.fn().mockResolvedValue(jsonResponse([minimalDelivery])),
    );

    expect(result).toBeNull();
  });

  it("fails closed for a non-2xx response", async () => {
    const result = await fetchReminderDeliveries(
      "",
      vi.fn().mockResolvedValue(new Response("{}", { status: 500 })),
    );

    expect(result).toBeNull();
  });

  it("fails closed for a network error", async () => {
    const result = await fetchReminderDeliveries(
      "",
      vi.fn().mockRejectedValue(new Error("ECONNREFUSED")),
    );

    expect(result).toBeNull();
  });

  it("fails closed when the request times out", async () => {
    const fetcher = vi.fn(
      (_input: RequestInfo | URL, init?: RequestInit): Promise<Response> =>
        new Promise((_, reject) => {
          init?.signal?.addEventListener("abort", () =>
            reject(new DOMException("Aborted", "AbortError")),
          );
        }),
    );

    const result = await fetchReminderDeliveries("", fetcher, 5);

    expect(result).toBeNull();
  }, 250);
});
