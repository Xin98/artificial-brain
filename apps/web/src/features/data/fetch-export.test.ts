import { describe, expect, it, vi } from "vitest";

import { exportBundle } from "./fetch-export";

describe("exportBundle", () => {
  it("returns the zip blob for a 200 response", async () => {
    const result = await exportBundle(
      "",
      vi.fn().mockResolvedValue(
        new Response("zip-bytes", {
          status: 200,
          headers: { "content-type": "application/zip" },
        }),
      ),
    );

    expect(result).toBeInstanceOf(Blob);
    expect(await result?.text()).toBe("zip-bytes");
  });

  it("POSTs to the export endpoint", async () => {
    const fetcher = vi
      .fn()
      .mockResolvedValue(new Response("zip-bytes", { status: 200 }));

    await exportBundle("", fetcher);

    expect(fetcher).toHaveBeenCalledWith(
      "/api/v1/portability/export",
      expect.objectContaining({ method: "POST" }),
    );
  });

  it("fails closed for a non-2xx response", async () => {
    const result = await exportBundle(
      "",
      vi.fn().mockResolvedValue(new Response("{}", { status: 500 })),
    );

    expect(result).toBeNull();
  });

  it("fails closed for a network error", async () => {
    const result = await exportBundle(
      "",
      vi.fn().mockRejectedValue(new Error("ECONNREFUSED")),
    );

    expect(result).toBeNull();
  });
});
