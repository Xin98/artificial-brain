import { fireEvent, render, screen } from "@testing-library/react";
import { expect, it, vi } from "vitest";

import type { DashboardSummary } from "./fetch-dashboard";
import type { ReminderDelivery } from "./fetch-reminders";
import { DashboardView } from "./dashboard-view";

const summary: DashboardSummary = {
  pendingTotal: 5,
  dueToday: 2,
  overdue: 1,
  noDue: 2,
  completedLast7Days: 3,
  reminderSucceeded: 4,
  reminderRetrying: 0,
  reminderFailed: 0,
  reminderSuppressed: 6,
  checkedAt: "2026-08-18T12:00:00Z",
};

const records: ReminderDelivery[] = [
  {
    id: "rd_01",
    todoId: "todo_01",
    todoTitle: "每日站会",
    channel: "email",
    state: "succeeded",
    attemptCount: 1,
    scheduledAt: "2026-08-19T01:00:00Z",
    createdAt: "2026-08-18T12:00:00Z",
  },
  {
    id: "rd_02",
    todoId: "todo_02",
    todoTitle: "周报",
    channel: "sms",
    state: "failed",
    attemptCount: 3,
    scheduledAt: "2026-08-19T02:00:00Z",
    createdAt: "2026-08-18T12:00:00Z",
    receiptState: "received_failed",
  },
];

it("renders the nine deterministic stat tiles", () => {
  render(<DashboardView summary={summary} />);

  expect(screen.getByText("待处理").parentElement).toHaveTextContent("5");
  expect(screen.getByText("今日到期").parentElement).toHaveTextContent("2");
  expect(screen.getByText("已逾期").parentElement).toHaveTextContent("1");
  expect(screen.getByText("无到期时间").parentElement).toHaveTextContent("2");
  expect(screen.getByText("近 7 天完成").parentElement).toHaveTextContent("3");
  expect(screen.getByText("提醒成功").parentElement).toHaveTextContent("4");
  expect(screen.getByText("重试中").parentElement).toHaveTextContent("0");
  expect(screen.getByText("失败").parentElement).toHaveTextContent("0");
  expect(screen.getByText("被抑制").parentElement).toHaveTextContent("6");
});

it("renders every todo stat tile as a link to the todo workspace", () => {
  render(<DashboardView summary={summary} />);

  for (const label of [
    "待处理",
    "今日到期",
    "已逾期",
    "无到期时间",
    "近 7 天完成",
  ]) {
    expect(
      screen.getByRole("link", { name: new RegExp(label) }),
    ).toHaveAttribute("href", "/todos");
  }
});

it("renders reminder stat tiles as pressed-state buttons and exposes a reset", () => {
  const onSelectReminderStatus = vi.fn();
  render(
    <DashboardView
      onSelectReminderStatus={onSelectReminderStatus}
      selectedReminderStatus="failed"
      summary={summary}
    />,
  );

  const failed = screen.getByRole("button", { name: /失败/ });
  const succeeded = screen.getByRole("button", { name: /提醒成功/ });
  expect(failed).toHaveAttribute("type", "button");
  expect(failed).toHaveAttribute("aria-controls", "reminder-records");
  expect(failed).toHaveAttribute("aria-pressed", "true");
  expect(succeeded).toHaveAttribute("aria-pressed", "false");

  fireEvent.click(succeeded);
  expect(onSelectReminderStatus).toHaveBeenCalledWith("succeeded");

  fireEvent.click(screen.getByRole("button", { name: "全部提醒" }));
  expect(onSelectReminderStatus).toHaveBeenCalledWith(null);
});

it("shows the checked instant", () => {
  render(<DashboardView summary={summary} />);
  const time = screen.getByText("统计时间").closest("p")?.querySelector("time");
  expect(time).toHaveAttribute("datetime", "2026-08-18T12:00:00Z");
});

it("lists each reminder record with title, channel, state, and schedule", () => {
  render(<DashboardView summary={summary} deliveries={records} />);

  expect(screen.getByText("提醒记录")).toBeInTheDocument();
  expect(screen.getByLabelText("提醒记录")).toHaveAttribute(
    "id",
    "reminder-records",
  );
  const first = screen.getByText("《每日站会》").closest("li");
  expect(first).toHaveTextContent("email");
  expect(first).toHaveTextContent("succeeded");
  expect(first?.querySelector("time")).toHaveAttribute(
    "datetime",
    "2026-08-19T01:00:00Z",
  );
  const second = screen.getByText("《周报》").closest("li");
  expect(second).toHaveTextContent("sms");
  expect(second).toHaveTextContent("failed");
});

it("shows the receipt state when present", () => {
  render(<DashboardView summary={summary} deliveries={records} />);

  expect(screen.getByText("received_failed")).toBeInTheDocument();
  const first = screen.getByText("《每日站会》").closest("li");
  expect(first).not.toHaveTextContent("received_");
});

it("shows the empty state when there are no reminder records", () => {
  render(<DashboardView summary={summary} deliveries={[]} />);

  expect(screen.getByText("暂无提醒记录")).toBeInTheDocument();
});

it("keeps the controlled records region mounted before deliveries load", () => {
  render(<DashboardView summary={summary} />);

  const records = screen.getByLabelText("提醒记录");
  expect(records).toHaveAttribute("id", "reminder-records");
  expect(records).toHaveAttribute("aria-busy", "true");
  expect(records).not.toHaveAttribute("aria-live");
  expect(screen.getByRole("status")).toHaveTextContent("提醒记录加载中");
  expect(screen.queryByText("暂无提醒记录")).not.toBeInTheDocument();
});

it("keeps the reminder list outside the live status region", () => {
  render(<DashboardView summary={summary} deliveries={records} />);

  const region = screen.getByLabelText("提醒记录");
  expect(region).toHaveAttribute("aria-busy", "false");
  expect(region).not.toHaveAttribute("aria-live");
  expect(screen.getByRole("status")).toHaveTextContent("已加载 2 条提醒记录");
  expect(screen.getByRole("list")).not.toHaveAttribute("aria-live");
});
