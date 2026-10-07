import { act, renderHook, waitFor } from "@testing-library/react";
import { expect, it, vi } from "vitest";
import { useResource } from "./hooks";
it("visibility polling ignores old responses and stops after unmount", async () => {
  let finish: (v: unknown) => void = () => {};
  const request = vi
    .fn()
    .mockImplementationOnce(
      () =>
        new Promise((r) => {
          finish = r;
        }),
    )
    .mockResolvedValue({ ok: true, value: "new" });
  const visibility = vi
    .spyOn(document, "visibilityState", "get")
    .mockReturnValue("visible");
  const client = { request };
  const { result, unmount } = renderHook(() =>
    useResource<string>(client, "/accounts", "Money", true),
  );
  // Keep the client stable across hook renders.
  visibility.mockReturnValue("hidden");
  act(() => document.dispatchEvent(new Event("visibilitychange")));
  visibility.mockReturnValue("visible");
  act(() => document.dispatchEvent(new Event("visibilitychange")));
  await waitFor(() =>
    expect(result.current.result).toEqual({ ok: true, value: "new" }),
  );
  await act(async () => finish({ ok: true, value: "old" }));
  expect(result.current.result).toEqual({ ok: true, value: "new" });
  unmount();
  const count = request.mock.calls.length;
  act(() => document.dispatchEvent(new Event("visibilitychange")));
  expect(request).toHaveBeenCalledTimes(count);
  visibility.mockRestore();
});
