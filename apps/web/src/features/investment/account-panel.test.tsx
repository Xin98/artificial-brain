import { act, fireEvent, render, screen } from "@testing-library/react";
import { expect, it, vi } from "vitest";
import { AccountPanel } from "./account-panel";
import {
  accountFixture,
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
  expect(screen.getByText("5000.00")).toBeVisible();
  expect(screen.getByText("4000.00")).toBeVisible();
  expect(screen.getByText("1000.00")).toBeVisible();
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
        return {
          ok: true,
          value: path.endsWith("/performance")
            ? performanceFixture()
            : { items: [], nextCursor: "" },
        };
      });
    render(<AccountPanel client={{ request }} accountId="one" />);
    await act(async () => {});
    fireEvent.change(screen.getByLabelText("证券 ID"), {
      target: { value: "fixture-01" },
    });
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
