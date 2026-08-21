import { describe, expect, it, vi } from "vitest";

import { confirmImport, uploadImportBundle } from "./import-flow";

// The wire shapes from the portability contract: the upload 201 body wraps
// the six-key preview next to the importId, and the confirm 200 body is the
// seven-key ImportReport (details and truncated included).
const wirePreview = {
  new: 3,
  skipped: 1,
  conflicts: 2,
  invalid: 0,
  details: [
    {
      kind: "todo",
      sourceRecordId: "src-todo-1",
      outcome: "new",
      reason: "首次导入",
    },
    {
      kind: "channel",
      sourceRecordId: "src-channel-1",
      outcome: "skipped",
      reason: "已导入过",
    },
  ],
  truncated: false,
};

const wireReport = {
  ...wirePreview,
  committedAt: "2026-08-21T03:00:00Z",
};

function json(status: number, body: unknown): Response {
  return new Response(JSON.stringify(body), {
    status,
    headers: { "content-type": "application/json" },
  });
}

const bundleFile = new File(["zip-bytes"], "export.zip", {
  type: "application/zip",
});

describe("uploadImportBundle", () => {
  it("POSTs the file as multipart field bundle and parses the preview", async () => {
    const fetcher = vi
      .fn()
      .mockResolvedValue(
        json(201, { importId: "import-1", preview: wirePreview }),
      );

    const result = await uploadImportBundle("", fetcher, bundleFile);

    expect(fetcher).toHaveBeenCalledWith(
      "/api/v1/portability/imports",
      expect.any(Object),
    );
    const init = fetcher.mock.calls[0][1];
    expect(init?.body).toBeInstanceOf(FormData);
    expect((init?.body as FormData).get("bundle")).toBe(bundleFile);
    expect(result).toEqual({
      ok: true,
      preview: { importId: "import-1", ...wirePreview },
    });
  });

  it("surfaces the envelope code for a 422 rejection", async () => {
    const result = await uploadImportBundle(
      "",
      vi.fn().mockResolvedValue(
        json(422, {
          code: "bundle_too_large",
          message: "export bundle exceeds the size limit",
          correlationId: "corr-1",
        }),
      ),
      bundleFile,
    );

    expect(result).toEqual({ ok: false, code: "bundle_too_large" });
  });

  it("fails closed when the preview misses a key", async () => {
    const { truncated: _truncated, ...legacyPreview } = wirePreview;
    const result = await uploadImportBundle(
      "",
      vi
        .fn()
        .mockResolvedValue(
          json(201, { importId: "import-1", preview: legacyPreview }),
        ),
      bundleFile,
    );

    expect(result).toEqual({ ok: false, code: "unknown" });
  });

  it("fails closed when a decision carries an unknown outcome", async () => {
    const preview = {
      ...wirePreview,
      details: [
        {
          kind: "todo",
          sourceRecordId: "src-todo-1",
          outcome: "merged",
          reason: "not a contract outcome",
        },
      ],
    };
    const result = await uploadImportBundle(
      "",
      vi.fn().mockResolvedValue(json(201, { importId: "import-1", preview })),
      bundleFile,
    );

    expect(result).toEqual({ ok: false, code: "unknown" });
  });

  it("fails closed for a network error", async () => {
    const result = await uploadImportBundle(
      "",
      vi.fn().mockRejectedValue(new Error("ECONNREFUSED")),
      bundleFile,
    );

    expect(result).toEqual({ ok: false, code: "unknown" });
  });

  it("fails closed for a non-2xx response without a parseable envelope", async () => {
    const result = await uploadImportBundle(
      "",
      vi.fn().mockResolvedValue(new Response("not-json", { status: 500 })),
      bundleFile,
    );

    expect(result).toEqual({ ok: false, code: "unknown" });
  });
});

describe("confirmImport", () => {
  it("returns the report for a 200 response", async () => {
    const result = await confirmImport(
      "",
      vi.fn().mockResolvedValue(json(200, wireReport)),
      "import-1",
    );

    expect(result).toEqual({
      ok: true,
      report: {
        new: 3,
        skipped: 1,
        conflicts: 2,
        invalid: 0,
        committedAt: "2026-08-21T03:00:00Z",
      },
    });
  });

  it("POSTs to the import's confirm path", async () => {
    const fetcher = vi.fn().mockResolvedValue(json(200, wireReport));

    await confirmImport("", fetcher, "import-1");

    expect(fetcher).toHaveBeenCalledWith(
      "/api/v1/portability/imports/import-1/confirm",
      expect.objectContaining({ method: "POST" }),
    );
  });

  it("maps a 409 to the import_conflict code", async () => {
    const result = await confirmImport(
      "",
      vi.fn().mockResolvedValue(
        json(409, {
          code: "import_conflict",
          message: "import is already committed",
          correlationId: "corr-2",
        }),
      ),
      "import-1",
    );

    expect(result).toEqual({ ok: false, code: "import_conflict" });
  });

  it("fails closed when the report misses committedAt", async () => {
    const { committedAt: _committedAt, ...noTimestamp } = wireReport;
    const result = await confirmImport(
      "",
      vi.fn().mockResolvedValue(json(200, noTimestamp)),
      "import-1",
    );

    expect(result).toEqual({ ok: false, code: "unknown" });
  });

  it("fails closed when the report carries an unknown key", async () => {
    const result = await confirmImport(
      "",
      vi.fn().mockResolvedValue(json(200, { ...wireReport, extra: 1 })),
      "import-1",
    );

    expect(result).toEqual({ ok: false, code: "unknown" });
  });

  it("fails closed for a network error", async () => {
    const result = await confirmImport(
      "",
      vi.fn().mockRejectedValue(new Error("ECONNREFUSED")),
      "import-1",
    );

    expect(result).toEqual({ ok: false, code: "unknown" });
  });
});
