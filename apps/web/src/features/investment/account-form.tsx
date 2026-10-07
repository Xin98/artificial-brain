"use client";
import { useState } from "react";
import { failureText, type InvestmentClient } from "./fetch-investment";
import { useMutation, useResource } from "./hooks";
import type { AccountView, VersionsPage } from "./types";
export function VersionSelect({
  client,
  resource,
  value,
  onChange,
  label,
}: {
  client: InvestmentClient;
  resource: "universes" | "strategies";
  value: string;
  onChange: (v: string) => void;
  label: string;
}) {
  const [cursor, setCursor] = useState("");
  const { result, retry } = useResource<VersionsPage>(
    client,
    "/" + resource + "?limit=100&cursor=" + encodeURIComponent(cursor),
    "VersionsPage",
  );
  return (
    <div>
      <label>
        {label}
        <select value={value} onChange={(e) => onChange(e.target.value)}>
          <option value="">采用当前默认版本</option>
          {value && !result?.ok ? <option value={value}>{value}</option> : null}
          {result?.ok &&
          !result.value.items.some((v) => v.id === value) &&
          value ? (
            <option value={value}>{value}</option>
          ) : null}
          {result?.ok
            ? result.value.items.map((v) => (
                <option key={v.id} value={v.id}>
                  {v.name} · {v.id}
                </option>
              ))
            : null}
        </select>
      </label>
      {result && !result.ok ? (
        <p role="alert">
          {failureText(result.code)}{" "}
          <button type="button" onClick={retry}>
            重试版本列表
          </button>
        </p>
      ) : null}
      {result?.ok && result.value.nextCursor ? (
        <button
          type="button"
          onClick={() => setCursor(result.value.nextCursor)}
        >
          下一页{label}
        </button>
      ) : null}
      {cursor ? (
        <button type="button" onClick={() => setCursor("")}>
          返回版本首屏
        </button>
      ) : null}
    </div>
  );
}
export function AccountForm({
  client,
  onCreated,
}: {
  client: InvestmentClient;
  onCreated: (a: AccountView) => void;
}) {
  const [name, setName] = useState("");
  const [cash, setCash] = useState("100000.00");
  const [universe, setUniverse] = useState("");
  const [strategy, setStrategy] = useState("");
  const [error, setError] = useState("");
  const mutation = useMutation<AccountView>(client, "AccountView");
  return (
    <form
      className="investment-form"
      onSubmit={async (e) => {
        e.preventDefault();
        if (
          !/^[0-9]+\.[0-9]{2}$/.test(cash) ||
          BigInt(cash.replace(".", "")) <= 0n ||
          BigInt(cash.replace(".", "")) > 100000000000n
        ) {
          setError("初始资金须为正数，保留两位小数，最多 1000000000.00 USD。");
          return;
        }
        setError("");
        const body = {
          name,
          initialCash: cash,
          ...(universe ? { universeVersionId: universe } : {}),
          ...(strategy ? { strategyVersionId: strategy } : {}),
        };
        const result = await mutation.submit("POST", "/accounts", body);
        if (result?.ok) onCreated(result.value);
      }}
    >
      <h2>创建模拟账户</h2>
      <label>
        账户名称
        <input
          required
          maxLength={100}
          value={name}
          onChange={(e) => setName(e.target.value)}
        />
      </label>
      <label>
        初始资金 USD
        <input
          inputMode="decimal"
          required
          value={cash}
          onChange={(e) => setCash(e.target.value)}
        />
      </label>
      <div className="investment-fields">
        <VersionSelect
          client={client}
          resource="universes"
          label="股票池版本"
          value={universe}
          onChange={setUniverse}
        />
        <VersionSelect
          client={client}
          resource="strategies"
          label="策略版本"
          value={strategy}
          onChange={setStrategy}
        />
      </div>
      <p>
        仅整数股、多头、日线；自动交易初始关闭。账户固定绑定创建时的数据模式和行情源。
      </p>
      {error ? (
        <p role="alert">{error}</p>
      ) : mutation.result && !mutation.result.ok ? (
        <p role="alert">{failureText(mutation.result.code)}</p>
      ) : null}
      <button className="btn-primary" disabled={mutation.busy}>
        创建模拟账户
      </button>
    </form>
  );
}
