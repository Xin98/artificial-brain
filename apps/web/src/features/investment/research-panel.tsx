"use client";
import Link from "next/link";
import { useRouter, useSearchParams } from "next/navigation";
import { useEffect, useState } from "react";
import {
  investmentClient,
  failureText,
  type InvestmentClient,
} from "./fetch-investment";
import { useCursorPager, useDebouncedValue, useResource } from "./hooks";
import type { DataStatus, InstrumentsPage } from "./types";
import {
  dataStateText,
  reasonText,
  SourceNotice,
  StatusBadge,
} from "./status-badge";
import { UniverseForm, StrategyForm, SyncForm } from "./universe-form";
export function ResearchPanel({
  client = investmentClient,
}: {
  client?: InvestmentClient;
}) {
  const router = useRouter();
  const searchParams = useSearchParams();
  const [search, setSearch] = useState(() => searchParams.get("search") ?? "");
  const [risk, setRisk] = useState(() => searchParams.get("risk") ?? "");
  const [potential, setPotential] = useState(
    () => searchParams.get("potential") ?? "",
  );
  const pager = useCursorPager();
  const debouncedSearch = useDebouncedValue(search, 300);
  useEffect(() => {
    const params = new URLSearchParams();
    if (debouncedSearch) params.set("search", debouncedSearch);
    if (risk) params.set("risk", risk);
    if (potential) params.set("potential", potential);
    const next = params.toString();
    if (next !== searchParams.toString())
      router.replace(next ? "/investment?" + next : "/investment", {
        scroll: false,
      });
  }, [debouncedSearch, risk, potential, router, searchParams]);
  const query = new URLSearchParams({
    search: debouncedSearch,
    risk,
    potential,
    cursor: pager.cursor,
  });
  const status = useResource<DataStatus>(client, "/data-status", "DataStatus");
  const rows = useResource<InstrumentsPage>(
    client,
    "/instruments?" + query,
    "InstrumentsPage",
  );
  const refreshing = !rows.result && rows.lastOk !== null;
  const page = rows.result?.ok ? rows.result.value : rows.lastOk;
  const filterQuery = [
    debouncedSearch ? "search=" + encodeURIComponent(debouncedSearch) : "",
    risk ? "risk=" + encodeURIComponent(risk) : "",
    potential ? "potential=" + encodeURIComponent(potential) : "",
  ]
    .filter(Boolean)
    .join("&");
  return (
    <div className="investment-layout">
      <nav className="investment-tabs" aria-label="投资模块">
        <Link href="/investment" aria-current="page">
          股票研究
        </Link>
        <Link href="/investment/accounts">模拟账户</Link>
        <Link href="/investment/research">回测实验</Link>
      </nav>
      {status.result?.ok ? (
        <>
          <SourceNotice data={status.result.value} />
          <div className="investment-data-status">
            {Object.entries({
              行情: status.result.value.market,
              财报: status.result.value.financial,
              新闻: status.result.value.news,
              日历: status.result.value.calendar,
            }).map(([name, v]) => (
              <span key={name}>
                {name}：
                {v.state === "available"
                  ? "可用"
                  : v.reason
                    ? reasonText(v.reason)
                    : dataStateText(v.state)}
              </span>
            ))}
          </div>
        </>
      ) : status.result ? (
        <p role="alert">
          {failureText(status.result.code)}{" "}
          <button onClick={status.retry}>重试数据状态</button>
        </p>
      ) : (
        <p role="status">正在读取数据来源…</p>
      )}
      <section>
        <h2>量化排名</h2>
        <p>
          动量 30% · 趋势 20% · 低波动 15% · 估值 15% · 质量
          20%。评分是股票池内的相对排名，不代表盈利概率。
        </p>
        <div className="investment-toolbar">
          <label>
            搜索股票
            <input
              value={search}
              onChange={(e) => {
                setSearch(e.target.value);
                pager.reset();
              }}
            />
          </label>
          <label>
            风险
            <select
              value={risk}
              onChange={(e) => {
                setRisk(e.target.value);
                pager.reset();
              }}
            >
              <option value="">全部风险</option>
              <option value="low">低</option>
              <option value="medium">中</option>
              <option value="high">高</option>
              <option value="unknown">未知</option>
            </select>
          </label>
          <label>
            潜力
            <select
              value={potential}
              onChange={(e) => {
                setPotential(e.target.value);
                pager.reset();
              }}
            >
              <option value="">全部潜力</option>
              <option value="high">较高</option>
              <option value="observe">待观察</option>
              <option value="weak">偏弱</option>
              <option value="unknown">未知</option>
            </select>
          </label>
        </div>
        {!rows.result && !page ? (
          <p role="status">正在评估股票…</p>
        ) : rows.result && !rows.result.ok ? (
          <p role="alert">
            {failureText(rows.result.code)}{" "}
            <button onClick={rows.retry}>重试排名</button>
          </p>
        ) : (
          <>
            {refreshing ? <p role="status">正在更新排名…</p> : null}
            <div className="investment-table-wrap">
              <table className="investment-table">
                <caption>潜力与风险独立展示；高潜力也可能伴随高风险。</caption>
                <thead>
                  <tr>
                    <th>排名</th>
                    <th>股票</th>
                    <th>评分</th>
                    <th>收盘价 USD</th>
                    <th>潜力</th>
                    <th>风险</th>
                  </tr>
                </thead>
                <tbody>
                  {page!.items.map((v) => (
                    <tr key={v.instrument.id}>
                      <td>{v.signal?.rank ?? "未入选"}</td>
                      <td>
                        <Link
                          href={
                            "/investment/stocks/" +
                            encodeURIComponent(v.instrument.id) +
                            (filterQuery
                              ? "?from=" + encodeURIComponent(filterQuery)
                              : "")
                          }
                        >
                          {v.instrument.ticker}
                        </Link>
                        <small>{v.instrument.name}</small>
                      </td>
                      <td>
                        {v.signal?.score.toFixed(1) ?? "不适用"}
                        <small>{v.reason}</small>
                      </td>
                      <td>{v.price ?? "缺失"}</td>
                      <td>
                        <StatusBadge kind="potential" value={v.potential} />
                      </td>
                      <td>
                        <StatusBadge kind="risk" value={v.risk.level} />
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
            {page!.items.length === 0 ? (
              debouncedSearch || risk || potential ? (
                <p>没有匹配当前筛选条件的股票，可调整搜索或风险/潜力筛选。</p>
              ) : (
                <p>
                  暂无股票。真实行情模式请先配置证券 ID 股票池，再同步数据。
                </p>
              )
            ) : null}
            <div className="investment-toolbar">
              {pager.canPrev ? (
                <button onClick={pager.prev}>上一页股票</button>
              ) : null}
              {page!.nextCursor ? (
                <button onClick={() => pager.next(page!.nextCursor)}>
                  下一页股票
                </button>
              ) : null}
              {pager.canPrev ? (
                <button onClick={pager.reset}>返回首屏</button>
              ) : null}
            </div>
          </>
        )}
      </section>
      <details className="investment-section">
        <summary>股票池与策略版本</summary>
        <UniverseForm client={client} onCreated={() => rows.retry()} />
        <StrategyForm client={client} />
        <SyncForm client={client} />
      </details>
    </div>
  );
}
