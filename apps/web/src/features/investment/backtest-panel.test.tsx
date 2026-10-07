import { render, screen } from "@testing-library/react";
import { expect, it, vi } from "vitest";
import { BacktestPanel } from "./backtest-panel";
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
