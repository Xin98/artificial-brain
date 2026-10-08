"use client";
import { useState } from "react";
import type { PricePoint } from "./types";
export function micros(text: string): bigint | null {
  if (!/^[0-9]+\.[0-9]{6}$/.test(text) || text.length > 24) return null;
  return BigInt(text.replace(".", ""));
}
export function trimPrice(close: string): string {
  if (!/^[0-9]+\.[0-9]{6}$/.test(close)) return close;
  const trimmed = close.replace(/0+$/, "");
  const fraction = trimmed.split(".")[1] ?? "";
  return fraction.length >= 2
    ? trimmed
    : trimmed + "0".repeat(2 - fraction.length);
}
export function PriceChart({
  series,
  sourceLabel = "",
}: {
  series: PricePoint[];
  sourceLabel?: string;
}) {
  const [page, setPage] = useState(0);
  if (series.length === 0) return <p>暂无已知价格。</p>;
  const closes: bigint[] = [];
  let previous = -Infinity;
  for (const point of series) {
    const close = micros(point.close);
    const date = Date.parse(point.sessionDate);
    if (close === null || !Number.isFinite(date) || date <= previous)
      return <p role="alert">价格数据不完整，无法绘制曲线。</p>;
    closes.push(close);
    previous = date;
  }
  const min = closes.reduce((a, b) => (a < b ? a : b));
  const max = closes.reduce((a, b) => (a > b ? a : b));
  const span = max - min;
  const y = (v: bigint) =>
    span === 0n ? 140 : 250 - Number(((v - min) * 22000n) / span) / 100;
  const x = (i: number) =>
    series.length === 1 ? 420 : 70 + (i * 740) / (series.length - 1);
  const line = closes
    .map((v, i) => x(i).toFixed(2) + "," + y(v).toFixed(2))
    .join(" ");
  const last = series[series.length - 1];
  const displayed = series.slice(page * 25, (page + 1) * 25);
  return (
    <figure className="investment-chart">
      <figcaption>价格走势 · {sourceLabel} · USD · 按交易日排列</figcaption>
      <p>
        最新收盘价 {trimPrice(last.close)} USD · {last.sessionDate.slice(0, 10)}
        （以可知日聚合收盘为准，非实时报价）
      </p>
      <svg role="img" aria-label="价格走势" viewBox="0 0 850 290">
        <title>最近交易日收盘价曲线</title>
        <line
          x1="70"
          y1="250"
          x2="810"
          y2="250"
          stroke="currentColor"
          opacity=".25"
        />
        <text x="70" y="20">
          {trimPrice(series[closes.indexOf(max)].close)}
        </text>
        <text x="70" y="270" opacity=".7">
          {trimPrice(series[closes.indexOf(min)].close)}
        </text>
        {closes.length === 1 ? (
          <circle cx={x(0)} cy={y(closes[0])} r="4" fill="var(--accent)" />
        ) : (
          <polyline
            points={line}
            fill="none"
            stroke="var(--accent)"
            strokeWidth="2.5"
          />
        )}
        <text x="70" y="288">
          {series[0].sessionDate.slice(0, 10)}
        </text>
        <text x="810" y="288" textAnchor="end">
          {last.sessionDate.slice(0, 10)}
        </text>
      </svg>
      <details>
        <summary>查看精确每日收盘价</summary>
        <div className="investment-table-wrap">
          <table className="investment-table">
            <thead>
              <tr>
                <th>交易日期</th>
                <th>收盘 USD</th>
              </tr>
            </thead>
            <tbody>
              {displayed.map((p) => (
                <tr key={p.sessionDate}>
                  <td>{p.sessionDate.slice(0, 10)}</td>
                  <td>{trimPrice(p.close)}</td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
        <div className="investment-toolbar">
          {page > 0 ? (
            <button onClick={() => setPage((p) => p - 1)}>上一页价格</button>
          ) : null}
          {(page + 1) * 25 < series.length ? (
            <button onClick={() => setPage((p) => p + 1)}>下一页价格</button>
          ) : null}
        </div>
      </details>
    </figure>
  );
}
