import { DashboardPanel } from "../../features/dashboard/dashboard-panel";

export default function DashboardPage(): React.JSX.Element {
  return (
    <main data-page="dashboard">
      <h1>仪表盘</h1>
      <DashboardPanel />
    </main>
  );
}
