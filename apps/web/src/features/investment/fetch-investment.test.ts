import { expect, it, vi } from "vitest";
import {
  createInvestmentClient,
  decode,
  createIntent,
  failureText,
} from "./fetch-investment";
import { localTime } from "./format";
it("translates investment error codes into actionable Chinese messages", () => {
  expect(failureText("risk_limit_exceeded")).toContain("风控上限");
  expect(failureText("data_stale")).not.toContain("data_stale");
  expect(failureText("insufficient_cash")).toContain("资金不足");
  expect(failureText("order_not_cancellable")).toContain("不能撤销");
  expect(failureText("something_unknown")).toContain("请求未完成");
});
it("localTime renders instants in the browser timezone with UTC fallback", () => {
  const rendered = localTime("2026-10-08T13:30:00Z");
  expect(rendered).not.toBe("2026-10-08T13:30:00Z");
  expect(rendered).toContain("10");
  expect(localTime("not-a-date")).toBe("not-a-date");
});
it("does not treat 503 as expired session", async () => {
  const navigate = vi.fn();
  const fetcher = vi
    .fn()
    .mockResolvedValue(
      new Response(
        JSON.stringify({ code: "service_unavailable", correlationId: "c" }),
        { status: 503 },
      ),
    );
  const client = createInvestmentClient("", fetcher, navigate);
  const result = await client.request("/accounts", {}, decode("Money"));
  expect(result.ok).toBe(false);
  expect(navigate).not.toHaveBeenCalled();
});
it("rejects numeric money and preserves decimal strings", async () => {
  const fetcher = vi
    .fn()
    .mockResolvedValueOnce(new Response("100"))
    .mockResolvedValueOnce(new Response('"100.00"'));
  const client = createInvestmentClient("", fetcher);
  expect((await client.request("/accounts", {}, decode("Money"))).ok).toBe(
    false,
  );
  expect(await client.request("/accounts", {}, decode("Money"))).toEqual({
    ok: true,
    value: "100.00",
  });
});
it("one unchanged intent keeps its key after network failure", async () => {
  const fetcher = vi
    .fn()
    .mockRejectedValueOnce(new TypeError("network"))
    .mockResolvedValue(new Response('"1.00"'));
  const client = createInvestmentClient("", fetcher);
  const intent = createIntent("POST", "/accounts", { initialCash: "100.00" });
  await client.request(intent.path, intent.options, decode("Money"));
  await client.request(intent.path, intent.options, decode("Money"));
  expect(fetcher.mock.calls[0][1].headers["Idempotency-Key"]).toBe(
    fetcher.mock.calls[1][1].headers["Idempotency-Key"],
  );
  expect(fetcher.mock.calls[0][1].body).toBe(fetcher.mock.calls[1][1].body);
});
