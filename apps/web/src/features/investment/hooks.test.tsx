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
it("last valid data survives transient failure but clears on ownership or login failure", async () => {
  for (const code of ["not_found", "unauthenticated"]) {
    const request = vi
      .fn()
      .mockResolvedValueOnce({ ok: true, value: "owned" })
      .mockResolvedValueOnce({ ok: false, code: "service_unavailable" })
      .mockResolvedValueOnce({ ok: false, code });
    const client = { request };
    const { result, unmount } = renderHook(() =>
      useResource<string>(client, "/accounts/one", "AccountView"),
    );
    await waitFor(() => expect(result.current.lastGood).toBe("owned"));
    act(() => result.current.retry());
    await waitFor(() =>
      expect(result.current.result).toEqual({
        ok: false,
        code: "service_unavailable",
      }),
    );
    expect(result.current.lastGood).toBe("owned");
    act(() => result.current.retry());
    await waitFor(() =>
      expect(result.current.result).toEqual({ ok: false, code }),
    );
    expect(result.current.lastGood).toBeNull();
    unmount();
  }
});
