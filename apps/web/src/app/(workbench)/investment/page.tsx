import { ResearchPanel } from "../../../features/investment/research-panel";
export default function InvestmentPage() {
  return (
    <main data-page="investment">
      <header className="page-header">
        <h1>投资</h1>
        <p className="page-lede">
          美股日线研究与模拟交易。先看量化证据，再由账户风控决定可执行动作。
        </p>
      </header>
      <ResearchPanel />
    </main>
  );
}
