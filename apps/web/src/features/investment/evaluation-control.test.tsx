import { fireEvent, render, screen } from "@testing-library/react";
import { expect, it, vi } from "vitest";
import { EvaluationControl } from "./evaluation-control";
it("shows translated evaluation progress", async () => {
  const request = vi
    .fn()
    .mockImplementation(async (path: string, options?: RequestInit) =>
      options?.method === "POST"
        ? {
            ok: true,
            value: {
              runId: "ev-1",
              status: "queued",
              phase: "",
              errorCode: "",
              reason: "",
              createdAt: "2026-10-06T21:00:00Z",
              updatedAt: "2026-10-06T21:00:00Z",
            },
          }
        : {
            ok: true,
            value: {
              id: "ev-1",
              accountId: "one",
              snapshotId: "s",
              datasetVersion: "v",
              mode: "fixture",
              strategyVersionId: "s",
              universeVersionId: "u",
              state: "completed",
              reason: "exit_signal",
              purpose: "research",
              asOf: "2026-10-06T21:00:00Z",
              sessionDate: "2026-10-06",
              signals: [],
              excluded: [],
              qualityFlags: [],
              orderIds: [],
              issuedOrders: [],
            },
          },
    );
  render(<EvaluationControl client={{ request }} accountId="one" />);
  fireEvent.click(screen.getByRole("button", { name: "运行研究评估" }));
  expect(await screen.findByText(/已完成/)).toBeVisible();
  expect(screen.getByText(/量化评分低于退出阈值/)).toBeVisible();
});
