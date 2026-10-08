import { act, fireEvent, render, screen } from "@testing-library/react";
import { expect, it, vi } from "vitest";
import { AccountDirectory, AccountPanel } from "./account-panel";
import {
  accountFixture,
  instrumentsPageFixture,
  orderFixture,
  performanceFixture,
} from "./test-fixtures";
it("reserved unsettled and dividends remain separate and pause preserves effective orders", async () => {
  const request = vi.fn().mockImplementation(async (path: string) => ({
    ok: true,
    value: path.endsWith("/performance")
      ? performanceFixture()
      : path === "/accounts/one"
        ? accountFixture()
        : {
            items: path.includes("/orders?") ? [orderFixture()] : [],
            nextCursor: "",
          },
  }));
  render(<AccountPanel client={{ request }} accountId="one" />);
  expect(await screen.findByText("已生效，等待日线确认")).toBeVisible();
  expect(screen.getByText("100,000.00")).toBeVisible();
  expect(screen.getByText("5,000.00")).toBeVisible();
  expect(screen.getByText("4,000.00")).toBeVisible();
  expect(screen.getByText("1,000.00")).toBeVisible();
  expect(screen.getAllByText(/暂停后仍可能补记/)[0]).toBeVisible();
  expect(screen.queryByRole("button", { name: "撤销订单" })).toBeNull();
});
it("switch account ignores old response", async () => {
  let finish: (v: unknown) => void = () => {};
  const request = vi.fn().mockImplementation((path: string) =>
    path === "/accounts/one"
      ? new Promise((r) => {
          finish = r;
        })
      : Promise.resolve({
          ok: true,
          value: path.endsWith("/performance")
            ? performanceFixture()
            : path === "/accounts/two"
              ? accountFixture("two")
              : { items: [], nextCursor: "" },
        }),
  );
  const { rerender } = render(
    <AccountPanel client={{ request }} accountId="one" />,
  );
  rerender(<AccountPanel client={{ request }} accountId="two" />);
  expect(await screen.findByRole("heading", { name: "two" })).toBeVisible();
  await act(async () => finish({ ok: true, value: accountFixture("one") }));
  expect(screen.queryByRole("heading", { name: "one" })).toBeNull();
});

