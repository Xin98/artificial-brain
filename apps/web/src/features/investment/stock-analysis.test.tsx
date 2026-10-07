import { act, render, screen } from "@testing-library/react";
import { expect, it, vi } from "vitest";
import { StockAnalysis } from "./stock-analysis";
const analysis = (name: string) => ({
  instrument: { ticker: name, name, id: name },
  mode: "fixture",
  feed: "synthetic",
  asOf: "2026-10-06T21:00:00Z",
  datasetVersion: "fixture/synthetic/v2",
  qualityFlags: [],
  signal: null,
  risk: { level: "high", reasons: [] },
  recommendation: {
    potential: "high",
    action: "avoid_new_automatic_buy",
    evidence: [],
    unknowns: [],
  },
  metrics: {
    valuation: {
      pe: { value: null, reason: "nonpositive_earnings", factRefs: [] },
    },
    indicators: {},
  },
  newsStatus: "not_configured",
  newsReason: "news_permission_not_verified",
  topics: [],
});
it("renders unavailable PE and news reason", async () => {
  render(
    <StockAnalysis
      instrumentId="a"
      client={{
        request: vi.fn().mockResolvedValue({ ok: true, value: analysis("A") }),
      }}
    />,
  );
  expect(await screen.findByText("市盈率不适用：盈利非正")).toBeVisible();
  expect(screen.getByText(/新闻权限未验证/)).toBeVisible();
});
it("late analysis response cannot overwrite another stock or account", async () => {
  let resolve: (v: unknown) => void = () => {};
  const request = vi
    .fn()
    .mockImplementationOnce(
      () =>
        new Promise((r) => {
          resolve = r;
        }),
    )
    .mockResolvedValue({ ok: true, value: analysis("B") });
  const { rerender } = render(
    <StockAnalysis instrumentId="a" accountId="one" client={{ request }} />,
  );
  rerender(
    <StockAnalysis instrumentId="b" accountId="two" client={{ request }} />,
  );
  expect(await screen.findByRole("heading", { name: "B · B" })).toBeVisible();
  await act(async () => resolve({ ok: true, value: analysis("A") }));
  expect(screen.queryByRole("heading", { name: "A · A" })).toBeNull();
});
