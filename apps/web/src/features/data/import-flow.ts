import {
  hasAllowedKeys,
  hasExactKeys,
  isBoolean,
  isInteger,
  isNonEmptyString,
  isRecord,
  isRFC3339,
  readErrorPayload,
  safeTimeout,
} from "../validation";

export interface ImportDecision {
  kind: string;
  sourceRecordId: string;
  outcome: "new" | "skipped" | "conflict" | "invalid";
  reason?: string;
}

export interface ImportPreview {
  importId: string;
  new: number;
  skipped: number;
  conflicts: number;
  invalid: number;
  details: ImportDecision[];
  truncated: boolean;
}

// ImportReport is the panel-facing projection of the committed report: the
// four counts plus the commit timestamp.
export interface ImportReport {
  new: number;
  skipped: number;
  conflicts: number;
  invalid: number;
  committedAt: string;
}

export type UploadResult =
  { ok: true; preview: ImportPreview } | { ok: false; code: string };

export type ConfirmResult =
  { ok: true; report: ImportReport } | { ok: false; code: string };

// UNKNOWN_CODE marks fail-closed outcomes: network errors, unparseable
// envelopes, and success bodies that fail validation. The panel maps it to
// the generic retry copy.
export const UNKNOWN_CODE = "unknown";

const DECISION_OUTCOMES = ["new", "skipped", "conflict", "invalid"];

// The confirm report's wire keys per the portability contract. The five
// required keys are the panel-facing report; details and truncated always
// ride along on the wire and are validated when present.
const REPORT_REQUIRED_KEYS = [
  "new",
  "skipped",
  "conflicts",
  "invalid",
  "committedAt",
] as const;

const REPORT_OPTIONAL_KEYS = ["details", "truncated"] as const;

// uploadImportBundle POSTs the bundle as multipart field "bundle" and parses
// the 201 preview. Rejections surface the error envelope's code; anything
// unparseable fails closed to UNKNOWN_CODE — nothing is confirmed against a
// preview we could not validate.
export async function uploadImportBundle(
  baseURL: string,
  fetcher: typeof fetch,
  file: File,
  timeoutMs = 30000,
): Promise<UploadResult> {
  try {
    const form = new FormData();
    form.append("bundle", file);
    const response = await fetcher(`${baseURL}/api/v1/portability/imports`, {
      method: "POST",
      signal: AbortSignal.timeout(safeTimeout(timeoutMs)),
      cache: "no-store",
      headers: { accept: "application/json" },
      body: form,
    });
    if (response.status === 201) {
      const payload: unknown = await response.json();
      const preview = toImportPreview(payload);
      if (preview === null) {
        return { ok: false, code: UNKNOWN_CODE };
      }
      return { ok: true, preview };
    }
    return { ok: false, code: await readErrorCode(response) };
  } catch {
    return { ok: false, code: UNKNOWN_CODE };
  }
}

// confirmImport executes the pending import exactly once. A 409 means the
// import was already committed or has expired; the panel says so.
export async function confirmImport(
  baseURL: string,
  fetcher: typeof fetch,
  importId: string,
  timeoutMs = 30000,
): Promise<ConfirmResult> {
  try {
    const response = await fetcher(
      `${baseURL}/api/v1/portability/imports/${encodeURIComponent(importId)}/confirm`,
      {
        method: "POST",
        signal: AbortSignal.timeout(safeTimeout(timeoutMs)),
        cache: "no-store",
        headers: { accept: "application/json" },
      },
    );
    if (response.status === 200) {
      const payload: unknown = await response.json();
      const report = toImportReport(payload);
      if (report === null) {
        return { ok: false, code: UNKNOWN_CODE };
      }
      return { ok: true, report };
    }
    if (response.status === 409) {
      return { ok: false, code: "import_conflict" };
    }
    return { ok: false, code: await readErrorCode(response) };
  } catch {
    return { ok: false, code: UNKNOWN_CODE };
  }
}

// readErrorCode extracts the error envelope's code, failing closed to
// UNKNOWN_CODE when the body is not a readable envelope.
async function readErrorCode(response: Response): Promise<string> {
  const payload = await readErrorPayload(response);
  if (isRecord(payload) && isNonEmptyString(payload.code)) {
    return payload.code;
  }
  return UNKNOWN_CODE;
}

function toImportPreview(value: unknown): ImportPreview | null {
  if (
    !isRecord(value) ||
    !hasExactKeys(value, ["importId", "preview"]) ||
    !isNonEmptyString(value.importId)
  ) {
    return null;
  }
  const preview = value.preview;
  if (
    !isRecord(preview) ||
    !hasExactKeys(preview, [
      "new",
      "skipped",
      "conflicts",
      "invalid",
      "details",
      "truncated",
    ]) ||
    !isInteger(preview.new) ||
    !isInteger(preview.skipped) ||
    !isInteger(preview.conflicts) ||
    !isInteger(preview.invalid) ||
    !Array.isArray(preview.details) ||
    !isBoolean(preview.truncated)
  ) {
    return null;
  }
  const details: ImportDecision[] = [];
  for (const item of preview.details) {
    if (!isImportDecision(item)) {
      return null;
    }
    details.push(item);
  }
  return {
    importId: value.importId,
    new: preview.new,
    skipped: preview.skipped,
    conflicts: preview.conflicts,
    invalid: preview.invalid,
    details,
    truncated: preview.truncated,
  };
}

function toImportReport(value: unknown): ImportReport | null {
  if (
    !isRecord(value) ||
    !hasAllowedKeys(value, [...REPORT_REQUIRED_KEYS, ...REPORT_OPTIONAL_KEYS])
  ) {
    return null;
  }
  for (const key of REPORT_REQUIRED_KEYS) {
    if (!(key in value)) {
      return null;
    }
  }
  if (
    !isInteger(value.new) ||
    !isInteger(value.skipped) ||
    !isInteger(value.conflicts) ||
    !isInteger(value.invalid) ||
    !isRFC3339(value.committedAt)
  ) {
    return null;
  }
  if (value.details !== undefined && !isDecisionList(value.details)) {
    return null;
  }
  if (value.truncated !== undefined && !isBoolean(value.truncated)) {
    return null;
  }
  return {
    new: value.new,
    skipped: value.skipped,
    conflicts: value.conflicts,
    invalid: value.invalid,
    committedAt: value.committedAt,
  };
}

function isDecisionList(value: unknown): value is ImportDecision[] {
  return Array.isArray(value) && value.every(isImportDecision);
}

function isImportDecision(value: unknown): value is ImportDecision {
  if (
    !isRecord(value) ||
    !hasAllowedKeys(value, ["kind", "sourceRecordId", "outcome", "reason"])
  ) {
    return false;
  }
  for (const key of ["kind", "sourceRecordId", "outcome"]) {
    if (!(key in value)) {
      return false;
    }
  }
  return (
    isNonEmptyString(value.kind) &&
    isNonEmptyString(value.sourceRecordId) &&
    typeof value.outcome === "string" &&
    DECISION_OUTCOMES.includes(value.outcome) &&
    (value.reason === undefined || typeof value.reason === "string")
  );
}
