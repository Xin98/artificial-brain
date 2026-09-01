import { DashboardPanel } from "../../features/dashboard/dashboard-panel";

export default function DashboardPage(): React.JSX.Element {
  return (
    <main data-page="dashboard">
      <header className="page-header">
        <h1>仪表盘</h1>
        <p className="page-lede">当前工作区的待办与提醒投递概况。</p>
      </header>
      <DashboardPanel />
    </main>
  );
}