it("a failed account poll preserves uncertain order intent and draft through recovery", async () => {
  vi.useFakeTimers();
  try {
    let accountReads = 0;
    let posts = 0;
    const request = vi
      .fn()
      .mockImplementation(async (path: string, options: RequestInit) => {
        if (options.method === "POST" && path.endsWith("/orders")) {
          posts++;
          return posts === 1
            ? { ok: false, code: "timeout" }
            : { ok: true, value: orderFixture("pending") };
        }
        if (path === "/accounts/one") {
          accountReads++;
          return accountReads === 2
            ? { ok: false, code: "service_unavailable" }
            : {
                ok: true,
                value: { ...accountFixture(), version: accountReads },
              };
        }
        if (path.startsWith("/instruments")) {
          return { ok: true, value: instrumentsPageFixture() };
        }
        return {
          ok: true,
          value: path.endsWith("/performance")
            ? performanceFixture()
            : { items: [], nextCursor: "" },
        };
      });
    render(<AccountPanel client={{ request }} accountId="one" />);
    await act(async () => {});
    await act(async () => {});
    fireEvent.focus(screen.getByLabelText("搜索证券"));
    fireEvent.click(screen.getByRole("option", { name: /FX01 虚构企业 1/ }));
    fireEvent.change(screen.getByLabelText("整数股数"), {
      target: { value: "10" },
    });
    await act(async () =>
      fireEvent.click(screen.getByRole("button", { name: "提交模拟订单" })),
    );
    await act(async () => vi.advanceTimersByTimeAsync(5000));
    expect(screen.getByLabelText("整数股数")).toHaveValue("10");
    expect(screen.getByText(/服务暂不可用/)).toBeVisible();
    await act(async () => vi.advanceTimersByTimeAsync(5000));
    await act(async () =>
      fireEvent.click(screen.getByRole("button", { name: "提交模拟订单" })),
    );
    const calls = request.mock.calls.filter(
      ([path, options]) =>
        path.endsWith("/orders") && options.method === "POST",
    );
    expect(calls).toHaveLength(2);
    expect(calls[1][1].body).toBe(calls[0][1].body);
    expect(calls[1][1].headers["Idempotency-Key"]).toBe(
      calls[0][1].headers["Idempotency-Key"],
    );
  } finally {
    vi.useRealTimers();
  }
});
it("translates ledger kinds, automation event kinds and evaluation states", async () => {
  const request = vi.fn().mockImplementation(async (path: string) => ({
    ok: true,
    value:
      path === "/accounts/one"
        ? accountFixture()
        : path.endsWith("/performance")
          ? performanceFixture()
          : path.includes("/evaluations")
            ? {
                items: [
                  {
                    id: "ev1",
                    sessionDate: "2026-10-06",
                    purpose: "automatic",
                    state: "completed",
                    reason: "",
                    orderIds: [],
                  },
                ],
                nextCursor: "",
              }
            : path.includes("/automation-events")
              ? {
                  items: [
                    {
                      id: "e1",
                      kind: "enabled",
                      reason: "",
                      effectiveAt: "2026-10-06T21:00:00Z",
                      recordedAt: "2026-10-06T21:00:00Z",
                    },
                  ],
                  nextCursor: "",
                }
              : path.includes("/ledger")
                ? {
                    items: [
                      {
                        id: "l1",
                        accountId: "one",
                        eventKey: "k1",
                        kind: "sell_fill",
                        instrumentId: "fixture-01",
                        delta: {
                          available: "-2100.00",
                          reserved: "0.00",
                          unsettled: "100.00",
                          dividends: "0.00",
                        },
                        quantityDelta: "0",
                        effectiveAt: "2026-10-06T21:00:00Z",
                        recordedAt: "2026-10-06T21:00:00Z",
                      },
                    ],
                    nextCursor: "",
                  }
                : { items: [], nextCursor: "" },
  }));
  render(<AccountPanel client={{ request }} accountId="one" />);
  expect(await screen.findByText("卖出成交")).toBeVisible();
  expect(screen.getByText("-2,100.00")).toBeVisible();
  expect(screen.getByText("启用")).toBeVisible();
  expect(screen.getByText("已完成")).toBeVisible();
});
it("paged tables walk back to the previous page", async () => {
  const request = vi.fn().mockImplementation(async (path: string) => ({
    ok: true,
    value:
      path === "/accounts/one"
        ? accountFixture()
        : path.endsWith("/performance")
          ? performanceFixture()
          : path.includes("/orders?")
            ? path.includes("cursor=c2")
              ? {
                  items: [
                    { ...orderFixture(), id: "b", instrumentId: "fixture-02" },
                  ],
                  nextCursor: "",
                }
              : { items: [orderFixture()], nextCursor: "c2" }
            : { items: [], nextCursor: "" },
  }));
  render(<AccountPanel client={{ request }} accountId="one" />);
  expect(await screen.findByText("fixture-01")).toBeVisible();
  fireEvent.click(screen.getByRole("button", { name: "下一页订单" }));
  expect(await screen.findByText("fixture-02")).toBeVisible();
  expect(screen.queryByText("fixture-01")).toBeNull();
  fireEvent.click(screen.getByRole("button", { name: "上一页订单" }));
  expect(await screen.findByText("fixture-01")).toBeVisible();
});
it("stops polling a paged table after leaving its first page", async () => {
  vi.useFakeTimers();
  try {
    const request = vi.fn().mockImplementation(async (path: string) => ({
      ok: true,
      value:
        path === "/accounts/one"
          ? accountFixture()
          : path.endsWith("/performance")
            ? performanceFixture()
            : path.includes("/orders?")
              ? path.includes("cursor=c2")
                ? {
                    items: [
                      {
                        ...orderFixture(),
                        id: "b",
                        instrumentId: "fixture-02",
                      },
                    ],
                    nextCursor: "",
                  }
                : { items: [orderFixture()], nextCursor: "c2" }
              : { items: [], nextCursor: "" },
    }));
    render(<AccountPanel client={{ request }} accountId="one" />);
    await act(async () => {});
    await act(async () => {});
    expect(screen.getByText("fixture-01")).toBeVisible();
    const orderCalls = () =>
      request.mock.calls.filter(([p]) => String(p).includes("/orders?")).length;
    await act(async () => {
      await vi.advanceTimersByTimeAsync(5000);
    });
    expect(orderCalls()).toBeGreaterThan(1);
    fireEvent.click(screen.getByRole("button", { name: "下一页订单" }));
    await act(async () => {});
    await act(async () => {});
    expect(screen.getByText("fixture-02")).toBeVisible();
    const onSecondPage = orderCalls();
    await act(async () => {
      await vi.advanceTimersByTimeAsync(15000);
    });
    expect(orderCalls()).toBe(onSecondPage);
  } finally {
    vi.useRealTimers();
  }
});
it("account directory walks back to the previous page", async () => {
  const request = vi.fn().mockImplementation(async (path: string) => ({
    ok: true,
    value: path.startsWith("/accounts?")
      ? path.includes("cursor=c2")
        ? { items: [accountFixture("two")], nextCursor: "" }
        : { items: [accountFixture("one")], nextCursor: "c2" }
      : { items: [], nextCursor: "" },
  }));
  render(<AccountDirectory client={{ request }} />);
  expect(await screen.findByRole("link", { name: "one" })).toBeVisible();
  fireEvent.click(screen.getByRole("button", { name: "下一页账户" }));
  expect(await screen.findByRole("link", { name: "two" })).toBeVisible();
  fireEvent.click(screen.getByRole("button", { name: "上一页账户" }));
  expect(await screen.findByRole("link", { name: "one" })).toBeVisible();
});
it("account directory offers per-account order links for a carried instrument", async () => {
  const request = vi.fn().mockImplementation(async (path: string) => ({
    ok: true,
    value: path.startsWith("/accounts?")
      ? { items: [accountFixture("one")], nextCursor: "" }
      : { items: [], nextCursor: "" },
  }));
  render(
    <AccountDirectory client={{ request }} prefillInstrumentId="fixture-01" />,
  );
  expect(await screen.findByRole("link", { name: "one" })).toBeVisible();
  const order = screen.getByRole("link", { name: "下单" });
  expect(order).toHaveAttribute(
    "href",
    "/investment/accounts/one?instrument=fixture-01",
  );
});
