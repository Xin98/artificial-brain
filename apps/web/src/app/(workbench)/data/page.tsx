import { DataPanel } from "../../../features/data/data-panel";

export default function DataPage(): React.JSX.Element {
  return (
    <main data-page="data">
      <header className="page-header">
        <h1>数据</h1>
        <p className="page-lede">导出或导入你的工作区数据。</p>
      </header>
      <DataPanel />
    </main>
  );
}
