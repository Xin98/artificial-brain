import { TodosPanel } from "../../../features/todos/todos-panel";

export default function TodosPage(): React.JSX.Element {
  return (
    <main data-page="todos">
      <h1>待办</h1>
      <TodosPanel />
    </main>
  );
}
