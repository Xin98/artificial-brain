import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { beforeEach, expect, it, vi } from "vitest";

import { DataPanel } from "./data-panel";

// The upload 201 body: importId next to the six-key preview. Counts are
// chosen distinct so each tile can be asserted unambiguously.
const uploadBody = {
  importId: "import-1",
  preview: {
    new: 3,
    skipped: 1,
    conflicts: 2,
    invalid: 0,
    details: [],
    truncated: false,
  },
};

const reportBody = {
  new: 3,
  skipped: 1,
  conflicts: 2,
  invalid: 0,
  details: [],
  truncated: false,
  committedAt: "2026-08-21T03:00:00Z",
};

function json(status: number, body: unknown): Response {
  return new Response(JSON.stringify(body), {
    status,
    headers: { "content-type": "application/json" },
  });
}

function selectBundleFile(): void {
  fireEvent.change(screen.getByLabelText(/选择导出包/), {
    target: { files: [new File(["zip-bytes"], "export.zip")] },
  });
}

// jsdom does not implement URL.createObjectURL; the panel's export download
// runs against these stubs.
const createObjectURL = vi.fn((): string => "blob:export-bundle");
const revokeObjectURL = vi.fn();

beforeEach(() => {
  createObjectURL.mockClear();
  revokeObjectURL.mockClear();
  Object.defineProperty(URL, "createObjectURL", {
    configurable: true,
    writable: true,
    value: createObjectURL,
  });
  Object.defineProperty(URL, "revokeObjectURL", {
    configurable: true,
    writable: true,
    value: revokeObjectURL,
  });
});

it("renders the export button and the bundle file input", () => {
  render(<DataPanel />);

  expect(
    screen.getByRole("button", { name: "导出全部数据" }),
  ).toBeInTheDocument();
  expect(screen.getByLabelText(/选择导出包/)).toBeInTheDocument();
});

it("uploads the bundle and renders the preview counts with a confirm button", async () => {
  const fetcher = vi.fn().mockResolvedValue(json(201, uploadBody));
  render(<DataPanel fetcher={fetcher as unknown as typeof fetch} />);

  selectBundleFile();

  await waitFor(() =>
    expect(screen.getByText("新增").parentElement).toHaveTextContent("3"),
  );
  expect(screen.getByText("跳过").parentElement).toHaveTextContent("1");
  expect(screen.getByText("冲突").parentElement).toHaveTextContent("2");
  expect(screen.getByText("无效").parentElement).toHaveTextContent("0");
  expect(screen.getByRole("button", { name: "确认导入" })).toBeInTheDocument();
  expect(fetcher).toHaveBeenCalledWith(
    "/api/v1/portability/imports",
    expect.any(Object),
  );
});

it("confirms the import and renders the report", async () => {
  const fetcher = vi
    .fn()
    .mockResolvedValueOnce(json(201, uploadBody))
    .mockResolvedValueOnce(json(200, reportBody));
  render(<DataPanel fetcher={fetcher as unknown as typeof fetch} />);

  selectBundleFile();
  await waitFor(() =>
    expect(
      screen.getByRole("button", { name: "确认导入" }),
    ).toBeInTheDocument(),
  );
  fireEvent.click(screen.getByRole("button", { name: "确认导入" }));

  await waitFor(() =>
    expect(screen.getByRole("status")).toHaveTextContent("导入完成"),
  );
  expect(screen.getByText(/2026-08-21T03:00:00Z/)).toBeInTheDocument();
  expect(screen.getByText("新增").parentElement).toHaveTextContent("3");
  expect(fetcher).toHaveBeenCalledWith(
    "/api/v1/portability/imports/import-1/confirm",
    expect.any(Object),
  );
});

it.each([
  ["bundle_too_large", "导出包超过上限，请拆分后重试"],
  ["checksum_mismatch", "导出包已损坏，请重新导出"],
  ["unsupported_schema_version", "导出包版本不受支持"],
  ["bundle_invalid", "导出包内容无效"],
])("maps the %s upload envelope to its Chinese copy", async (code, copy) => {
  const fetcher = vi
    .fn()
    .mockResolvedValue(
      json(422, { code, message: "rejected", correlationId: "corr-1" }),
    );
  render(<DataPanel fetcher={fetcher as unknown as typeof fetch} />);

  selectBundleFile();

  await waitFor(() =>
    expect(screen.getByRole("alert")).toHaveTextContent(copy),
  );
});

it("maps a confirm conflict to the import_conflict copy", async () => {
  const fetcher = vi
    .fn()
    .mockResolvedValueOnce(json(201, uploadBody))
    .mockResolvedValueOnce(
      json(409, {
        code: "import_conflict",
        message: "import is already committed",
        correlationId: "corr-2",
      }),
    );
  render(<DataPanel fetcher={fetcher as unknown as typeof fetch} />);

  selectBundleFile();
  await waitFor(() =>
    expect(
      screen.getByRole("button", { name: "确认导入" }),
    ).toBeInTheDocument(),
  );
  fireEvent.click(screen.getByRole("button", { name: "确认导入" }));

  await waitFor(() =>
    expect(screen.getByRole("alert")).toHaveTextContent("该导入已确认或已过期"),
  );
});

it("maps an unknown code to the generic fallback copy", async () => {
  const fetcher = vi
    .fn()
    .mockResolvedValue(
      json(422, { code: "mystery", message: "?", correlationId: "corr-3" }),
    );
  render(<DataPanel fetcher={fetcher as unknown as typeof fetch} />);

  selectBundleFile();

  await waitFor(() =>
    expect(screen.getByRole("alert")).toHaveTextContent("操作失败"),
  );
});

it("caps the preview details and shows the truncated note", async () => {
  const details = Array.from({ length: 25 }, (_unused, index) => ({
    kind: "todo",
    sourceRecordId: `src-${index + 1}`,
    outcome: "new",
    reason: "首次导入",
  }));
  const fetcher = vi.fn().mockResolvedValue(
    json(201, {
      importId: "import-1",
      preview: { ...uploadBody.preview, details, truncated: true },
    }),
  );
  render(<DataPanel fetcher={fetcher as unknown as typeof fetch} />);

  selectBundleFile();

  await waitFor(() =>
    expect(screen.getByText(/仅显示前 20 条/)).toBeInTheDocument(),
  );
  expect(screen.getAllByText(/首次导入/)).toHaveLength(20);
  expect(screen.queryByText(/src-21/)).not.toBeInTheDocument();
});

it("shows the export error when the export request fails", async () => {
  const fetcher = vi
    .fn()
    .mockResolvedValue(new Response("{}", { status: 500 }));
  render(<DataPanel fetcher={fetcher as unknown as typeof fetch} />);

  fireEvent.click(screen.getByRole("button", { name: "导出全部数据" }));

  await waitFor(() =>
    expect(screen.getByRole("alert")).toHaveTextContent("导出失败"),
  );
});

it("downloads a successful export through a transient object URL", async () => {
  const fetcher = vi.fn().mockResolvedValue(
    new Response("zip-bytes", {
      status: 200,
      headers: { "content-type": "application/zip" },
    }),
  );
  render(<DataPanel fetcher={fetcher as unknown as typeof fetch} />);

  fireEvent.click(screen.getByRole("button", { name: "导出全部数据" }));

  await waitFor(() =>
    expect(createObjectURL).toHaveBeenCalledWith(expect.any(Blob)),
  );
  expect(revokeObjectURL).toHaveBeenCalledWith("blob:export-bundle");
});
