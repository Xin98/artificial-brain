"use client";
import { useState } from "react";
import { failureText, type InvestmentClient } from "./fetch-investment";
import { useMutation, useResource } from "./hooks";
import type {
  RunView,
  VersionView,
  VersionsPage,
  StrategyParameters,
} from "./types";
export function UniverseForm({
  client,
  onCreated,
}: {
  client: InvestmentClient;
  onCreated: (v: VersionView) => void;
}) {
  const [name, setName] = useState("");
  const [ids, setIds] = useState("");
  const [previous, setPrevious] = useState("");
  const [error, setError] = useState("");
  const versions = useResource<VersionsPage>(
    client,
    "/universes",
    "VersionsPage",
  );
  const mutation = useMutation<VersionView>(client, "VersionView");
  return (
    <form
      className="investment-form"
      onSubmit={async (e) => {
        e.preventDefault();
        const members = [...new Set(ids.split(/[\s,]+/).filter(Boolean))];
        if (members.length < 1 || members.length > 100) {
          setError("股票池必须包含 1 至最多 100 个证券 ID。");
          return;
        }
        setError("");
        const result = await mutation.submit(
          "POST",
          previous ? "/universes/" + previous + "/versions" : "/universes",
          { name, instrumentIds: members },
        );
        if (result?.ok) {
          onCreated(result.value);
          versions.retry();
        }
      }}
    >
      <h3>选股池</h3>
      <label>
        股票池名称
        <input
          required
          maxLength={100}
          value={name}
          onChange={(e) => setName(e.target.value)}
        />
      </label>
      <label>
        证券 ID
        <textarea
          required
          value={ids}
          onChange={(e) => setIds(e.target.value)}
          aria-describedby="investment-universe-help"
        />
      </label>
      <p id="investment-universe-help">
        逗号或空格分隔，最多 100 个普通股。演示 ID 如
        fixture-01；实源初次同步可输入代码，同步后使用返回的稳定 ID。
      </p>
      <label>
        版本归属
        <select value={previous} onChange={(e) => setPrevious(e.target.value)}>
          <option value="">创建新股票池</option>
          {versions.result?.ok
            ? versions.result.value.items.map((v) => (
                <option key={v.id} value={v.id}>
                  {v.name} · {v.id}
                </option>
              ))
            : null}
        </select>
      </label>
      {error ? (
        <p role="alert">{error}</p>
      ) : mutation.result && !mutation.result.ok ? (
        <p role="alert">{failureText(mutation.result.code)}</p>
      ) : mutation.result?.ok ? (
        <p role="status">已保存版本 {mutation.result.value.id}</p>
      ) : null}
      <button className="btn-primary" disabled={mutation.busy}>
        保存股票池
      </button>
    </form>
  );
}
export function StrategyForm({ client }: { client: InvestmentClient }) {
  const [weights, setWeights] = useState(["30", "20", "15", "15", "20"]);
  const [entry, setEntry] = useState("70");
  const [exit, setExit] = useState("50");
  const [band, setBand] = useState("2");
  const [max, setMax] = useState("10");
  const [error, setError] = useState("");
  const mutation = useMutation<VersionView>(client, "VersionView");
  return (
    <form
      className="investment-form"
      onSubmit={async (e) => {
        e.preventDefault();
        const w = weights.map((x) => Number(x) / 100);
        const p: StrategyParameters = {
          weights: w,
          entryScore: Number(entry),
          exitScore: Number(exit),
          rebalanceBand: Number(band) / 100,
          maxHoldings: Number(max),
        };
        if (
          Math.abs(w.reduce((a, b) => a + b, 0) - 1) > 1e-9 ||
          w.some((n) => !Number.isFinite(n) || n < 0) ||
          p.entryScore <= p.exitScore ||
          p.entryScore > 100 ||
          p.exitScore < 0 ||
          p.rebalanceBand < 0 ||
          p.rebalanceBand > 1 ||
          !Number.isInteger(p.maxHoldings) ||
          p.maxHoldings < 1 ||
          p.maxHoldings > 10
        ) {
          setError(
            "权重总和需为 100%，买入阈值须高于卖出阈值，持仓数为 1–10。",
          );
          return;
        }
        setError("");
        await mutation.submit("POST", "/strategies/multifactor-v1/versions", {
          parameters: p,
        });
      }}
    >
      <h3>多因子策略参数</h3>
      <div className="investment-fields">
        {["动量", "趋势", "低波动", "估值", "质量"].map((label, i) => (
          <label key={label}>
            {label}权重 %
            <input
              type="number"
              min="0"
              max="100"
              step="0.01"
              required
              value={weights[i]}
              onChange={(e) =>
                setWeights(
                  weights.map((v, n) => (n === i ? e.target.value : v)),
                )
              }
            />
          </label>
        ))}
        <label>
          买入分数
          <input
            type="number"
            required
            min="0"
            max="100"
            value={entry}
            onChange={(e) => setEntry(e.target.value)}
          />
        </label>
        <label>
          卖出分数
          <input
            type="number"
            required
            min="0"
            max="100"
            value={exit}
            onChange={(e) => setExit(e.target.value)}
          />
        </label>
        <label>
          再平衡容忍 %
          <input
            type="number"
            min="0"
            max="100"
            step="0.1"
            required
            value={band}
            onChange={(e) => setBand(e.target.value)}
          />
        </label>
        <label>
          最多持仓
          <input
            type="number"
            min="1"
            max="10"
            required
            value={max}
            onChange={(e) => setMax(e.target.value)}
          />
        </label>
      </div>
      {error ? (
        <p role="alert">{error}</p>
      ) : mutation.result && !mutation.result.ok ? (
        <p role="alert">{failureText(mutation.result.code)}</p>
      ) : mutation.result?.ok ? (
        <p role="status">已保存策略版本 {mutation.result.value.id}</p>
      ) : null}
      <button disabled={mutation.busy}>保存策略版本</button>
    </form>
  );
}
export function SyncForm({ client }: { client: InvestmentClient }) {
  const [from, setFrom] = useState("2023-01-01");
  const [to, setTo] = useState(new Date().toISOString().slice(0, 10));
  const [ids, setIds] = useState("");
  const [run, setRun] = useState<RunView | null>(null);
  const mutation = useMutation<RunView>(client, "RunView");
  return (
    <form
      className="investment-form"
      onSubmit={async (e) => {
        e.preventDefault();
        const result = await mutation.submit("POST", "/data-sync", {
          from: from + "T00:00:00Z",
          to: to + "T00:00:00Z",
          instrumentIds: ids.split(/[\s,]+/).filter(Boolean),
        });
        if (result?.ok) setRun(result.value);
      }}
    >
      <h3>同步只读数据</h3>
      <p>
        数据源由服务端配置。真实模式请先同步股票代码（包括比较基准
        SPY），再建立稳定 ID 股票池。
      </p>
      <div className="investment-fields">
        <label>
          起始日期
          <input
            type="date"
            required
            value={from}
            onChange={(e) => setFrom(e.target.value)}
          />
        </label>
        <label>
          截止日期
          <input
            type="date"
            required
            value={to}
            onChange={(e) => setTo(e.target.value)}
          />
        </label>
      </div>
      <label>
        股票代码或证券 ID
        <input
          value={ids}
          onChange={(e) => setIds(e.target.value)}
          placeholder="AAPL, MSFT, SPY"
        />
      </label>
      <button disabled={mutation.busy}>开始同步</button>
      {mutation.result && !mutation.result.ok ? (
        <p role="alert">{failureText(mutation.result.code)}</p>
      ) : null}
      {run ? <SyncProgress client={client} id={run.runId} /> : null}
    </form>
  );
}
function SyncProgress({
  client,
  id,
}: {
  client: InvestmentClient;
  id: string;
}) {
  const { result } = useResource<RunView>(
    client,
    "/data-sync/" + id,
    "RunView",
    true,
  );
  return (
    <p role="status">
      {result?.ok
        ? result.value.status + " · " + result.value.reason
        : result && !result.ok
          ? failureText(result.code)
          : "同步已排队"}
    </p>
  );
}
