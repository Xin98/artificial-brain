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
  prices: [],
  recommendation: {
    potential: "high",
    action: "avoid_new_automatic_buy",
    evidence: [] as string[],
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
it("renders account holding reduction separately from buy permission", async () => {
  const value = analysis("A");
  value.recommendation.action = "reduce_holding";
  value.recommendation.evidence = ["stop_loss"];
  render(
    <StockAnalysis
      instrumentId="a"
      accountId="one"
      client={{ request: vi.fn().mockResolvedValue({ ok: true, value }) }}
    />,
  );
  expect(await screen.findByText(/当前持仓触发减仓规则/)).toBeVisible();
  expect(screen.getByText("持仓亏损达到止损阈值")).toBeVisible();
});
it("renders latest close and a price chart from recent closes", async () => {
  const value = {
    ...analysis("A"),
    prices: [
      { sessionDate: "2026-10-05T00:00:00Z", close: "25.000000" },
      { sessionDate: "2026-10-06T00:00:00Z", close: "26.500000" },
      { sessionDate: "2026-10-07T00:00:00Z", close: "26.000000" },
    ],
  };
  render(
    <StockAnalysis
      instrumentId="a"
      client={{ request: vi.fn().mockResolvedValue({ ok: true, value }) }}
    />,
  );
  expect(
    await screen.findByText(/最新收盘价 26\.00 USD · 2026-10-07/),
  ).toBeVisible();
  expect(screen.getByRole("img", { name: "价格走势" })).toBeVisible();
  expect(screen.getAllByText("2026-10-05").length).toBeGreaterThan(0);
  expect(screen.getAllByText("2026-10-07").length).toBeGreaterThan(0);
});
it("shows an explicit empty state when no price is known", async () => {
  render(
    <StockAnalysis
      instrumentId="a"
      client={{
        request: vi.fn().mockResolvedValue({ ok: true, value: analysis("A") }),
      }}
    />,
  );
  expect(await screen.findByText("暂无已知价格。")).toBeVisible();
  expect(screen.queryByRole("img", { name: "价格走势" })).toBeNull();
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
it("links to the account order form only when an account context exists", async () => {
  const { unmount } = render(
    <StockAnalysis
      instrumentId="a"
      accountId="one"
      client={{
        request: vi.fn().mockResolvedValue({ ok: true, value: analysis("A") }),
      }}
    />,
  );
  const link = await screen.findByRole("link", { name: "前往账户下单" });
  expect(link).toHaveAttribute("href", "/investment/accounts/one?instrument=a");
  unmount();
  render(
    <StockAnalysis
      instrumentId="a"
      client={{
        request: vi.fn().mockResolvedValue({ ok: true, value: analysis("A") }),
      }}
    />,
  );
  expect(await screen.findByText(/风险较高，自动策略避免新买入/)).toBeVisible();
  expect(screen.queryByRole("link", { name: "前往账户下单" })).toBeNull();
});
it("returns to the filtered research list when a return query exists", async () => {
  render(
    <StockAnalysis
      instrumentId="a"
      returnQuery="search=demo&risk=low"
      client={{
        request: vi.fn().mockResolvedValue({ ok: true, value: analysis("A") }),
      }}
    />,
  );
  const back = await screen.findByRole("link", { name: "返回股票研究" });
  expect(back).toHaveAttribute("href", "/investment?search=demo&risk=low");
});
