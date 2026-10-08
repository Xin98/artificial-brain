"use client";
import { useState } from "react";
import type { NAVPoint } from "./types";
export function cents(text: string): bigint | null {
  if (!/^[0-9]+\.[0-9]{2}$/.test(text) || text.length > 24) return null;
  return BigInt(text.replace(".", ""));
}
function values(series: NAVPoint[]): bigint[] | null {
  let previous = -Infinity;
  const out: bigint[] = [];
  for (const p of series) {
    const v = cents(p.nav);
    const date = Date.parse(p.sessionDate);
    if (v === null || !Number.isFinite(date) || date <= previous) return null;
    out.push(v);
    previous = date;
  }
  return out;
}
export function PerformanceChart({
  series,
  benchmarkSeries = [],
  title,
  sourceLabel = "",
}: {
  series: NAVPoint[];
  benchmarkSeries?: NAVPoint[];
  title: string;
  sourceLabel?: string;
}) {
  const [page, setPage] = useState(0);
  if (series.length === 0) return <p>暂无已确认净资产曲线。</p>;
  const own = values(series);
  const bench = values(benchmarkSeries);
  if (!own || !bench)
    return <p role="alert">净资产数据不完整，无法绘制曲线。</p>;
  const all = [...own, ...bench];
  const min = all.reduce((a, b) => (a < b ? a : b));
  const max = all.reduce((a, b) => (a > b ? a : b));
  const span = max - min;
  const y = (v: bigint) =>
    span === 0n ? 140 : 250 - Number(((v - min) * 22000n) / span) / 100;
  const x = (i: number) =>
    series.length === 1 ? 420 : 70 + (i * 740) / (series.length - 1);
  const line = own
    .map((v, i) => x(i).toFixed(2) + "," + y(v).toFixed(2))
    .join(" ");
  const indices = new Map(series.map((p, i) => [p.sessionDate, i]));
  const benchmark = bench
    .map((v, i) => {
      const index = indices.get(benchmarkSeries[i].sessionDate);
      return index === undefined
        ? ""
        : x(index).toFixed(2) + "," + y(v).toFixed(2);
    })
    .filter(Boolean)
    .join(" ");
  const displayed = series.slice(page * 25, (page + 1) * 25);
  return (
    <figure className="investment-chart">
      <figcaption>
        {title} · {sourceLabel} · USD · 按交易日排列
      </figcaption>
      <svg role="img" aria-label={title} viewBox="0 0 850 290">
        <title>{title}；实线为策略，虚线为 SPY 基准</title>
        <line
          x1="70"
          y1="250"
          x2="810"
          y2="250"
          stroke="currentColor"
          opacity=".25"
        />
        <text x="70" y="20">
          {series[own.indexOf(max)]?.nav ??
            benchmarkSeries[bench.indexOf(max)]?.nav}
        </text>
        {own.length === 1 ? (
          <circle cx={x(0)} cy={y(own[0])} r="4" fill="var(--accent)" />
        ) : (
          <polyline
            points={line}
            fill="none"
            stroke="var(--accent)"
            strokeWidth="2.5"
          />
        )}
        {benchmark ? (
          <polyline
            points={benchmark}
            fill="none"
            stroke="currentColor"
            strokeWidth="1.8"
            strokeDasharray="6 4"
          />
        ) : null}
        <text x="70" y="280">
          {series[0].sessionDate.slice(0, 10)}
        </text>
        <text x="810" y="280" textAnchor="end">
          {series.at(-1)!.sessionDate.slice(0, 10)}
        </text>
      </svg>
      <p>
        实线：策略净资产。
        {benchmark ? "虚线：SPY 净资产。" : "基准曲线未提供。"}
      </p>
      <details>
        <summary>查看精确每日净资产</summary>
        <div className="investment-table-wrap">
          <table className="investment-table">
            <thead>
              <tr>
                <th>交易日期</th>
                <th>策略 USD</th>
                <th>SPY USD</th>
              </tr>
            </thead>
            <tbody>
              {displayed.map((p) => (
                <tr key={p.sessionDate}>
                  <td>{p.sessionDate.slice(0, 10)}</td>
                  <td>{p.nav}</td>
                  <td>
                    {benchmarkSeries.find(
                      (b) => b.sessionDate === p.sessionDate,
                    )?.nav ?? "缺失"}
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
        <div className="investment-toolbar">
          {page > 0 ? (
            <button onClick={() => setPage((p) => p - 1)}>上一页净资产</button>
          ) : null}
          {(page + 1) * 25 < series.length ? (
            <button onClick={() => setPage((p) => p + 1)}>下一页净资产</button>
          ) : null}
        </div>
      </details>
    </figure>
  );
}
export function DrawdownChart({
  series,
  initialCash,
  sourceLabel = "",
}: {
  series: NAVPoint[];
  initialCash: string;
  sourceLabel?: string;
}) {
  const own = values(series);
  let peak = cents(initialCash);
  if (!own || peak === null || peak <= 0n || own.length === 0) return null;
  const percentages: number[] = [];
  for (const v of own) {
    if (v > peak) peak = v;
    percentages.push(Number(((peak - v) * 1000000n) / peak) / 10000);
  }
  const max = Math.max(...percentages, 1);
  const points = percentages
    .map(
      (v, i) =>
        (own.length === 1 ? 420 : 70 + (i * 740) / (own.length - 1)).toFixed(
          2,
        ) +
        "," +
        (35 + (v / max) * 180).toFixed(2),
    )
    .join(" ");
  return (
    <figure className="investment-chart">
      <figcaption>相对历史净资产峰值的回撤 · {sourceLabel}</figcaption>
      <svg role="img" aria-label="回撤" viewBox="0 0 850 250">
        <title>回撤曲线，包含初始资金峰值</title>
        <line
          x1="70"
          y1="35"
          x2="810"
          y2="35"
          stroke="currentColor"
          opacity=".3"
        />
        <polyline
          points={points}
          fill="none"
          stroke="var(--danger)"
          strokeWidth="2"
        />
        <text x="70" y="25">
          0%
        </text>
        <text x="70" y="245">
          最大回撤 {Math.max(...percentages).toFixed(2)}%
        </text>
      </svg>
    </figure>
  );
}
