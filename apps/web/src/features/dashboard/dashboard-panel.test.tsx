import { fireEvent, render, screen, waitFor } from "@testing-library/react";
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

const failedDelivery = {
  ...delivery,
  id: "rd_02",
  todoId: "todo_02",
  todoTitle: "发送周报",
  state: "failed",
  attemptCount: 3,
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

it("refetches reminder records by the selected status and resets to all records", async () => {
  const fetcher = vi.fn(async (input: RequestInfo | URL): Promise<Response> => {
    const url = String(input);
    if (url.startsWith("/api/v1/dashboard/summary")) {
      return jsonResponse(summary);
    }
    if (url === "/api/v1/reminders?status=failed") {
      return jsonResponse({ deliveries: [failedDelivery] });
    }
    if (url === "/api/v1/reminders") {
      return jsonResponse({ deliveries: [delivery] });
    }
    return new Response("{}", { status: 404 });
  });
  render(<DashboardPanel fetcher={fetcher as unknown as typeof fetch} />);

  await waitFor(() =>
    expect(screen.getByText("《每日站会》")).toBeInTheDocument(),
  );

  fireEvent.click(screen.getByRole("button", { name: /失败/ }));

  await waitFor(() =>
    expect(fetcher).toHaveBeenCalledWith(
      "/api/v1/reminders?status=failed",
      expect.any(Object),
    ),
  );
  await waitFor(() =>
    expect(screen.getByText("《发送周报》")).toBeInTheDocument(),
  );
  expect(screen.queryByText("《每日站会》")).not.toBeInTheDocument();
  expect(screen.getByRole("button", { name: /失败/ })).toHaveAttribute(
    "aria-pressed",
    "true",
  );

  fireEvent.click(screen.getByRole("button", { name: "全部提醒" }));

  await waitFor(() => {
    const unfilteredCalls = fetcher.mock.calls.filter(
      ([input]) => String(input) === "/api/v1/reminders",
    );
    expect(unfilteredCalls).toHaveLength(2);
  });
  await waitFor(() =>
    expect(screen.getByText("《每日站会》")).toBeInTheDocument(),
  );
});

it("keeps the controlled records region mounted while a filter is loading", async () => {
  let resolveReminders: ((response: Response) => void) | undefined;
  const reminders = new Promise<Response>((resolve) => {
    resolveReminders = resolve;
  });
  const fetcher = vi.fn((input: RequestInfo | URL): Promise<Response> => {
    if (String(input).startsWith("/api/v1/dashboard/summary")) {
      return Promise.resolve(jsonResponse(summary));
    }
    return reminders;
  });
  render(<DashboardPanel fetcher={fetcher as unknown as typeof fetch} />);

  await waitFor(() =>
    expect(screen.getByRole("button", { name: /失败/ })).toBeInTheDocument(),
  );
  const records = screen.getByLabelText("提醒记录");
  expect(records).toHaveAttribute("id", "reminder-records");
  expect(screen.getByRole("status")).toHaveTextContent("提醒记录加载中");

  resolveReminders?.(jsonResponse({ deliveries: [delivery] }));
  await waitFor(() =>
    expect(screen.getByText("《每日站会》")).toBeInTheDocument(),
  );
});

it("retries a failed selected reminder filter when its card is clicked again", async () => {
  const fetcher = vi.fn(async (input: RequestInfo | URL): Promise<Response> => {
    const url = String(input);
    if (url.startsWith("/api/v1/dashboard/summary")) {
      return jsonResponse(summary);
    }
    if (url === "/api/v1/reminders") {
      return jsonResponse({ deliveries: [delivery] });
    }
    if (url === "/api/v1/reminders?status=failed") {
      return new Response("{}", { status: 500 });
    }
    return new Response("{}", { status: 404 });
  });
  render(<DashboardPanel fetcher={fetcher as unknown as typeof fetch} />);

  await waitFor(() =>
    expect(screen.getByText("《每日站会》")).toBeInTheDocument(),
  );
  const failed = screen.getByRole("button", { name: /失败/ });
  fireEvent.click(failed);
  await waitFor(() =>
    expect(screen.getByRole("status")).toHaveTextContent("提醒记录暂时不可用"),
  );

  fireEvent.click(failed);
  await waitFor(() => {
    const filteredCalls = fetcher.mock.calls.filter(
      ([input]) => String(input) === "/api/v1/reminders?status=failed",
    );
    expect(filteredCalls).toHaveLength(2);
  });
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
