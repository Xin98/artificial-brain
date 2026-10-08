import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { expect, it, vi } from "vitest";
import { AutomationForm } from "./automation-form";
import { accountFixture } from "./test-fixtures";
it("old config conflict cannot silently overwrite current version", async () => {
  const request = vi
    .fn()
    .mockImplementation(async (_path: string, o: RequestInit) =>
      o.method === "PUT"
        ? { ok: false, code: "version_conflict" }
        : { ok: true, value: { items: [], nextCursor: "" } },
    );
  render(
    <AutomationForm
      client={{ request }}
      account={accountFixture()}
      onChanged={vi.fn()}
    />,
  );
  fireEvent.click(screen.getByRole("button", { name: "启用自动交易" }));
  expect(await screen.findByRole("alert")).toHaveTextContent(/版本/);
  expect(screen.getByRole("button", { name: "启用自动交易" })).toBeDisabled();
  await waitFor(() =>
    expect(
      request.mock.calls.filter(([, o]) => o.method === "PUT"),
    ).toHaveLength(1),
  );
});
