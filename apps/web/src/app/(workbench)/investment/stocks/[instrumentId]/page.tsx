import { StockAnalysis } from "../../../../../features/investment/stock-analysis";
export default async function StockPage({
  params,
  searchParams,
}: {
  params: Promise<{ instrumentId: string }>;
  searchParams: Promise<{ accountId?: string }>;
}) {
  const { instrumentId } = await params;
  const { accountId } = await searchParams;
  return (
    <main data-page="investment">
      <StockAnalysis instrumentId={instrumentId} accountId={accountId} />
    </main>
  );
}
