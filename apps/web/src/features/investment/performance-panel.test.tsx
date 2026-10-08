import { render, screen } from "@testing-library/react";
import { expect, it, vi } from "vitest";
import { PerformancePanel } from "./performance-panel";
const value = {
  mode: "fixture",
  feed: "synthetic",
  datasetVersion: "fixture/synthetic/v2",
  asOf: "2026-10-06T21:00:00Z",
  curve: [{ sessionDate: "2026-10-06T00:00:00Z", nav: "100000.00" }],
  benchmarkCurve: [],
  metrics: {
    annualReturn: null,
    sharpe: null,
    cumulativeReturn: 0,
    maxDrawdown: 0,
    turnover: 0,
    tradingDays: 1,
    missingReasons: [
      "annual_requires_252_trading_days",
      "sharpe_requires_60_daily_returns",
    ],
  },
  benchmarkMetrics: null,
  qualityFlags: [],
  benchmarkReason: "benchmark_history_incomplete",
  kind: "forward_paper",
  initialCash: "100000.00",
};
it("forward paper and backtest results are explicitly separate", async () => {
  render(
    <PerformancePanel
      client={{ request: vi.fn().mockResolvedValue({ ok: true, value }) }}
      resource="account"
      id="one"
    />,
  );
  expect(
    await screen.findByRole("heading", { name: "模拟账户前向绩效" }),
  ).toBeVisible();
  expect(screen.queryByRole("heading", { name: "历史回测绩效" })).toBeNull();
});
it("short history has unavailable annual Sharpe and incomplete benchmark reasons", async () => {
  render(
    <PerformancePanel
      client={{ request: vi.fn().mockResolvedValue({ ok: true, value }) }}
      resource="account"
      id="one"
    />,
  );
  expect(
    await screen.findByText("年化收益不适用：不足 252 个交易日"),
  ).toBeVisible();
  expect(screen.getByText("Sharpe 不适用：不足 60 个有效日收益")).toBeVisible();
  expect(screen.getByText("无法比较基准：数据不完整")).toBeVisible();
});
