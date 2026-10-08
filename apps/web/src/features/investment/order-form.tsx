"use client";
import { useEffect, useRef, useState } from "react";
import Link from "next/link";
import { decode, failureText, type InvestmentClient } from "./fetch-investment";
import { useMutation } from "./hooks";
import type { AccountView, OrderView, PlaceOrderRequest } from "./types";
export const orderState = (state: string) =>
  (
    ({
      pending: "待生效",
      awaiting_bar: "已生效，等待日线确认",
      filled: "已成交",
      partially_filled_cancelled: "部分成交，余量取消",
      expired: "行情缺失已过期",
      rejected: "已拒绝",
      cancelled: "已撤销",
    }) as Record<string, string>
  )[state] ?? state;
export function OrderForm({
  client,
  accountId,
  account,
  onSubmitted,
}: {
  client: InvestmentClient;
  accountId: string;
  account?: AccountView;
  onSubmitted: () => void;
}) {
  const [instrument, setInstrument] = useState("");
  const [side, setSide] = useState("buy");
  const [quantity, setQuantity] = useState("1");
  const [error, setError] = useState("");
  const mutation = useMutation<OrderView>(client, "OrderView");
  const frozen = useRef<{ signature: string; body: PlaceOrderRequest } | null>(
    null,
  );
  return (
    <form
      className="investment-form"
      onSubmit={async (e) => {
        e.preventDefault();
        if (!/^[1-9][0-9]*$/.test(quantity) || BigInt(quantity) > 1000000000n) {
          setError("股数须为 1 至 1000000000 的整数。");
          return;
        }
        setError("");
        const signature = JSON.stringify({ instrument, side, quantity });
        if (frozen.current?.signature !== signature) {
          let a = account;
          if (!a) {
            const result = await client.request<AccountView>(
              "/accounts/" + accountId,
              {},
              decode("AccountView"),
            );
            if (!result.ok) {
              setError(failureText(result.code));
              return;
            }
            a = result.value;
          }
          frozen.current = {
            signature,
            body: {
              instrumentId: instrument,
              side,
              quantity,
              expectedVersion: a.version,
            },
          };
        }
        const result = await mutation.submit(
          "POST",
          "/accounts/" + accountId + "/orders",
          frozen.current.body,
        );
        if (result?.ok) {
          frozen.current = null;
          onSubmitted();
        } else if (
          result?.code === "version_conflict" ||
          result?.code === "conflict"
        ) {
          setError("账户版本已变化，请先刷新账户，再重新提交。");
          frozen.current = null;
          onSubmitted();
        }
      }}
    >
      <h2>手动模拟订单</h2>
      <div className="investment-fields">
        <label>
          证券 ID
          <input
            required
            value={instrument}
            onChange={(e) => setInstrument(e.target.value)}
          />
        </label>
        <label>
          方向
          <select value={side} onChange={(e) => setSide(e.target.value)}>
            <option value="buy">买入</option>
            <option value="sell">卖出</option>
          </select>
        </label>
        <label>
          整数股数
          <input
            required
            inputMode="numeric"
            value={quantity}
            onChange={(e) => setQuantity(e.target.value)}
          />
        </label>
      </div>
      <p>
        按下一常规开盘价加 10bp 滑点、1bp 费用（最低 0.01
        USD）模拟；不保证全额成交。未结算卖出款不可再次买入。
      </p>
      {error ? (
        <p role="alert">{error}</p>
      ) : mutation.result && !mutation.result.ok ? (
        <p role="alert">{failureText(mutation.result.code)}</p>
      ) : mutation.result?.ok ? (
        <p role="status">订单状态：{orderState(mutation.result.value.state)}</p>
      ) : null}
      <button disabled={mutation.busy}>提交模拟订单</button>
    </form>
  );
}
export function OrderRow({
  client,
  accountId,
  order,
  onChanged,
}: {
  client: InvestmentClient;
  accountId: string;
  order: OrderView;
  onChanged: () => void;
}) {
  const mutation = useMutation<OrderView>(client, "OrderView");
  const [clock, setClock] = useState(() => Date.now());
  useEffect(() => {
    const timer = setTimeout(
      () => setClock(Date.now()),
      Math.max(
        1,
        Math.min(Date.parse(order.targetOpenAt) - Date.now(), 2147483647),
      ),
    );
    return () => clearTimeout(timer);
  }, [order.targetOpenAt]);
  const canCancel =
    order.state === "pending" && clock < Date.parse(order.targetOpenAt);
  return (
    <tr>
      <td>
        <Link
          href={
            "/investment/stocks/" +
            encodeURIComponent(order.instrumentId) +
            "?accountId=" +
            accountId
          }
        >
          {order.instrumentId}
        </Link>
        <small>
          {order.side === "buy" ? "买入" : "卖出"} {order.quantity} 股
        </small>
      </td>
      <td>
        {orderState(order.state)}
        <small>{order.reason}</small>
      </td>
      <td>
        {order.targetOpenAt}
        <small>最迟确认 {order.expiresAt}</small>
      </td>
      <td>
        {order.fill ? (
          <>
            {order.fill.price}
            <small>
              成交 {order.fill.quantity} 股 · 费用 {order.fill.fee}
            </small>
            <small>经济时间 {order.fill.effectiveAt}</small>
            <small>记录时间 {order.fill.recordedAt}</small>
          </>
        ) : (
          "等待日线确认"
        )}
      </td>
      <td>
        {canCancel ? (
          <button
            type="button"
            disabled={mutation.busy}
            onClick={async () => {
              const r = await mutation.submit(
                "POST",
                "/accounts/" + accountId + "/orders/" + order.id + "/cancel",
                { expectedVersion: order.version },
              );
              if (
                r?.ok ||
                r?.code === "order_not_cancellable" ||
                r?.code === "version_conflict"
              )
                onChanged();
            }}
          >
            撤销订单
          </button>
        ) : null}
        {mutation.result && !mutation.result.ok ? (
          <p role="alert">{failureText(mutation.result.code)}</p>
        ) : null}
      </td>
    </tr>
  );
}
