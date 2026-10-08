import { fireEvent, render, screen } from "@testing-library/react";
import { expect, it, vi } from "vitest";
import { SyncForm, UniverseForm } from "./universe-form";
it("rejects 101 members and retains retry draft", async () => {
  const request = vi.fn().mockResolvedValue({ ok: false, code: "network" });
  render(<UniverseForm client={{ request }} onCreated={vi.fn()} />);
  fireEvent.change(screen.getByLabelText("股票池名称"), {
    target: { value: "长期观察" },
  });
  fireEvent.change(screen.getByLabelText("证券 ID"), {
    target: {
      value: Array.from({ length: 101 }, (_, i) => "id" + i).join(","),
    },
  });
  fireEvent.click(screen.getByRole("button", { name: "保存股票池" }));
  expect(await screen.findByRole("alert")).toHaveTextContent(/最多 100/);
  expect(
    request.mock.calls.filter(([, options]) => options.method === "POST"),
  ).toHaveLength(0);
  fireEvent.change(screen.getByLabelText("证券 ID"), {
    target: { value: "fixture-01,fixture-02" },
  });
  fireEvent.click(screen.getByRole("button", { name: "保存股票池" }));
  expect(await screen.findByRole("alert")).toHaveTextContent(/网络/);
  expect(screen.getByLabelText("股票池名称")).toHaveValue("长期观察");
});
it("shows translated sync progress states", async () => {
  const run = (status: string) => ({
    runId: "run-1",
    status,
    phase: "market",
    errorCode: "",
    reason: "",
    createdAt: "2026-10-06T21:00:00Z",
    updatedAt: "2026-10-06T21:00:00Z",
  });
  const request = vi
    .fn()
    .mockImplementation(async (path: string, options?: RequestInit) =>
      options?.method === "POST"
        ? { ok: true, value: run("queued") }
        : { ok: true, value: run("running") },
    );
  render(<SyncForm client={{ request }} />);
  fireEvent.change(screen.getByLabelText("股票代码或证券 ID"), {
    target: { value: "AAPL" },
  });
  fireEvent.click(screen.getByRole("button", { name: "开始同步" }));
  expect(await screen.findByText(/运行中/)).toBeVisible();
});
