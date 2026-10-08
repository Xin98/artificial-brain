import { AccountDirectory } from "../../../../features/investment/account-panel";
export default function AccountsPage() {
  return (
    <main data-page="investment">
      <header className="page-header">
        <h1>模拟账户</h1>
        <p className="page-lede">
          用独立资金账本观察日线策略，全部交易均为模拟。
        </p>
      </header>
      <AccountDirectory />
    </main>
  );
}
