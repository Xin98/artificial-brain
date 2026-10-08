import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { expect, it, vi } from "vitest";
import { OrderForm, OrderRow } from "./order-form";
import {
  accountFixture,
  instrumentsPageFixture,
  orderFixture,
} from "./test-fixtures";
it("requires integer shares and preserves inputs after 503", async () => {
  const request = vi
    .fn()
    .mockImplementation((path: string) =>
      path.startsWith("/instruments")
        ? Promise.resolve({ ok: true, value: instrumentsPageFixture() })
        : Promise.resolve({ ok: false, code: "service_unavailable" }),
    );
  render(
    <OrderForm
      client={{ request }}
      accountId="one"
      account={accountFixture()}
      onSubmitted={vi.fn()}
    />,
  );
  fireEvent.focus(await screen.findByLabelText("搜索证券"));
  fireEvent.click(
    await screen.findByRole("option", { name: /FX01 虚构企业 1/ }),
  );
  fireEvent.change(screen.getByLabelText("整数股数"), {
    target: { value: "1.5" },
  });
  fireEvent.submit(
    screen.getByRole("button", { name: "提交模拟订单" }).closest("form")!,
  );
  expect(await screen.findByRole("alert")).toHaveTextContent(/整数/);
  expect(
    request.mock.calls.filter(
      ([path]) => !String(path).startsWith("/instruments"),
    ),
  ).toHaveLength(0);
  fireEvent.change(screen.getByLabelText("整数股数"), {
    target: { value: "10" },
  });
  fireEvent.click(screen.getByRole("button", { name: "提交模拟订单" }));
  expect(await screen.findByRole("alert")).toHaveTextContent(/服务暂不可用/);
  expect(screen.getByLabelText("整数股数")).toHaveValue("10");
});
it("searches instruments on the server and submits the selected id, not the ticker", async () => {
  const request = vi.fn().mockImplementation((path: string) => {
    if (path.startsWith("/instruments")) {
      const search =
        new URL(path, "http://local").searchParams.get("search") ?? "";
      const items = instrumentsPageFixture().items.filter((item) =>
        (
          item.instrument.ticker +
          " " +
          item.instrument.name +
          " " +
          item.instrument.id
        ).includes(search),
      );
      return Promise.resolve({ ok: true, value: { items, nextCursor: "" } });
    }
    return Promise.resolve({ ok: true, value: orderFixture("pending") });
  });
  render(
    <OrderForm
      client={{ request }}
      accountId="one"
      account={accountFixture()}
      onSubmitted={vi.fn()}
    />,
  );
  const search = await screen.findByLabelText("搜索证券");
  fireEvent.change(search, { target: { value: "虚构企业 2" } });
  await waitFor(() =>
    expect(screen.queryByRole("option", { name: /FX01/ })).toBeNull(),
  );
  const searches = request.mock.calls.filter(
    ([path]) =>
      new URL(String(path), "http://local").searchParams.get("search") ===
      "虚构企业 2",
  );
  expect(searches).toHaveLength(1);
  fireEvent.click(
    await screen.findByRole("option", { name: /FX02 虚构企业 2/ }),
  );
  expect(screen.getByText(/已选证券：/)).toBeVisible();
  expect(screen.getByText("FX02")).toBeVisible();
  fireEvent.change(screen.getByLabelText("整数股数"), {
    target: { value: "10" },
  });
  fireEvent.click(screen.getByRole("button", { name: "提交模拟订单" }));
  await waitFor(() => {
    const call = request.mock.calls.find(
      ([path]) => path === "/accounts/one/orders",
    );
    expect(call).toBeTruthy();
    expect(JSON.parse(String(call![1].body))).toMatchObject({
      instrumentId: "fixture-02",
    });
  });
});
it("blocks submit until an instrument is selected", async () => {
  const request = vi
    .fn()
    .mockImplementation((path: string) =>
      path.startsWith("/instruments")
        ? Promise.resolve({ ok: true, value: instrumentsPageFixture() })
        : Promise.resolve({ ok: true, value: orderFixture("pending") }),
    );
  render(
    <OrderForm
      client={{ request }}
      accountId="one"
      account={accountFixture()}
      onSubmitted={vi.fn()}
    />,
  );
  await screen.findByLabelText("搜索证券");
  fireEvent.change(screen.getByLabelText("整数股数"), {
    target: { value: "10" },
  });
  fireEvent.click(screen.getByRole("button", { name: "提交模拟订单" }));
  expect(await screen.findByRole("alert")).toHaveTextContent(/选择证券/);
  expect(
    request.mock.calls.filter(([path]) => path === "/accounts/one/orders"),
  ).toHaveLength(0);
});
it("keeps the option list hidden until focus and closes it on Escape", async () => {
  const request = vi
    .fn()
    .mockImplementation((path: string) =>
      path.startsWith("/instruments")
        ? Promise.resolve({ ok: true, value: instrumentsPageFixture() })
        : Promise.resolve({ ok: true, value: orderFixture("pending") }),
    );
  render(
    <OrderForm
      client={{ request }}
      accountId="one"
      account={accountFixture()}
      onSubmitted={vi.fn()}
    />,
  );
  const search = await screen.findByLabelText("搜索证券");
  expect(screen.queryByRole("listbox")).toBeNull();
  fireEvent.focus(search);
  expect(await screen.findByRole("listbox")).toBeVisible();
  fireEvent.keyDown(search, { key: "Escape" });
  expect(screen.queryByRole("listbox")).toBeNull();
});
it("order before open can cancel and after open cannot cancel", async () => {
  const request = vi
    .fn()
    .mockResolvedValue({ ok: true, value: orderFixture("cancelled") });
  const changed = vi.fn();
  const { rerender } = render(
    <table>
      <tbody>
        <OrderRow
          client={{ request }}
          accountId="one"
          order={orderFixture("pending")}
          onChanged={changed}
        />
      </tbody>
    </table>,
  );
  fireEvent.click(screen.getByRole("button", { name: "撤销订单" }));
  await waitFor(() => expect(changed).toHaveBeenCalledTimes(1));
  rerender(
    <table>
      <tbody>
        <OrderRow
          client={{ request }}
          accountId="one"
          order={orderFixture()}
          onChanged={changed}
        />
      </tbody>
    </table>,
  );
  expect(screen.queryByRole("button", { name: "撤销订单" })).toBeNull();
});
it("shows available cash and the account max buyable quantity for buys", async () => {
  const request = vi.fn().mockImplementation((path: string) => {
    if (path.includes("/analysis"))
      return Promise.resolve({
        ok: true,
        value: {
          accountRisk: { allowed: true, maxQuantity: "42", reasonCode: "" },
        },
      });
    if (path.startsWith("/instruments"))
      return Promise.resolve({ ok: true, value: instrumentsPageFixture() });
    return Promise.resolve({ ok: true, value: orderFixture("pending") });
  });
  render(
    <OrderForm
      client={{ request }}
      accountId="one"
      account={accountFixture()}
      onSubmitted={vi.fn()}
    />,
  );
  expect(await screen.findByText(/可用现金 90,000.00 USD/)).toBeVisible();
  fireEvent.focus(await screen.findByLabelText("搜索证券"));
  fireEvent.click(
    await screen.findByRole("option", { name: /FX01 虚构企业 1/ }),
  );
  expect(await screen.findByText(/最多可买 42 股/)).toBeVisible();
  fireEvent.change(screen.getByLabelText("方向"), {
    target: { value: "sell" },
  });
  expect(screen.queryByText(/最多可买/)).toBeNull();
});
it("clears security and quantity after a successful submit", async () => {
  const request = vi
    .fn()
    .mockImplementation((path: string) =>
      path.startsWith("/instruments")
        ? Promise.resolve({ ok: true, value: instrumentsPageFixture() })
        : Promise.resolve({ ok: true, value: orderFixture("pending") }),
    );
  render(
    <OrderForm
      client={{ request }}
      accountId="one"
      account={accountFixture()}
      onSubmitted={vi.fn()}
    />,
  );
  fireEvent.focus(await screen.findByLabelText("搜索证券"));
  fireEvent.click(
    await screen.findByRole("option", { name: /FX01 虚构企业 1/ }),
  );
  fireEvent.change(screen.getByLabelText("整数股数"), {
    target: { value: "10" },
  });
  fireEvent.click(screen.getByRole("button", { name: "提交模拟订单" }));
  expect(await screen.findByText(/订单状态：待生效/)).toBeVisible();
  expect(screen.getByLabelText("搜索证券")).toHaveValue("");
  expect(screen.getByLabelText("整数股数")).toHaveValue("1");
});
it("accepts a prefilled instrument from the account link", async () => {
  const request = vi
    .fn()
    .mockImplementation((path: string) =>
      path.startsWith("/instruments")
        ? Promise.resolve({ ok: true, value: instrumentsPageFixture() })
        : Promise.resolve({ ok: true, value: orderFixture("pending") }),
    );
  render(
    <OrderForm
      client={{ request }}
      accountId="one"
      account={accountFixture()}
      prefillInstrumentId="fixture-09"
      onSubmitted={vi.fn()}
    />,
  );
  expect(await screen.findByText(/已选证券：/)).toBeVisible();
  expect(screen.getByText("fixture-09")).toBeVisible();
  fireEvent.change(screen.getByLabelText("整数股数"), {
    target: { value: "3" },
  });
  fireEvent.click(screen.getByRole("button", { name: "提交模拟订单" }));
  await waitFor(() => {
    const call = request.mock.calls.find(
      ([path]) => path === "/accounts/one/orders",
    );
    expect(call).toBeTruthy();
    expect(JSON.parse(String(call![1].body))).toMatchObject({
      instrumentId: "fixture-09",
      quantity: "3",
    });
  });
});
