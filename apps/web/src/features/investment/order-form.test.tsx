import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { expect, it, vi } from "vitest";
import { OrderForm, OrderRow } from "./order-form";
import { accountFixture, orderFixture } from "./test-fixtures";
it("requires integer shares and preserves inputs after 503", async () => {
  const request = vi
    .fn()
    .mockResolvedValue({ ok: false, code: "service_unavailable" });
  render(
    <OrderForm
      client={{ request }}
      accountId="one"
      account={accountFixture()}
      onSubmitted={vi.fn()}
    />,
  );
  fireEvent.change(screen.getByLabelText("证券 ID"), {
    target: { value: "fixture-01" },
  });
  fireEvent.change(screen.getByLabelText("整数股数"), {
    target: { value: "1.5" },
  });
  fireEvent.submit(
    screen.getByRole("button", { name: "提交模拟订单" }).closest("form")!,
  );
  expect(await screen.findByRole("alert")).toHaveTextContent(/整数/);
  expect(request).not.toHaveBeenCalled();
  fireEvent.change(screen.getByLabelText("整数股数"), {
    target: { value: "10" },
  });
  fireEvent.click(screen.getByRole("button", { name: "提交模拟订单" }));
  expect(await screen.findByRole("alert")).toHaveTextContent(/服务暂不可用/);
  expect(screen.getByLabelText("整数股数")).toHaveValue("10");
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
