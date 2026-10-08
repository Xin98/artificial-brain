import { act, renderHook, waitFor } from "@testing-library/react";
import { expect, it, vi } from "vitest";
import { useCursorPager, useDebouncedValue, useResource } from "./hooks";

it("debounced value trails rapid changes", () => {
  vi.useFakeTimers();
  try {
    const { result, rerender } = renderHook(
      ({ value }) => useDebouncedValue(value, 300),
      { initialProps: { value: "a" } },
    );
    expect(result.current).toBe("a");
    rerender({ value: "ab" });
    rerender({ value: "abc" });
    act(() => {
      vi.advanceTimersByTime(299);
    });
    expect(result.current).toBe("a");
    act(() => {
      vi.advanceTimersByTime(1);
    });
    expect(result.current).toBe("abc");
  } finally {
    vi.useRealTimers();
  }
});

it("cursor pager walks back through visited pages and resets", () => {
  const { result } = renderHook(() => useCursorPager());
  expect(result.current.cursor).toBe("");
  expect(result.current.canPrev).toBe(false);
  act(() => result.current.next("c2"));
  expect(result.current.cursor).toBe("c2");
  expect(result.current.canPrev).toBe(true);
  act(() => result.current.next("c3"));
  act(() => result.current.prev());
  expect(result.current.cursor).toBe("c2");
  act(() => result.current.prev());
  expect(result.current.cursor).toBe("");
  expect(result.current.canPrev).toBe(false);
  act(() => result.current.next("c2"));
  act(() => result.current.reset());
  expect(result.current.cursor).toBe("");
  expect(result.current.canPrev).toBe(false);
});

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
it("lastOk keeps the previous success across query changes", async () => {
  const request = vi
    .fn()
    .mockImplementation((path: string) =>
      Promise.resolve({ ok: true, value: "value-of-" + path }),
    );
  const { result, rerender } = renderHook(
    ({ path }) => useResource<string>({ request }, path, "InstrumentsPage"),
    { initialProps: { path: "/a" } },
  );
  await waitFor(() =>
    expect(result.current.result).toEqual({ ok: true, value: "value-of-/a" }),
  );
  rerender({ path: "/b" });
  expect(result.current.result).toBeNull();
  expect(result.current.lastOk).toBe("value-of-/a");
  await waitFor(() =>
    expect(result.current.result).toEqual({ ok: true, value: "value-of-/b" }),
  );
  expect(result.current.lastOk).toBe("value-of-/b");
});
