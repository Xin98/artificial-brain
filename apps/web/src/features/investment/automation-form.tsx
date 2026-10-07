"use client";
import { useRef, useState } from "react";
import { failureText, type InvestmentClient } from "./fetch-investment";
import { useMutation } from "./hooks";
import { VersionSelect } from "./account-form";
import type { AccountView, AutomationRequest, RiskPolicy } from "./types";
const limits: { key: keyof RiskPolicy; label: string; max: number }[] = [
  { key: "singleWeight", label: "单股占比", max: 10 },
  { key: "industryWeight", label: "行业占比", max: 30 },
  { key: "stockWeight", label: "股票总占比", max: 80 },
  { key: "drawdownPause", label: "回撤暂停", max: 15 },
  { key: "stopLoss", label: "单股止损", max: 10 },
  { key: "turnoverLimit", label: "每日双边换手", max: 20 },
];
export function AutomationForm({
  client,
  account,
  onChanged,
}: {
  client: InvestmentClient;
  account: AccountView;
  onChanged: () => void;
}) {
  const [fresh, setFresh] = useState<AccountView | null>(null);
  const current = fresh && fresh.version > account.version ? fresh : account;
  const [strategy, setStrategy] = useState(account.strategyVersionId);
  const [universe, setUniverse] = useState(account.universeVersionId);
  const [policy, setPolicy] = useState(account.policy);
  const [conflict, setConflict] = useState(false);
  const [error, setError] = useState("");
  const frozen = useRef<{ signature: string; body: AutomationRequest } | null>(
    null,
  );
  const mutation = useMutation<AccountView>(client, "AccountView");
  async function submit(enabled: boolean) {
    const signature = JSON.stringify({ enabled, strategy, universe, policy });
    if (frozen.current?.signature !== signature)
      frozen.current = {
        signature,
        body: {
          enabled,
          expectedVersion: current.version,
          strategyVersionId: strategy,
          universeVersionId: universe,
          policy,
        },
      };
    setError("");
    const result = await mutation.submit(
      "PUT",
      "/accounts/" + account.id + "/automation",
      frozen.current.body,
    );
    if (result?.ok) {
      frozen.current = null;
      setFresh(result.value);
      onChanged();
    } else if (
      result?.code === "version_conflict" ||
      result?.code === "conflict"
    ) {
      setConflict(true);
      onChanged();
    }
  }
  return (
    <form
      className="investment-form"
      onSubmit={(e) => {
        e.preventDefault();
        void submit(true);
      }}
    >
      <h2>自动交易</h2>
      <p>
        {current.automationEnabled
          ? "已启用：每个交易日收盘后评估，下一常规开盘模拟执行。"
          : "当前已暂停，不会新增自动买入。"}
      </p>
      <p>
        已生效订单暂停后仍可能补记成交；暂停不会强制卖出现有持仓。配置变更于下一常规开盘生效。
      </p>
      {account.pendingConfig ? (
        <p>待生效配置：{account.pendingConfig.effectiveAt}</p>
      ) : null}
      <div className="investment-fields">
        <VersionSelect
          client={client}
          resource="strategies"
          label="策略版本"
          value={strategy}
          onChange={setStrategy}
        />
        <VersionSelect
          client={client}
          resource="universes"
          label="股票池版本"
          value={universe}
          onChange={setUniverse}
        />
      </div>
      <div className="investment-fields">
        {limits.map(({ key, label, max }) => (
          <label key={key}>
            {label} %
            <input
              type="number"
              required
              min="0.01"
              max={max}
              step="0.01"
              value={policy[key] * 100}
              onChange={(e) =>
                setPolicy({ ...policy, [key]: Number(e.target.value) / 100 })
              }
            />
          </label>
        ))}
      </div>
      <p>
        默认限制：单股 10%、行业 30%、股票 80%、回撤 15%、止损
        10%、每日普通双边换手
        20%。只允许收紧。止损和降风险卖出可绕过普通换手限制并记录原因。
      </p>
      {error ? (
        <p role="alert">{error}</p>
      ) : mutation.result && !mutation.result.ok ? (
        <p role="alert">{failureText(mutation.result.code)}</p>
      ) : null}
      <div className="investment-toolbar">
        <button
          className="btn-primary"
          disabled={mutation.busy || conflict}
          type="submit"
        >
          {current.automationEnabled ? "保存自动配置" : "启用自动交易"}
        </button>
        {current.automationEnabled ? (
          <button
            type="button"
            disabled={mutation.busy || conflict}
            onClick={() => void submit(false)}
          >
            暂停自动交易
          </button>
        ) : null}
        {conflict ? (
          <button
            type="button"
            onClick={async () => {
              const { decode } = await import("./fetch-investment");
              const result = await client.request<AccountView>(
                "/accounts/" + account.id,
                {},
                decode("AccountView"),
              );
              if (result.ok) {
                setFresh(result.value);
                setStrategy(result.value.strategyVersionId);
                setUniverse(result.value.universeVersionId);
                setPolicy(result.value.policy);
                setConflict(false);
                frozen.current = null;
                setError("");
                onChanged();
              } else setError(failureText(result.code));
            }}
          >
            重新加载配置
          </button>
        ) : null}
      </div>
    </form>
  );
}
