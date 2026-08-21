import { DataPanel } from "../../../features/data/data-panel";

export default function DataPage(): React.JSX.Element {
  return (
    <main data-page="data">
      <h1>数据</h1>
      <DataPanel />
    </main>
  );
}
