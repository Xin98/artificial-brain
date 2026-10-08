import { fireEvent, render, screen } from "@testing-library/react";
import { expect, it, vi } from "vitest";
import { BacktestPanel } from "./backtest-panel";

it("translates run status and walks back through run history pages", async () => {
  const run = (runId: string, status: string, from: string) => ({
    runId,
    status,
    reason: "",
    datasetVersion: "fixture/synthetic/v2",
    from: from + "T00:00:00Z",
    to: "2026-07-31T00:00:00Z",
  });
  const request = vi.fn().mockImplementation(async (path: string) => ({
    ok: true,
    value: path.startsWith("/backtests?")
      ? path.includes("cursor=c2")
        ? { items: [run("old", "completed", "2026-01-01")], nextCursor: "" }
        : { items: [run("new", "queued", "2026-02-01")], nextCursor: "c2" }
      : { items: [], nextCursor: "" },
  }));
  render(<BacktestPanel client={{ request }} />);
  expect(await screen.findByText("已排队")).toBeVisible();
  fireEvent.click(screen.getByRole("button", { name: "下一页回测" }));
  expect(await screen.findByText("已完成")).toBeVisible();
  fireEvent.click(screen.getByRole("button", { name: "上一页回测" }));
  expect(await screen.findByText("已排队")).toBeVisible();
});

it("insufficient history never draws a fake backtest curve", async () => {
  const request = vi.fn().mockImplementation(async (path: string) => ({
    ok: true,
    value:
      path === "/backtests/fail"
        ? {
            runId: "fail",
            status: "failed",
            reason: "insufficient_history",
            errorCode: "insufficient_history",
            curve: [],
            benchmarkCurve: [],
            metrics: null,
            benchmarkMetrics: null,
            mode: "fixture",
            feed: "synthetic",
            datasetVersion: "fixture/synthetic/v2",
            asOf: "2026-10-06T21:00:00Z",
            qualityFlags: [],
            snapshotIds: [],
            initialCash: "100000.00",
            strategyVersionId: "s",
            universeVersionId: "u",
            from: "2026-07-01T00:00:00Z",
            to: "2026-07-31T00:00:00Z",
            executionModel: "model",
            intervalLabel: "research_not_out_of_sample",
            benchmarkReason: "benchmark_history_incomplete",
          }
        : { items: [], nextCursor: "" },
  }));
  render(<BacktestPanel client={{ request }} initialRunId="fail" />);
  expect(await screen.findByText(/回测失败：历史不足/)).toBeVisible();
  expect(screen.queryByRole("img")).toBeNull();
  expect(screen.getAllByText(/幸存者偏差/)[0]).toBeVisible();
});
it("defaults the research window to the last year", async () => {
  const request = vi.fn().mockImplementation(async () => ({
    ok: true,
    value: { items: [], nextCursor: "" },
  }));
  render(<BacktestPanel client={{ request }} />);
  const to = await screen.findByLabelText("结束日期");
  const from = screen.getByLabelText("开始日期");
  const expectedTo = new Date();
  const expectedFrom = new Date(expectedTo);
  expectedFrom.setUTCFullYear(expectedFrom.getUTCFullYear() - 1);
  expect(from).toHaveValue(expectedFrom.toISOString().slice(0, 10));
  expect(to).toHaveValue(expectedTo.toISOString().slice(0, 10));
});
