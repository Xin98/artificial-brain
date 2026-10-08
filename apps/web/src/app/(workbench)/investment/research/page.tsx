import { BacktestPanel } from "../../../../features/investment/backtest-panel";
export default async function ResearchPage({
  searchParams,
}: {
  searchParams: Promise<{ runId?: string }>;
}) {
  const { runId } = await searchParams;
  return (
    <main data-page="investment">
      <header className="page-header">
        <h1>回测实验</h1>
        <p className="page-lede">
          按可知时点回放历史，比较策略与同口径 SPY 基准。
        </p>
      </header>
      <BacktestPanel initialRunId={runId} key={runId ?? ""} />
    </main>
  );
}
