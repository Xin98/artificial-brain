import {
  hasAllowedKeys,
  hasExactKeys,
  isInteger,
  isNonEmptyString,
  isRecord,
  isRFC3339,
  safeTimeout,
} from "../validation";

export type ReminderChannel = "email" | "sms";

export type ReminderState =
  "scheduled" | "sending" | "succeeded" | "failed" | "suppressed";

export type ReminderReceiptState = "received_ok" | "received_failed";

// ReminderDelivery mirrors the API DeliveryView: required lifecycle fields
// plus optional fields that the Go encoder omits when unset.
export interface ReminderDelivery {
  id: string;
  todoId: string;
  todoTitle: string;
  channel: ReminderChannel;
  state: ReminderState;
  attemptCount: number;
  scheduledAt: string;
  createdAt: string;
  suppressionReason?: string;
  providerMessageId?: string;
  lastErrorCode?: string;
  submittedAt?: string;
  finalizedAt?: string;
  receiptState?: ReminderReceiptState;
  receiptAt?: string;
  receiptErrorCode?: string;
}

const REQUIRED_KEYS = [
  "id",
  "todoId",
  "todoTitle",
  "channel",
  "state",
  "attemptCount",
  "scheduledAt",
  "createdAt",
] as const;

const OPTIONAL_KEYS = [
  "suppressionReason",
  "providerMessageId",
  "lastErrorCode",
  "submittedAt",
  "finalizedAt",
  "receiptState",
  "receiptAt",
  "receiptErrorCode",
] as const;

const SUPPRESSION_REASONS = [
  "todo_completed",
  "todo_deleted",
  "version_stale",
  "channel_unavailable",
  "plan_revoked",
];

export async function fetchReminderDeliveries(
  baseURL: string,
  fetcher: typeof fetch,
  timeoutMs = 3000,
): Promise<ReminderDelivery[] | null> {
  try {
    const response = await fetcher(`${baseURL}/api/v1/reminders`, {
      signal: AbortSignal.timeout(safeTimeout(timeoutMs)),
      cache: "no-store",
      headers: { accept: "application/json" },
    });
    if (!response.ok) {
      return null;
    }
    const payload: unknown = await response.json();
    if (
      !isRecord(payload) ||
      !hasExactKeys(payload, ["deliveries"]) ||
      !Array.isArray(payload.deliveries)
    ) {
      return null;
    }
    const deliveries: ReminderDelivery[] = [];
    for (const item of payload.deliveries) {
      if (!isReminderDelivery(item)) {
        return null;
      }
      deliveries.push(item);
    }
    return deliveries;
  } catch {
    return null;
  }
}

function isReminderDelivery(value: unknown): value is ReminderDelivery {
  if (!isRecord(value)) {
    return false;
  }
  if (!hasAllowedKeys(value, [...REQUIRED_KEYS, ...OPTIONAL_KEYS])) {
    return false;
  }
  for (const key of REQUIRED_KEYS) {
    if (!(key in value)) {
      return false;
    }
  }
  if (
    !isNonEmptyString(value.id) ||
    !isNonEmptyString(value.todoId) ||
    !isNonEmptyString(value.todoTitle) ||
    (value.channel !== "email" && value.channel !== "sms") ||
    (value.state !== "scheduled" &&
      value.state !== "sending" &&
      value.state !== "succeeded" &&
      value.state !== "failed" &&
      value.state !== "suppressed") ||
    !isInteger(value.attemptCount) ||
    !isRFC3339(value.scheduledAt) ||
    !isRFC3339(value.createdAt)
  ) {
    return false;
  }
  if (
    value.suppressionReason !== undefined &&
    (typeof value.suppressionReason !== "string" ||
      !SUPPRESSION_REASONS.includes(value.suppressionReason))
  ) {
    return false;
  }
  if (
    value.receiptState !== undefined &&
    value.receiptState !== "received_ok" &&
    value.receiptState !== "received_failed"
  ) {
    return false;
  }
  for (const key of [
    "providerMessageId",
    "lastErrorCode",
    "receiptErrorCode",
  ] as const) {
    const candidate = value[key];
    if (candidate !== undefined && typeof candidate !== "string") {
      return false;
    }
  }
  for (const key of ["submittedAt", "finalizedAt", "receiptAt"] as const) {
    const candidate = value[key];
    if (candidate !== undefined && !isRFC3339(candidate)) {
      return false;
    }
  }
  return true;
}
