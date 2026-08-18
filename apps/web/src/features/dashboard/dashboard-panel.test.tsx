import { render, screen, waitFor } from "@testing-library/react";
import { expect, it, vi } from "vitest";

import { DashboardPanel } from "./dashboard-panel";

const summary = {
  pendingTotal: 1,
  dueToday: 1,
  overdue: 0,
  noDue: 0,
  completedLast7Days: 0,
  reminderRetrying: 0,
  reminderFailed: 0,
  checkedAt: "2026-08-18T12:00:00Z",
};

it("fetches with the browser timezone and renders tiles", async () => {
  const fetcher = vi.fn().mockResolvedValue(
    new Response(JSON.stringify(summary), {
      status: 200,
      headers: { "content-type": "application/json" },
    }),
  );
  render(
    <DashboardPanel
      fetcher={fetcher as unknown as typeof fetch}
      timezoneProvider={() => "Asia/Shanghai"}
    />,
  );

  await waitFor(() => expect(screen.getByText("待处理")).toBeInTheDocument());
  const url = String(fetcher.mock.calls[0][0]);
  expect(url).toBe(
    "/api/v1/dashboard/summary?timezone=" + encodeURIComponent("Asia/Shanghai"),
  );
});

it("fails closed when the dashboard cannot load", async () => {
  const fetcher = vi
    .fn()
    .mockResolvedValue(new Response("{}", { status: 500 }));
  render(<DashboardPanel fetcher={fetcher as unknown as typeof fetch} />);

  await waitFor(() =>
    expect(screen.getByRole("alert")).toHaveTextContent("仪表盘暂时不可用"),
  );
});
