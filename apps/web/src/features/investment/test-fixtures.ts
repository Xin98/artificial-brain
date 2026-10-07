import type { AccountView, OrderView } from "./types";
export function accountFixture(id = "one"): AccountView {
  return {
    id,
    name: id,
    mode: "fixture",
    feed: "synthetic",
    datasetVersion: "fixture/synthetic/v2",
    initialCash: "100000.00",
    cash: {
      available: "90000.00",
      reserved: "5000.00",
      unsettled: "4000.00",
      dividends: "1000.00",
    },
    nav: "100000.00",
    asOf: "2026-10-06T21:00:00Z",
    version: 1,
    automationEnabled: false,
    pauseReason: "",
    policy: {
      singleWeight: 0.1,
      industryWeight: 0.3,
      stockWeight: 0.8,
      drawdownPause: 0.15,
      stopLoss: 0.1,
      turnoverLimit: 0.2,
    },
    pendingConfig: null,
    qualityFlags: [],
    blockReasons: [],
    strategyVersionId: "strategy",
    universeVersionId: "universe",
    createdAt: "2026-10-01T00:00:00Z",
  };
}
export function orderFixture(state = "awaiting_bar"): OrderView {
  return {
    id: "order",
    accountId: "one",
    instrumentId: "fixture-01",
    side: "buy",
    state,
    reason: "",
    origin: "manual",
    quantity: "10",
    reservedQuantity: "0",
    reservedCash: "5000.00",
    capacity: "100",
    version: 1,
    targetOpenAt:
      state === "pending" ? "2099-01-01T14:30:00Z" : "2026-10-07T13:30:00Z",
    expiresAt: "2099-01-01T00:00:00Z",
    createdAt: "2026-10-06T21:00:00Z",
    fill: null,
  };
}
