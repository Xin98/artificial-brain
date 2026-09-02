"use client";

import { useState } from "react";

import { requestLoginChallenge, verifyLogin } from "./fetch-auth";
import type { AuthErrorCode, LoginIdentifier } from "./fetch-auth";

const errorMessages: Record<AuthErrorCode, string> = {
  validation_error: "输入格式不正确,请检查后重试。",
  rate_limited: "请求过于频繁,请稍后再试。",
  unauthenticated: "验证码不正确或已失效。",
  unavailable: "服务暂时不可用,请稍后再试。",
  sms_unavailable: "当前环境暂不支持手机号登录,请使用邮箱。",
  verification_send_failed: "验证码发送失败,请稍后重试。",
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
}: {
  fetcher?: typeof fetch;
  onNavigate?: (path: string) => void;
}): React.JSX.Element {
  const [step, setStep] = useState<"identifier" | "code">("identifier");
  const [identifier, setIdentifier] = useState("");
  const [code, setCode] = useState("");
  const [error, setError] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);

  async function submitIdentifier(event: React.FormEvent): Promise<void> {
    event.preventDefault();
    setBusy(true);
    setError(null);
    const outcome = await requestLoginChallenge(
      "",
      fetcher,
      identifierFrom(identifier),
    );
    setBusy(false);
    if (outcome.ok) {
      setStep("code");
      return;
    }
    setError(errorMessages[outcome.error ?? "unavailable"]);
  }

  async function submitCode(event: React.FormEvent): Promise<void> {
    event.preventDefault();
    setBusy(true);
    setError(null);
    const outcome = await verifyLogin(
      "",
      fetcher,
      identifierFrom(identifier),
      code,
    );
    setBusy(false);
    if (outcome.ok) {
      onNavigate("/");
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
          <button
            className="btn-ghost"
            onClick={() => {
              setStep("identifier");
              setError(null);
            }}
            type="button"
          >
            返回
          </button>
          <p className="login-hint">
            本地开发可以从
            <a
              href={`/api/v1/dev/sms-inbox?address=${encodeURIComponent(identifier)}`}
            >
              开发收件箱
            </a>
            查看验证码。
          </p>
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
