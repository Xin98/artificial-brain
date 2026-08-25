import { TodosPanel } from "../../../features/todos/todos-panel";

export default function TodosPage(): React.JSX.Element {
  return (
    <main data-page="todos">
      <header className="page-header">
        <h1>待办</h1>
        <p className="page-lede">新建、筛选并完成你的承诺事项。</p>
      </header>
      <TodosPanel />
    </main>
  );
}
