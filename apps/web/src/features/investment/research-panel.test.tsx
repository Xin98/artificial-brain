import { render, screen } from "@testing-library/react";
import { expect, it, vi } from "vitest";
import { ResearchPanel } from "./research-panel";
it("shows demo feed asOf and independent potential and risk", async () => {
  const request = vi.fn().mockImplementation(async (path: string) => ({
    ok: true,
    value: path.startsWith("/data-status")
      ? {
          mode: "fixture",
          feed: "synthetic",
          asOf: "2026-10-06T21:00:00Z",
          datasetVersion: "fixture/synthetic/v2",
          qualityFlags: [],
          market: { state: "available" },
          financial: { state: "available" },
          news: { state: "available" },
          calendar: { state: "available" },
        }
      : path.startsWith("/universes")
        ? { items: [], nextCursor: "" }
        : {
            items: [
              {
                instrument: {
                  id: "fixture-01",
                  ticker: "DEMO01",
                  name: "演示公司",
                },
                price: "100.000000",
                signal: { rank: 1, score: 80 },
                risk: { level: "high" },
                potential: "high",
                reason: "",
              },
            ],
            nextCursor: "",
          },
  }));
  render(<ResearchPanel client={{ request }} />);
  expect(await screen.findByText("潜力较高")).toBeVisible();
  expect(screen.getByText("风险高")).toBeVisible();
  expect(screen.getByText("演示数据")).toBeVisible();
  expect(screen.getAllByText(/synthetic/)[0]).toBeVisible();
  expect(screen.getByText(/2026-10-06/)).toBeVisible();
});
