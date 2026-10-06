"use client";

import { useEffect, useRef, useState } from "react";

import {
  addChannel,
  listChannels,
  setChannelEnabled,
  verifyChannel,
  resendChannelVerification,
} from "./fetch-channels";
import type { ContactChannel } from "./fetch-channels";

const errorMessages: Record<string, string> = {
  validation_error: "格式无效,请检查类型与地址。",
  conflict: "该联系方式已存在。",
  not_found: "联系方式不存在。",
  unavailable: "服务暂时不可用,请稍后再试。",
  rate_limited: "发送过于频繁，请稍后重新发送。",
};

// ChannelManager lists contact channels and offers add, verify-code, and
// enable-toggle actions.
export function ChannelManager({
  fetcher = fetch,
}: {
  fetcher?: typeof fetch;
}): React.JSX.Element {
  const [channels, setChannels] = useState<ContactChannel[]>([]);
  const [kind, setKind] = useState("email");
  const [address, setAddress] = useState("");
  const [codes, setCodes] = useState<Record<string, string>>({});
  const [error, setError] = useState<string | null>(null);
  const [loading, setLoading] = useState(true);
  const [reloadKey, setReloadKey] = useState(0);
  const [busy, setBusy] = useState(false);
  const inFlight = useRef(false);
  const [notice, setNotice] = useState<string | null>(null);
  const [sentAt, setSentAt] = useState<Record<string, number>>({});
  const [now, setNow] = useState(0);
  useEffect(() => {
    if (!Object.keys(sentAt).length) return;
    const timer = setInterval(() => setNow(Date.now()), 1000);
    return () => clearInterval(timer);
  }, [sentAt]);
  function remaining(channelId: string): number {
    return sentAt[channelId]
      ? Math.max(0, 60 - Math.floor((now - sentAt[channelId]) / 1000))
      : 0;
  }
  function startCountdown(channelId: string): void {
    const instant = Date.now();
    setNow(instant);
    setSentAt((previous) => ({ ...previous, [channelId]: instant }));
  }
  async function handleResend(channel: ContactChannel): Promise<void> {
    if (inFlight.current || remaining(channel.id) > 0) return;
    inFlight.current = true;
    setBusy(true);
    setError(null);
    const outcome = await resendChannelVerification("", fetcher, channel.id);
    inFlight.current = false;
    setBusy(false);
    if (outcome.ok) {
      startCountdown(channel.id);
      setNotice(`验证码已重新发送至 ${channel.address}，请使用最新验证码。`);
    } else setError(errorMessages[outcome.error ?? "unavailable"]);
  }

  useEffect(() => {
    let cancelled = false;
    void listChannels("", fetcher).then((result) => {
      if (cancelled) {
        return;
      }
      setLoading(false);
      if (result === null) {
        setError(errorMessages.unavailable);
        return;
      }
      setChannels(result);
      setError(null);
    });
    return () => {
      cancelled = true;
    };
  }, [fetcher, reloadKey]);

  function refresh(): void {
    setLoading(true);
    setReloadKey((key) => key + 1);
  }

  async function handleAdd(event: React.FormEvent): Promise<void> {
    event.preventDefault();
    if (inFlight.current) return;
    const normalized = address.trim();
    if (
      !(kind === "email"
        ? /^[^\s@]+@[^\s@]+\.[^\s@]+$/.test(normalized)
        : /^\+[1-9]\d{7,14}$/.test(normalized))
    ) {
      setError(errorMessages.validation_error);
      return;
    }
    if (
      channels.some(
        (channel) =>
          channel.kind === kind &&
          channel.address.toLowerCase() === normalized.toLowerCase(),
      )
    ) {
      setError(errorMessages.conflict);
      return;
    }
    inFlight.current = true;
    setBusy(true);
    setError(null);
    const outcome = await addChannel("", fetcher, kind, normalized);
    inFlight.current = false;
    setBusy(false);
    if (outcome.ok) {
      if (outcome.channel) startCountdown(outcome.channel.id);
      setNotice(
        `联系方式已添加，验证码已发送至 ${normalized}。请检查收件箱及垃圾邮件，验证后才能接收提醒。`,
      );
      setAddress("");
      refresh();
      return;
    }
    setError(errorMessages[outcome.error ?? "unavailable"]);
  }

  async function handleVerify(channelId: string): Promise<void> {
    if (inFlight.current) return;
    setError(null);
    const code = (codes[channelId] ?? "").trim();
    if (!/^\d{6}$/.test(code)) {
      setError("请输入六位验证码。");
      return;
    }
    inFlight.current = true;
    setBusy(true);
    const outcome = await verifyChannel("", fetcher, channelId, code);
    inFlight.current = false;
    setBusy(false);
    if (outcome.ok) {
      setNotice("联系方式已验证，请确认它处于启用状态。");
      refresh();
      return;
    }
    setError(errorMessages[outcome.error ?? "unavailable"]);
  }

  async function handleToggle(channel: ContactChannel): Promise<void> {
    if (inFlight.current) return;
    inFlight.current = true;
    setBusy(true);
    setError(null);
    const outcome = await setChannelEnabled(
      "",
      fetcher,
      channel.id,
      !channel.enabled,
    );
    inFlight.current = false;
    setBusy(false);
    if (outcome.ok) {
      refresh();
      return;
    }
    setError(errorMessages[outcome.error ?? "unavailable"]);
  }

  return (
    <section aria-label="联系方式" className="channel-manager">
      <form className="channel-add" onSubmit={handleAdd}>
        <div className="field">
          <label htmlFor="channel-kind">类型</label>
          <select
            id="channel-kind"
            onChange={(event) => setKind(event.target.value)}
            value={kind}
          >
            <option value="email">邮箱</option>
            <option value="sms">短信</option>
          </select>
        </div>
        <div className="field">
          <label htmlFor="channel-address">地址</label>
          <input
            id="channel-address"
            onChange={(event) => setAddress(event.target.value)}
            type="text"
            value={address}
          />
        </div>
        <button className="btn-primary" disabled={busy} type="submit">
          添加
        </button>
      </form>
      {notice ? (
        <p className="channel-guidance" role="status">
          {notice}
        </p>
      ) : null}
      {error ? (
        <p aria-live="polite" className="channel-error" role="alert">
          {error}
        </p>
      ) : null}
      {error ? (
        <button
          className="btn-ghost"
          disabled={busy || loading}
          onClick={refresh}
          type="button"
        >
          重新加载联系方式
        </button>
      ) : null}
      {loading ? (
        <ul aria-label="加载中" className="list-skeleton">
          {Array.from({ length: 2 }, (_unused, index) => (
            <li key={index}>
              <span className="skeleton skeleton-line" />
            </li>
          ))}
        </ul>
      ) : channels.length === 0 ? (
        <p className="list-empty">还没有联系方式,添加后即可接收提醒。</p>
      ) : (
        <ul>
          {channels.map((channel) => (
            <li className="channel-item" key={channel.id}>
              <span className="channel-address">
                <span className="badge badge-muted">
                  {channel.kind === "email" ? "邮箱" : "短信"}
                </span>
                {channel.address}
              </span>
              <span className="channel-guidance">
                {!channel.verified
                  ? "待验证 · 暂不能接收提醒"
                  : channel.enabled
                    ? "已启用 · 可接收提醒"
                    : "已停用 · 不接收提醒"}
              </span>
              {channel.verified ? (
                <span className="badge badge-ok">已验证</span>
              ) : (
                <span className="channel-verify">
                  <span className="channel-guidance">
                    添加时已发送验证码，请检查收件箱及垃圾邮件。若验证码失效或未收到，可重新发送，并使用最新验证码。
                  </span>
                  <label htmlFor={`channel-code-${channel.id}`}>验证码</label>
                  <input
                    id={`channel-code-${channel.id}`}
                    onChange={(event) =>
                      setCodes((previous) => ({
                        ...previous,
                        [channel.id]: event.target.value,
                      }))
                    }
                    type="text"
                    value={codes[channel.id] ?? ""}
                  />
                  <button
                    className="btn-ghost"
                    disabled={busy}
                    onClick={() => void handleVerify(channel.id)}
                    type="button"
                  >
                    验证
                  </button>
                  <button
                    className="btn-ghost"
                    disabled={busy || remaining(channel.id) > 0}
                    onClick={() => void handleResend(channel)}
                    type="button"
                  >
                    {remaining(channel.id) > 0
                      ? `${remaining(channel.id)} 秒后重新发送`
                      : "重新发送验证码"}
                  </button>
                </span>
              )}
              <button
                className="btn-quiet"
                disabled={busy}
                onClick={() => void handleToggle(channel)}
                type="button"
              >
                {channel.enabled ? "停用" : "启用"}
              </button>
            </li>
          ))}
        </ul>
      )}
    </section>
  );
}
