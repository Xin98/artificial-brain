"use client";

import { useEffect, useState } from "react";

import {
  addChannel,
  listChannels,
  setChannelEnabled,
  verifyChannel,
} from "./fetch-channels";
import type { ContactChannel } from "./fetch-channels";

const errorMessages: Record<string, string> = {
  validation_error: "格式无效,请检查类型与地址。",
  conflict: "该联系方式已存在。",
  not_found: "联系方式不存在。",
  unavailable: "服务暂时不可用,请稍后再试。",
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
    setError(null);
    const outcome = await addChannel("", fetcher, kind, address);
    if (outcome.ok) {
      setAddress("");
      refresh();
      return;
    }
    setError(errorMessages[outcome.error ?? "unavailable"]);
  }

  async function handleVerify(channelId: string): Promise<void> {
    setError(null);
    const code = codes[channelId] ?? "";
    const outcome = await verifyChannel("", fetcher, channelId, code);
    if (outcome.ok) {
      refresh();
      return;
    }
    setError(errorMessages[outcome.error ?? "unavailable"]);
  }

  async function handleToggle(channel: ContactChannel): Promise<void> {
    setError(null);
    const outcome = await setChannelEnabled(
      "",
      fetcher,
      channel.id,
      !channel.enabled,
    );
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
        <button className="btn-primary" type="submit">
          添加
        </button>
      </form>
      {error ? (
        <p aria-live="polite" className="channel-error" role="alert">
          {error}
        </p>
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
              {channel.verified ? (
                <span className="badge badge-ok">已验证</span>
              ) : (
                <span className="channel-verify">
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
                    onClick={() => void handleVerify(channel.id)}
                    type="button"
                  >
                    验证
                  </button>
                </span>
              )}
              <button
                className="btn-quiet"
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
