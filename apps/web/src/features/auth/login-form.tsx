"use client";

import { useEffect, useRef, useState } from "react";

import { requestLoginChallenge, verifyLogin } from "./fetch-auth";
import type { AuthErrorCode, LoginIdentifier } from "./fetch-auth";
import {
  rememberConversationOwner,
  safeInternalReturnTo,
} from "./session-recovery";

const errorMessages: Record<AuthErrorCode, string> = {
  validation_error: "输入格式不正确,请检查后重试。",
  rate_limited: "请求过于频繁,请稍后再试。",
  unauthenticated: "验证码不正确或已失效。",
  unavailable: "服务暂时不可用,请稍后再试。",
  sms_unavailable: "当前环境暂不支持手机号登录,请使用邮箱。",
  verification_send_failed: "验证码发送失败,请稍后重试。",
  registration_closed: "当前服务注册已关闭，请使用已授权账号或联系管理员。",
};

// identifierFrom classifies the single login input: an address containing
// '@' is an email identifier, everything else a phone number.
function identifierFrom(value: string): LoginIdentifier {
  if (value.includes("@")) {
    return { email: value };
  }
  return { phone: value };
}

// LoginForm is the two-step identifier + code login. fetcher and onNavigate
// are injected so tests can drive the flow without network or navigation.
export function LoginForm({
  fetcher = fetch,
  onNavigate = (path: string) => window.location.assign(path),
  returnTo,
}: {
  fetcher?: typeof fetch;
  onNavigate?: (path: string) => void;
  returnTo?: string;
}): React.JSX.Element {
  const [step, setStep] = useState<"identifier" | "code">("identifier");
  const [identifier, setIdentifier] = useState("");
  const [code, setCode] = useState("");
  const [error, setError] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);
  const inFlight = useRef(false);
  const [sentAt, setSentAt] = useState<number | null>(null);
  const [now, setNow] = useState(0);
  const remaining = sentAt
    ? Math.max(0, 60 - Math.floor((now - sentAt) / 1000))
    : 0;
  const expired = sentAt !== null && now - sentAt >= 5 * 60 * 1000;
  useEffect(() => {
    if (sentAt === null) return;
    const timer = setInterval(() => setNow(Date.now()), 1000);
    return () => clearInterval(timer);
  }, [sentAt]);

  async function submitIdentifier(event: React.FormEvent): Promise<void> {
    event.preventDefault();
    if (inFlight.current || (step === "code" && remaining > 0)) return;
    inFlight.current = true;
    setBusy(true);
    setError(null);
    const outcome = await requestLoginChallenge(
      "",
      fetcher,
      identifierFrom(identifier.trim()),
    );
    setBusy(false);
    inFlight.current = false;
    if (outcome.ok) {
      setIdentifier(identifier.trim());
      setCode("");
      setSentAt(Date.now());
      setNow(Date.now());
      setStep("code");
      return;
    }
    setError(errorMessages[outcome.error ?? "unavailable"]);
  }

  async function submitCode(event: React.FormEvent): Promise<void> {
    event.preventDefault();
    if (inFlight.current) return;
    inFlight.current = true;
    setBusy(true);
    setError(null);
    const outcome = await verifyLogin(
      "",
      fetcher,
      identifierFrom(identifier),
      code,
    );
    setBusy(false);
    inFlight.current = false;
    if (outcome.ok) {
      if (outcome.userId) rememberConversationOwner(outcome.userId);
      onNavigate(
        safeInternalReturnTo(
          returnTo ??
            new URLSearchParams(window.location.search).get("returnTo"),
        ),
      );
      return;
    }
    setError(errorMessages[outcome.error ?? "unavailable"]);
  }

  return (
    <form
      aria-label="登录"
      className="login-form"
      onSubmit={step === "identifier" ? submitIdentifier : submitCode}
    >
      {step === "identifier" ? (
        <div className="login-step">
          <div className="field">
            <label htmlFor="login-identifier">手机号或邮箱</label>
            <input
              autoComplete="username"
              id="login-identifier"
              name="identifier"
              onChange={(event) => setIdentifier(event.target.value)}
              placeholder="+8613800138000 或 you@example.com"
              type="text"
              value={identifier}
            />
          </div>
          <button className="btn-primary" disabled={busy} type="submit">
            获取验证码
          </button>
        </div>
      ) : (
        <div className="login-step">
          <p className="login-account">
            验证码已发送至 <strong>{identifier}</strong>
          </p>
          <div className="field">
            <label htmlFor="login-code">验证码</label>
            <input
              autoComplete="one-time-code"
              id="login-code"
              name="code"
              onChange={(event) => setCode(event.target.value)}
              type="text"
              value={code}
            />
          </div>
          <button className="btn-primary" disabled={busy} type="submit">
            登录
          </button>
          <p role="status">
            {expired
              ? "验证码可能已过期，请重新发送。"
              : "请尽快输入验证码；失效后可重新发送。请检查收件箱及垃圾邮件。"}
          </p>
          <button
            className="btn-ghost"
            disabled={busy || remaining > 0}
            onClick={(event) => void submitIdentifier(event)}
            type="button"
          >
            {remaining > 0 ? `${remaining} 秒后重新发送` : "重新发送验证码"}
          </button>
          <button
            className="btn-ghost"
            disabled={busy}
            onClick={() => {
              setStep("identifier");
              setError(null);
            }}
            type="button"
          >
            返回
          </button>
        </div>
      )}
      {error ? (
        <p aria-live="polite" className="login-error" role="alert">
          {error}
        </p>
      ) : null}
    </form>
  );
}
