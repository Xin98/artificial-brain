import { act, fireEvent, render, screen } from "@testing-library/react";
import { expect, it, vi } from "vitest";
import { ResearchPanel } from "./research-panel";

const dataStatusFixture = () => ({
  mode: "fixture",
  feed: "synthetic",
  asOf: "2026-10-06T21:00:00Z",
  datasetVersion: "fixture/synthetic/v2",
  qualityFlags: [],
  market: { state: "available" },
  financial: { state: "available" },
  news: { state: "available" },
  calendar: { state: "available" },
});
const rankRow = (id: string, ticker: string) => ({
  instrument: { id, ticker, name: "演示公司 " + ticker },
  price: "100.000000",
  signal: { rank: 1, score: 80 },
  risk: { level: "high" },
  potential: "high",
  reason: "",
});

it("debounces search input and keeps the previous ranking while refreshing", async () => {
  vi.useFakeTimers();
  try {
    let hold: (v: unknown) => void = () => {};
    const request = vi.fn().mockImplementation((path: string) => {
      if (path.startsWith("/data-status"))
        return Promise.resolve({ ok: true, value: dataStatusFixture() });
      if (path.startsWith("/universes") || path.startsWith("/strategies"))
        return Promise.resolve({
          ok: true,
          value: { items: [], nextCursor: "" },
        });
      if (path.includes("search=demo"))
        return new Promise((r) => {
          hold = r;
        });
      return Promise.resolve({
        ok: true,
        value: { items: [rankRow("fixture-01", "DEMO01")], nextCursor: "" },
      });
    });
    render(<ResearchPanel client={{ request }} />);
    await act(async () => {});
    await act(async () => {});
    expect(screen.getByText("DEMO01")).toBeVisible();
    const instrumentCalls = () =>
      request.mock.calls.filter(([p]) => String(p).startsWith("/instruments"));
    expect(instrumentCalls()).toHaveLength(1);
    fireEvent.change(screen.getByLabelText("搜索股票"), {
      target: { value: "d" },
    });
    fireEvent.change(screen.getByLabelText("搜索股票"), {
      target: { value: "demo" },
    });
    await act(async () => {});
    expect(instrumentCalls()).toHaveLength(1);
    await act(async () => {
      await vi.advanceTimersByTimeAsync(300);
    });
    expect(instrumentCalls()).toHaveLength(2);
    expect(
      instrumentCalls().filter(([p]) => String(p).includes("search=demo")),
    ).toHaveLength(1);
    expect(screen.getByText("DEMO01")).toBeVisible();
    expect(screen.getByText(/正在更新排名/)).toBeVisible();
    await act(async () => {
      hold({
        ok: true,
        value: { items: [rankRow("fixture-02", "DEMO02")], nextCursor: "" },
      });
    });
    expect(screen.getByText("DEMO02")).toBeVisible();
    expect(screen.queryByText("DEMO01")).toBeNull();
    expect(screen.queryByText(/正在更新排名/)).toBeNull();
  } finally {
    vi.useRealTimers();
  }
});

it("walks back to the previous ranking page", async () => {
  const request = vi.fn().mockImplementation(async (path: string) => ({
    ok: true,
    value: path.startsWith("/data-status")
      ? dataStatusFixture()
      : path.startsWith("/universes") || path.startsWith("/strategies")
        ? { items: [], nextCursor: "" }
        : path.includes("cursor=c2")
          ? { items: [rankRow("fixture-02", "DEMO02")], nextCursor: "" }
          : { items: [rankRow("fixture-01", "DEMO01")], nextCursor: "c2" },
  }));
  render(<ResearchPanel client={{ request }} />);
  expect(await screen.findByText("DEMO01")).toBeVisible();
  fireEvent.click(screen.getByRole("button", { name: "下一页股票" }));
  expect(await screen.findByText("DEMO02")).toBeVisible();
  expect(screen.queryByText("DEMO01")).toBeNull();
  fireEvent.click(screen.getByRole("button", { name: "上一页股票" }));
  expect(await screen.findByText("DEMO01")).toBeVisible();
});

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
