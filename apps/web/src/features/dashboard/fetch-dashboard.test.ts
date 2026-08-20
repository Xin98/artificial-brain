import { describe, expect, it, vi } from "vitest";

import { fetchDashboardSummary } from "./fetch-dashboard";

const validSummary = {
  pendingTotal: 5,
  dueToday: 2,
  overdue: 1,
  noDue: 2,
  completedLast7Days: 3,
  reminderSucceeded: 4,
  reminderRetrying: 1,
  reminderFailed: 2,
  reminderSuppressed: 6,
  checkedAt: "2026-08-19T04:00:00Z",
};

// The ITER-0002 payload shape: the two new reminder counters are missing, so
// the ten-key validator must fail it closed.
const legacySummary = {
  pendingTotal: 5,
  dueToday: 2,
  overdue: 1,
  noDue: 2,
  completedLast7Days: 3,
  reminderRetrying: 1,
  reminderFailed: 2,
  checkedAt: "2026-08-19T04:00:00Z",
};

describe("fetchDashboardSummary", () => {
  it("returns the summary for a valid ten-key payload", async () => {
    const result = await fetchDashboardSummary(
      "",
      vi
        .fn()
        .mockResolvedValue(
          new Response(JSON.stringify(validSummary), { status: 200 }),
        ),
      "Asia/Shanghai",
    );

    expect(result).toEqual(validSummary);
  });

  it("fails closed for the legacy eight-key payload", async () => {
    const result = await fetchDashboardSummary(
      "",
      vi
        .fn()
        .mockResolvedValue(
          new Response(JSON.stringify(legacySummary), { status: 200 }),
        ),
      "Asia/Shanghai",
    );

    expect(result).toBeNull();
  });

  it("fails closed for a non-2xx response", async () => {
    const result = await fetchDashboardSummary(
      "",
      vi.fn().mockResolvedValue(new Response("{}", { status: 500 })),
      "Asia/Shanghai",
    );

    expect(result).toBeNull();
  });

  it("fails closed for a network error", async () => {
    const result = await fetchDashboardSummary(
      "",
      vi.fn().mockRejectedValue(new Error("ECONNREFUSED")),
      "Asia/Shanghai",
    );

    expect(result).toBeNull();
  });

  it("queries the summary endpoint with the encoded timezone", async () => {
    const fetcher = vi
      .fn()
      .mockResolvedValue(
        new Response(JSON.stringify(validSummary), { status: 200 }),
      );

    await fetchDashboardSummary("", fetcher, "Asia/Shanghai");

    expect(fetcher).toHaveBeenCalledWith(
      "/api/v1/dashboard/summary?timezone=" +
        encodeURIComponent("Asia/Shanghai"),
      expect.any(Object),
    );
  });
});
