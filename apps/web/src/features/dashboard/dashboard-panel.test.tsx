import { render, screen, waitFor } from "@testing-library/react";
import { expect, it, vi } from "vitest";

import { DashboardPanel } from "./dashboard-panel";

const summary = {
  pendingTotal: 1,
  dueToday: 1,
  overdue: 0,
  noDue: 0,
  completedLast7Days: 0,
  reminderSucceeded: 2,
  reminderRetrying: 0,
  reminderFailed: 0,
  reminderSuppressed: 1,
  checkedAt: "2026-08-18T12:00:00Z",
};

const delivery = {
  id: "rd_01",
  todoId: "todo_01",
  todoTitle: "每日站会",
  channel: "email",
  state: "succeeded",
  attemptCount: 1,
  scheduledAt: "2026-08-19T01:00:00Z",
  createdAt: "2026-08-18T12:00:00Z",
};

function jsonResponse(body: unknown): Response {
  return new Response(JSON.stringify(body), {
    status: 200,
    headers: { "content-type": "application/json" },
  });
}

// Routes each request to its endpoint response; pass "fail" to answer with a
// 500 and exercise the fail-closed / degraded paths.
function routingFetcher(
  summaryBody: unknown | "fail",
  remindersBody: unknown | "fail",
) {
  return vi.fn(async (input: RequestInfo | URL): Promise<Response> => {
    const url = String(input);
    if (url.startsWith("/api/v1/dashboard/summary")) {
      return summaryBody === "fail"
        ? new Response("{}", { status: 500 })
        : jsonResponse(summaryBody);
    }
    if (url === "/api/v1/reminders") {
      return remindersBody === "fail"
        ? new Response("{}", { status: 500 })
        : jsonResponse(remindersBody);
    }
    return new Response("{}", { status: 404 });
  });
}

it("fetches with the browser timezone and renders tiles and records", async () => {
  const fetcher = routingFetcher(summary, { deliveries: [delivery] });
  render(
    <DashboardPanel
      fetcher={fetcher as unknown as typeof fetch}
      timezoneProvider={() => "Asia/Shanghai"}
    />,
  );

  await waitFor(() => expect(screen.getByText("待处理")).toBeInTheDocument());
  await waitFor(() =>
    expect(screen.getByText("《每日站会》")).toBeInTheDocument(),
  );
  expect(fetcher).toHaveBeenCalledWith(
    "/api/v1/dashboard/summary?timezone=" + encodeURIComponent("Asia/Shanghai"),
    expect.any(Object),
  );
  expect(fetcher).toHaveBeenCalledWith("/api/v1/reminders", expect.any(Object));
});

it("shows the summary tiles and the empty records state when no deliveries exist", async () => {
  const fetcher = routingFetcher(summary, { deliveries: [] });
  render(<DashboardPanel fetcher={fetcher as unknown as typeof fetch} />);

  await waitFor(() =>
    expect(screen.getByText("暂无提醒记录")).toBeInTheDocument(),
  );
  expect(screen.getByText("提醒成功").parentElement).toHaveTextContent("2");
});

it("degrades to the summary view with a note when the records fail", async () => {
  const fetcher = routingFetcher(summary, "fail");
  render(<DashboardPanel fetcher={fetcher as unknown as typeof fetch} />);

  await waitFor(() => expect(screen.getByText("待处理")).toBeInTheDocument());
  expect(screen.getByText("提醒成功").parentElement).toHaveTextContent("2");
  await waitFor(() =>
    expect(screen.getByRole("status")).toHaveTextContent("提醒记录暂时不可用"),
  );
  expect(screen.queryByRole("alert")).not.toBeInTheDocument();
});

it("fails closed when the dashboard summary cannot load", async () => {
  const fetcher = routingFetcher("fail", { deliveries: [delivery] });
  render(<DashboardPanel fetcher={fetcher as unknown as typeof fetch} />);

  await waitFor(() =>
    expect(screen.getByRole("alert")).toHaveTextContent("仪表盘暂时不可用"),
  );
});
