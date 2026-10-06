"use client";

import { useState } from "react";

import { exportBundle } from "./fetch-export";
import { confirmImport, uploadImportBundle } from "./import-flow";
import type {
  ImportDecision,
  ImportPreview,
  ImportReport,
} from "./import-flow";

// Portability envelope codes mapped to actionable copy; the strings are the
// iteration's agreed wording, verbatim.
const ERROR_COPY: Record<string, string> = {
  bundle_too_large:
    "导出包超过本实例的上传上限。请联系实例管理员提高导入容量后重试。",
  checksum_mismatch: "导出包已损坏，请重新导出",
  unsupported_schema_version: "导出包版本不受支持",
  bundle_invalid: "导出包内容无效",
  import_conflict: "该导入已确认或已过期",
  not_found: "导入预览已失效或不属于当前账户，请重新选择文件生成预览。",
};

const FALLBACK_COPY = "操作失败,请稍后再试。";

const OUTCOME_LABELS: Record<ImportDecision["outcome"], string> = {
  new: "新增",
  skipped: "跳过",
  conflict: "冲突",
  invalid: "无效",
};

const KIND_LABELS: Record<string, string> = {
  todo: "待办",
  channel: "渠道",
  delivery: "提醒记录",
  session: "会话",
  message: "消息",
};

// DETAILS_LIMIT caps the decision list the panel renders; the server caps
// the stored list too, so this only bounds the DOM.
const DETAILS_LIMIT = 20;

type Phase = "idle" | "previewing" | "preview" | "confirming" | "report";

function errorCopy(code: string): string {
  return ERROR_COPY[code] ?? FALLBACK_COPY;
}

function reasonCopy(reason: string): string {
  const labels: Record<string, string> = {
    "fingerprint unchanged since last import": "与上次导入内容相同",
    "fingerprint changed since last import": "与已导入内容不同，保留现有版本",
    "channel already exists": "联系渠道已存在",
    todo_not_found: "关联待办未恢复",
    legacy_source_owner_unverified:
      "此工作区已导入过该记录，无法核实旧记录的账户归属；请联系管理员核对，已保留原数据",
    session_not_found: "关联会话不存在",
    session_not_found_or_conflict: "关联会话未恢复或有冲突",
    "duplicate session id": "会话编号重复",
    "duplicate message id or order": "消息编号或顺序重复",
  };
  return labels[reason] ?? reason;
}

function exportFilename(): string {
  const now = new Date();
  const pad = (value: number): string => String(value).padStart(2, "0");
  return (
    `artificial-brain-export-${now.getFullYear()}` +
    `${pad(now.getMonth() + 1)}${pad(now.getDate())}.zip`
  );
}

// downloadBundle saves the blob through a transient object URL: anchor
// click, then revoke.
function downloadBundle(blob: Blob): void {
  const url = URL.createObjectURL(blob);
  const anchor = document.createElement("a");
  anchor.href = url;
  anchor.download = exportFilename();
  document.body.appendChild(anchor);
  anchor.click();
  anchor.remove();
  URL.revokeObjectURL(url);
}

