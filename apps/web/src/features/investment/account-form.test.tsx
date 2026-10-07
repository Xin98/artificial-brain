import {
  act,
  fireEvent,
  render,
  screen,
  waitFor,
} from "@testing-library/react";
import { expect, it, vi } from "vitest";
import { AccountForm } from "./account-form";
import { accountFixture } from "./test-fixtures";
it("double click and timeout retry use same key and unchanged body", async () => {
  let finish: (v: unknown) => void = () => {};
  const request = vi
    .fn()
    .mockImplementation((path: string, options: RequestInit) =>
      options.method === "POST"
        ? new Promise((r) => {
            finish = r;
          })
        : Promise.resolve({ ok: true, value: { items: [], nextCursor: "" } }),
    );
  const created = vi.fn();
  render(<AccountForm client={{ request }} onCreated={created} />);
  fireEvent.change(screen.getByLabelText("账户名称"), {
    target: { value: "长期" },
  });
  const button = screen.getByRole("button", { name: "创建模拟账户" });
  fireEvent.click(button);
  fireEvent.click(button);
  await waitFor(() =>
    expect(
      request.mock.calls.filter(([, o]) => o.method === "POST"),
    ).toHaveLength(1),
  );
  await act(async () => finish({ ok: false, code: "timeout" }));
  fireEvent.click(button);
  await waitFor(() =>
    expect(
      request.mock.calls.filter(([, o]) => o.method === "POST"),
    ).toHaveLength(2),
  );
  const calls = request.mock.calls.filter(([, o]) => o.method === "POST");
  expect(calls[0][1].headers["Idempotency-Key"]).toBe(
    calls[1][1].headers["Idempotency-Key"],
  );
  expect(calls[0][1].body).toBe(calls[1][1].body);
  await act(async () => finish({ ok: true, value: accountFixture() }));
  expect(created).toHaveBeenCalledTimes(1);
});
