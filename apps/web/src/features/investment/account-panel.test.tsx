import { act, render, screen } from "@testing-library/react";
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