// DataPanel walks the two data portability flows. Export downloads the
// workspace bundle; import is two-phase: upload shows the preview, confirm
// commits it. Every step fails closed with an inline message.
export function DataPanel({
  fetcher = fetch,
}: {
  fetcher?: typeof fetch;
}): React.JSX.Element {
  const [phase, setPhase] = useState<Phase>("idle");
  const [preview, setPreview] = useState<ImportPreview | null>(null);
  const [report, setReport] = useState<ImportReport | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [exporting, setExporting] = useState(false);

  function restartImport(): void {
    setPreview(null);
    setReport(null);
    setError(null);
    setPhase("idle");
  }

  async function handleExport(): Promise<void> {
    setExporting(true);
    setError(null);
    const blob = await exportBundle("", fetcher);
    setExporting(false);
    if (blob === null) {
      setError("导出失败,请稍后再试。");
      return;
    }
    downloadBundle(blob);
  }

  async function handleBundleChange(
    event: React.ChangeEvent<HTMLInputElement>,
  ): Promise<void> {
    const file = event.target.files?.[0];
    event.target.value = "";
    if (file === undefined) {
      return;
    }
    setError(null);
    setPreview(null);
    setReport(null);
    setPhase("previewing");
    const result = await uploadImportBundle("", fetcher, file);
    if (!result.ok) {
      setError(errorCopy(result.code));
      setPhase("idle");
      return;
    }
    setPreview(result.preview);
    setPhase("preview");
  }

  async function handleConfirm(): Promise<void> {
    if (preview === null) {
      return;
    }
    setError(null);
    setPhase("confirming");
    const result = await confirmImport("", fetcher, preview.importId);
    if (!result.ok) {
      setError(
        result.code === "unknown"
          ? "导入结果尚未确认，请再次确认同一导入以核对结果。不要重新上传文件。"
          : errorCopy(result.code),
      );
      if (result.code === "import_conflict" || result.code === "not_found") {
        setPreview(null);
        setPhase("idle");
      } else {
        setPhase("preview");
      }
      return;
    }
    setPreview(null);
    setReport(result.report);
    setPhase("report");
  }

  return (
    <section aria-label="数据携带" className="data-panel">
      {error ? (
        <p aria-live="polite" className="data-error" role="alert">
          {error}
        </p>
      ) : null}

      <div className="data-export">
        <h2>导出数据</h2>
        <p>
          下载当前账户的待办、联系渠道偏好、会话与完整消息历史，以及当前工作区的提醒记录。导入仅恢复历史，不会重新执行会话操作或发送旧提醒。
        </p>
        <button
          className="btn-primary"
          disabled={exporting}
          onClick={() => void handleExport()}
          type="button"
        >
          {exporting ? "导出中…" : "导出全部数据"}
        </button>
      </div>

      <div className="data-import">
        <h2>导入数据</h2>
        {phase === "idle" || phase === "previewing" ? (
          <>
            <div className="field">
              <label htmlFor="bundle-file">选择导出包(.zip)</label>
              <input
                accept=".zip"
                disabled={phase === "previewing"}
                id="bundle-file"
                onChange={(event) => void handleBundleChange(event)}
                type="file"
              />
            </div>
            {phase === "previewing" ? (
              <p role="status">正在解析导出包…</p>
            ) : null}
          </>
        ) : null}

        {phase === "preview" && preview !== null ? (
          <>
            <CountList counts={preview} />
            <p>仅新增记录会被恢复；冲突和无效记录不会覆盖现有数据。</p>
            {preview.details.length > 0 ? (
              <ul className="data-details">
                {preview.details.slice(0, DETAILS_LIMIT).map((decision) => (
                  <li
                    key={`${decision.kind}-${decision.label || decision.sourceRecordId}`}
                  >
                    <span className="data-detail-kind">
                      {KIND_LABELS[decision.kind] ?? decision.kind}
                    </span>
                    <span className="data-detail-id">
                      {decision.label || decision.sourceRecordId}
                    </span>
                    <span className="data-detail-outcome">
                      {OUTCOME_LABELS[decision.outcome]}
                    </span>
                    {decision.reason ? (
                      <span>{reasonCopy(decision.reason)}</span>
                    ) : null}
                  </li>
                ))}
              </ul>
            ) : null}
            {preview.truncated || preview.details.length > DETAILS_LIMIT ? (
              <p className="data-truncated" role="status">
                明细较多,仅显示前 {DETAILS_LIMIT} 条。
              </p>
            ) : null}
            <button
              className="btn-primary"
              onClick={() => void handleConfirm()}
              type="button"
            >
              确认导入
            </button>
            <button
              className="btn-secondary"
              onClick={restartImport}
              type="button"
            >
              取消导入 / 更换文件
            </button>
          </>
        ) : null}

        {phase === "confirming" ? <p role="status">正在导入…</p> : null}

        {phase === "report" && report !== null ? (
          <>
            <p role="status">
              {report.conflicts > 0 || report.invalid > 0
                ? "导入已提交，部分记录未导入"
                : "导入完成"}
              ，提交于 {new Date(report.committedAt).toLocaleString()}。
            </p>
            <CountList counts={report} />
            {report.conflicts > 0 || report.invalid > 0 ? (
              <p>
                冲突记录保留了现有版本；无效记录未恢复。请根据下方明细检查原始数据后重新导出。
              </p>
            ) : null}
            {(report.details ?? []).length > 0 ? (
              <ul className="data-details">
                {(report.details ?? [])
                  .slice(0, DETAILS_LIMIT)
                  .map((decision) => (
                    <li key={`${decision.kind}-${decision.sourceRecordId}`}>
                      <span className="data-detail-kind">
                        {KIND_LABELS[decision.kind] ?? decision.kind}
                      </span>
                      <span className="data-detail-id">
                        {decision.label || decision.sourceRecordId}
                      </span>
                      <span className="data-detail-outcome">
                        {OUTCOME_LABELS[decision.outcome]}
                      </span>
                      {decision.reason ? (
                        <span>{reasonCopy(decision.reason)}</span>
                      ) : null}
                    </li>
                  ))}
              </ul>
            ) : null}
            {report.truncated ||
            (report.details ?? []).length > DETAILS_LIMIT ? (
              <p className="data-truncated">
                明细较多，仅显示前 {DETAILS_LIMIT} 条。
              </p>
            ) : null}
            <button
              className="btn-secondary"
              onClick={restartImport}
              type="button"
            >
              继续导入其他文件
            </button>
          </>
        ) : null}
      </div>
    </section>
  );
}

function CountList({
  counts,
}: {
  counts: { new: number; skipped: number; conflicts: number; invalid: number };
}): React.JSX.Element {
  const rows = [
    { label: "新增", value: counts.new },
    { label: "跳过", value: counts.skipped },
    { label: "冲突", value: counts.conflicts },
    { label: "无效", value: counts.invalid },
  ];
  return (
    <ul className="data-counts">
      {rows.map((row) => (
        <li key={row.label}>
          <span>{row.label}</span>
          <span>{row.value}</span>
        </li>
      ))}
    </ul>
  );
}
