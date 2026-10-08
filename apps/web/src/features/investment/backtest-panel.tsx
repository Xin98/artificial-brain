"use client";
import Link from "next/link";
import { useState } from "react";
import {
  failureText,
  investmentClient,
  type InvestmentClient,
} from "./fetch-investment";
import { useMutation, useResource } from "./hooks";
import type { BacktestsPage, RunView } from "./types";
import { VersionSelect } from "./account-form";
import { PerformancePanel } from "./performance-panel";
export function BacktestPanel({
  client = investmentClient,
  initialRunId = "",
}: {
  client?: InvestmentClient;
  initialRunId?: string;
}) {
  const [universe, setUniverse] = useState("");
  const [strategy, setStrategy] = useState("");
  const [from, setFrom] = useState("2026-01-01");
  const [to, setTo] = useState("2026-07-31");
  const [cash, setCash] = useState("100000.00");
  const [selected, setSelected] = useState(initialRunId);
  const [cursor, setCursor] = useState("");
  const [error, setError] = useState("");
  const mutation = useMutation<RunView>(client, "RunView");
  const runs = useResource<BacktestsPage>(
    client,
    "/backtests?cursor=" + encodeURIComponent(cursor),
    "BacktestsPage",
    true,
  );
  return (
    <div className="investment-layout">
      <nav className="investment-tabs" aria-label="投资模块">
        <Link href="/investment">股票研究</Link>
        <Link href="/investment/accounts">模拟账户</Link>
        <Link href="/investment/research" aria-current="page">
          回测实验
        </Link>
      </nav>
      <p>
        回测使用独立资金账本，不改变模拟账户。至少需要 201 日预热和 20
        个执行交易日。
      </p>
      <form
        className="investment-form"
        onSubmit={async (e) => {
          e.preventDefault();
          if (
            !universe ||
            !strategy ||
            !/^[0-9]+\.[0-9]{2}$/.test(cash) ||
            BigInt(cash.replace(".", "")) <= 0n ||
            BigInt(cash.replace(".", "")) > 100000000000n ||
            Date.parse(from) > Date.parse(to)
          ) {
            setError(
              "请选择股票池和策略版本，输入有效日期与两位小数的正资金。",
            );
            return;
          }
          setError("");
          const result = await mutation.submit("POST", "/backtests", {
            universeVersionId: universe,
            strategyVersionId: strategy,
            from: from + "T00:00:00Z",
            to: to + "T00:00:00Z",
            initialCash: cash,
          });
          if (result?.ok) {
            setSelected(result.value.runId);
            runs.retry();
          }
        }}
      >
        <h2>新建历史回测</h2>
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
          <label>
            开始日期
            <input
              type="date"
              required
              value={from}
              onChange={(e) => setFrom(e.target.value)}
            />
          </label>
          <label>
            结束日期
            <input
              type="date"
              required
              value={to}
              onChange={(e) => setTo(e.target.value)}
            />
          </label>
          <label>
            回测初始资金 USD
            <input
              inputMode="decimal"
              required
              value={cash}
              onChange={(e) => setCash(e.target.value)}
            />
          </label>
        </div>
        <p>固定股票池有幸存者偏差；结果不能证明未来收益或幻方模型效果。</p>
        {error ? (
          <p role="alert">{error}</p>
        ) : mutation.result && !mutation.result.ok ? (
          <p role="alert">{failureText(mutation.result.code)}</p>
        ) : null}
        <button className="btn-primary" disabled={mutation.busy}>
          开始回测
        </button>
      </form>
      {selected ? (
        <PerformancePanel
          key={selected}
          client={client}
          resource="backtest"
          id={selected}
        />
      ) : null}
      <section className="investment-section">
        <h2>我的回测记录</h2>
        {!runs.result ? (
          <p role="status">正在读取回测记录…</p>
        ) : !runs.result.ok ? (
          <p role="alert">
            {failureText(runs.result.code)}{" "}
            <button onClick={runs.retry}>重试回测列表</button>
          </p>
        ) : (
          <>
            <div className="investment-table-wrap">
              <table className="investment-table">
                <thead>
                  <tr>
                    <th>研究区间</th>
                    <th>状态</th>
                    <th>数据版本</th>
                    <th>结果</th>
                  </tr>
                </thead>
                <tbody>
                  {runs.result.value.items.map((r) => (
                    <tr key={r.runId}>
                      <td>
                        {r.from.slice(0, 10)} 至 {r.to.slice(0, 10)}
                      </td>
                      <td>
                        {r.status}
                        <small>{r.reason}</small>
                      </td>
                      <td>{r.datasetVersion}</td>
                      <td>
                        <button onClick={() => setSelected(r.runId)}>
                          查看回测
                        </button>
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
            {runs.result.value.items.length === 0 ? (
              <p>暂无历史回测。</p>
            ) : null}
            <div className="investment-toolbar">
              {runs.result.value.nextCursor ? (
                <button
                  onClick={() =>
                    setCursor(
                      runs.result!.ok ? runs.result!.value.nextCursor : "",
                    )
                  }
                >
                  下一页回测
                </button>
              ) : null}
              {cursor ? (
                <button onClick={() => setCursor("")}>返回回测首屏</button>
              ) : null}
            </div>
          </>
        )}
      </section>
    </div>
  );
}
