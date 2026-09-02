import {
  classifyErrorPayload,
  hasExactKeys,
  isNonEmptyString,
  isRecord,
  isRFC3339,
  readErrorPayload,
  safeTimeout,
} from "../validation";

export type AuthErrorCode =
  | "validation_error"
  | "rate_limited"
  | "unauthenticated"
  | "unavailable"
  | "sms_unavailable"
  | "verification_send_failed";

export interface LoginIdentifier {
  phone?: string;
  email?: string;
}

export interface RequestChallengeOutcome {
  ok: boolean;
  error?: AuthErrorCode;
}

export interface VerifyOutcome {
  ok: boolean;
  userId?: string;
  workspaceId?: string;
  expiresAt?: string;
  error?: AuthErrorCode;
}

async function postJSON(
  baseURL: string,
  fetcher: typeof fetch,
  path: string,
  body: unknown,
  timeoutMs: number,
): Promise<Response> {
  return fetcher(`${baseURL}${path}`, {
    method: "POST",
    signal: AbortSignal.timeout(safeTimeout(timeoutMs)),
    cache: "no-store",
    headers: { "content-type": "application/json", accept: "application/json" },
    body: JSON.stringify(body),
  });
}

export async function requestLoginChallenge(
  baseURL: string,
  fetcher: typeof fetch,
  identifier: LoginIdentifier,
  timeoutMs = 5000,
): Promise<RequestChallengeOutcome> {
  try {
    const response = await postJSON(
      baseURL,
      fetcher,
      "/api/v1/auth/login/request",
      identifier,
      timeoutMs,
    );
    if (response.status === 202) {
      return { ok: true };
    }
    return { ok: false, error: await classifyStatus(response) };
  } catch {
    return { ok: false, error: "unavailable" };
  }
}

export async function verifyLogin(
  baseURL: string,
  fetcher: typeof fetch,
  identifier: LoginIdentifier,
  code: string,
  timeoutMs = 5000,
): Promise<VerifyOutcome> {
  try {
    const response = await postJSON(
      baseURL,
      fetcher,
      "/api/v1/auth/login/verify",
      { ...identifier, code },
      timeoutMs,
    );
    if (response.status === 200) {
      const payload: unknown = await response.json();
      if (
        isRecord(payload) &&
        hasExactKeys(payload, ["userId", "workspaceId", "expiresAt"]) &&
        isNonEmptyString(payload.userId) &&
        isNonEmptyString(payload.workspaceId) &&
        isRFC3339(payload.expiresAt)
      ) {
        return {
          ok: true,
          userId: payload.userId,
          workspaceId: payload.workspaceId,
          expiresAt: payload.expiresAt,
        };
      }
      return { ok: false, error: "unavailable" };
    }
    if (response.status === 401) {
      return { ok: false, error: "unauthenticated" };
    }
    return { ok: false, error: await classifyStatus(response) };
  } catch {
    return { ok: false, error: "unavailable" };
  }
}

export async function logout(
  baseURL: string,
  fetcher: typeof fetch,
  timeoutMs = 5000,
): Promise<boolean> {
  try {
    const response = await postJSON(
      baseURL,
      fetcher,
      "/api/v1/auth/logout",
      {},
      timeoutMs,
    );
    return response.status === 200;
  } catch {
    return false;
  }
}

async function classifyStatus(response: Response): Promise<AuthErrorCode> {
  const payload = await readErrorPayload(response);
  const classified = classifyErrorPayload(payload);
  if (classified.code === "validation_error") {
    return "validation_error";
  }
  if (classified.code === "rate_limited") {
    return "rate_limited";
  }
  // classifyErrorPayload collapses unknown codes to "other", so inspect the
  // raw payload's code field for the auth-specific codes it does not enum.
  if (isRecord(payload) && typeof payload.code === "string") {
    if (payload.code === "sms_unavailable") {
      return "sms_unavailable";
    }
    if (payload.code === "verification_send_failed") {
      return "verification_send_failed";
    }
  }
  return "unavailable";
}
