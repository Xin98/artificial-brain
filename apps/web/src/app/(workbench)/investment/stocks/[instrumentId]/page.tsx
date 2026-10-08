import { StockAnalysis } from "../../../../../features/investment/stock-analysis";
export default async function StockPage({
  params,
  searchParams,
}: {
  params: Promise<{ instrumentId: string }>;
  searchParams: Promise<{ accountId?: string; from?: string }>;
}) {
  const { instrumentId } = await params;
  const { accountId, from } = await searchParams;
  return (
    <main data-page="investment">
      <StockAnalysis
        instrumentId={instrumentId}
        accountId={accountId}
        returnQuery={typeof from === "string" ? from : ""}
      />
    </main>
  );
}
