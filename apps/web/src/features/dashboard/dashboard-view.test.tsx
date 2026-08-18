import { render, screen } from "@testing-library/react";
import { expect, it } from "vitest";

import type { DashboardSummary } from "./fetch-dashboard";
import { DashboardView } from "./dashboard-view";

const summary: DashboardSummary = {
  pendingTotal: 5,
  dueToday: 2,
  overdue: 1,
  noDue: 2,
  completedLast7Days: 3,
  reminderRetrying: 0,
  reminderFailed: 0,
  checkedAt: "2026-08-18T12:00:00Z",
};

it("renders the six deterministic stat tiles", () => {
  render(<DashboardView summary={summary} />);

  expect(screen.getByText("待处理").parentElement).toHaveTextContent("5");
  expect(screen.getByText("今日到期").parentElement).toHaveTextContent("2");
  expect(screen.getByText("已逾期").parentElement).toHaveTextContent("1");
  expect(screen.getByText("无到期时间").parentElement).toHaveTextContent("2");
  expect(screen.getByText("近 7 天完成").parentElement).toHaveTextContent("3");
  expect(screen.getByText("提醒重试/失败").parentElement).toHaveTextContent(
    "0 / 0",
  );
});

it("shows the checked instant", () => {
  render(<DashboardView summary={summary} />);
  const time = screen.getByText("统计时间").closest("p")?.querySelector("time");
  expect(time).toHaveAttribute("datetime", "2026-08-18T12:00:00Z");
});
