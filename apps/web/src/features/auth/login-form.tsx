"use client";

import { useState } from "react";

import { requestLoginChallenge, verifyLogin } from "./fetch-auth";
import type { AuthErrorCode } from "./fetch-auth";

const errorMessages: Record<AuthErrorCode, string> = {
  validation_error: "输入格式不正确,请检查后重试。",
  rate_limited: "请求过于频繁,请稍后再试。",
  unauthenticated: "验证码不正确或已失效。",
  unavailable: "服务暂时不可用,请稍后再试。",
};

// LoginForm is the two-step phone + code login. fetcher and onNavigate are
// injected so tests can drive the flow without network or navigation.
export function LoginForm({
  fetcher = fetch,
  onNavigate = (path: string) => window.location.assign(path),
}: {
  fetcher?: typeof fetch;
  onNavigate?: (path: string) => void;
}): React.JSX.Element {
  const [step, setStep] = useState<"phone" | "code">("phone");
  const [phone, setPhone] = useState("");
  const [code, setCode] = useState("");
  const [error, setError] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);

  async function submitPhone(event: React.FormEvent): Promise<void> {
    event.preventDefault();
    setBusy(true);
    setError(null);
    const outcome = await requestLoginChallenge("", fetcher, phone);
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
    const outcome = await verifyLogin("", fetcher, phone, code);
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
      onSubmit={step === "phone" ? submitPhone : submitCode}
    >
      {step === "phone" ? (
        <div className="login-step">
          <div className="field">
            <label htmlFor="login-phone">手机号</label>
            <input
              autoComplete="tel"
              id="login-phone"
              name="phone"
              onChange={(event) => setPhone(event.target.value)}
              placeholder="+8613800138000"
              type="tel"
              value={phone}
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
              setStep("phone");
              setError(null);
            }}
            type="button"
          >
            返回
          </button>
          <p className="login-hint">
            本地开发可以从
            <a
              href={`/api/v1/dev/sms-inbox?address=${encodeURIComponent(phone)}`}
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
