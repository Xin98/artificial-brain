import {
  classifyErrorPayload,
  hasExactKeys,
  isBoolean,
  isNonEmptyString,
  isRecord,
  isRFC3339,
  readErrorPayload,
  safeTimeout,
} from "../validation";

export interface ContactChannel {
  id: string;
  kind: "email" | "sms";
  address: string;
  verified: boolean;
  enabled: boolean;
  createdAt: string;
}

export type ChannelErrorCode = "validation_error" | "conflict" | "unavailable";

export interface ChannelOutcome {
  ok: boolean;
  channel?: ContactChannel;
  error?: ChannelErrorCode;
}

export async function listChannels(
  baseURL: string,
  fetcher: typeof fetch,
  timeoutMs = 5000,
): Promise<ContactChannel[] | null> {
  try {
    const response = await fetcher(
      `${baseURL}/api/v1/settings/contact-channels`,
      {
        signal: AbortSignal.timeout(safeTimeout(timeoutMs)),
        cache: "no-store",
        headers: { accept: "application/json" },
      },
    );
    if (!response.ok) {
      return null;
    }
    const payload: unknown = await response.json();
    if (
      !isRecord(payload) ||
      !hasExactKeys(payload, ["channels"]) ||
      !Array.isArray(payload.channels)
    ) {
      return null;
    }
    const channels: ContactChannel[] = [];
    for (const item of payload.channels) {
      if (!isChannel(item)) {
        return null;
      }
      channels.push(item);
    }
    return channels;
  } catch {
    return null;
  }
}

export async function addChannel(
  baseURL: string,
  fetcher: typeof fetch,
  kind: string,
  address: string,
  timeoutMs = 5000,
): Promise<ChannelOutcome> {
  try {
    const response = await fetcher(
      `${baseURL}/api/v1/settings/contact-channels`,
      {
        method: "POST",
        signal: AbortSignal.timeout(safeTimeout(timeoutMs)),
        cache: "no-store",
        headers: {
          "content-type": "application/json",
          accept: "application/json",
        },
        body: JSON.stringify({ kind, address }),
      },
    );
    if (response.status === 201) {
      const payload: unknown = await response.json();
      return isChannel(payload)
        ? { ok: true, channel: payload }
        : { ok: false, error: "unavailable" };
    }
    return { ok: false, error: await classify(response) };
  } catch {
    return { ok: false, error: "unavailable" };
  }
}

export async function verifyChannel(
  baseURL: string,
  fetcher: typeof fetch,
  channelId: string,
  code: string,
  timeoutMs = 5000,
): Promise<{ ok: boolean; error?: ChannelErrorCode | "not_found" }> {
  try {
    const response = await fetcher(
      `${baseURL}/api/v1/settings/contact-channels/${encodeURIComponent(channelId)}/verify`,
      {
        method: "POST",
        signal: AbortSignal.timeout(safeTimeout(timeoutMs)),
        cache: "no-store",
        headers: {
          "content-type": "application/json",
          accept: "application/json",
        },
        body: JSON.stringify({ code }),
      },
    );
    if (response.status === 200) {
      const payload: unknown = await response.json();
      if (
        isRecord(payload) &&
        hasExactKeys(payload, ["verified"]) &&
        payload.verified === true
      ) {
        return { ok: true };
      }
      return { ok: false, error: "unavailable" };
    }
    if (response.status === 404) {
      return { ok: false, error: "not_found" };
    }
    return { ok: false, error: await classify(response) };
  } catch {
    return { ok: false, error: "unavailable" };
  }
}

export async function setChannelEnabled(
  baseURL: string,
  fetcher: typeof fetch,
  channelId: string,
  enabled: boolean,
  timeoutMs = 5000,
): Promise<ChannelOutcome> {
  try {
    const response = await fetcher(
      `${baseURL}/api/v1/settings/contact-channels/${encodeURIComponent(channelId)}`,
      {
        method: "PATCH",
        signal: AbortSignal.timeout(safeTimeout(timeoutMs)),
        cache: "no-store",
        headers: {
          "content-type": "application/json",
          accept: "application/json",
        },
        body: JSON.stringify({ enabled }),
      },
    );
    if (response.status === 200) {
      const payload: unknown = await response.json();
      return isChannel(payload)
        ? { ok: true, channel: payload }
        : { ok: false, error: "unavailable" };
    }
    return { ok: false, error: await classify(response) };
  } catch {
    return { ok: false, error: "unavailable" };
  }
}

async function classify(response: Response): Promise<ChannelErrorCode> {
  const payload = await readErrorPayload(response);
  const classified = classifyErrorPayload(payload);
  if (classified.code === "validation_error") {
    return "validation_error";
  }
  if (classified.code === "conflict") {
    return "conflict";
  }
  return "unavailable";
}

function isChannel(value: unknown): value is ContactChannel {
  return (
    isRecord(value) &&
    hasExactKeys(value, [
      "id",
      "kind",
      "address",
      "verified",
      "enabled",
      "createdAt",
    ]) &&
    isNonEmptyString(value.id) &&
    (value.kind === "email" || value.kind === "sms") &&
    isNonEmptyString(value.address) &&
    isBoolean(value.verified) &&
    isBoolean(value.enabled) &&
    isRFC3339(value.createdAt)
  );
}
