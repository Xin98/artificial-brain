import { AccountDirectory } from "../../../../features/investment/account-panel";
export default async function AccountsPage({
  searchParams,
}: {
  searchParams: Promise<{ instrument?: string }>;
}) {
  const { instrument } = await searchParams;
  return (
    <main data-page="investment">
      <header className="page-header">
        <h1>模拟账户</h1>
        <p className="page-lede">
          用独立资金账本观察日线策略，全部交易均为模拟。
        </p>
      </header>
      <AccountDirectory
        prefillInstrumentId={typeof instrument === "string" ? instrument : ""}
      />
    </main>
  );
}
