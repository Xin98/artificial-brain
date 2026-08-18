"use client";

import { useState } from "react";

import { TodoForm } from "./todo-form";
import { TodoList } from "./todo-list";

// TodosPanel pairs the create form with the list; finishing a form
// remounts the list so it reloads.
export function TodosPanel({
  fetcher = fetch,
}: {
  fetcher?: typeof fetch;
}): React.JSX.Element {
  const [reloadKey, setReloadKey] = useState(0);

  return (
    <div className="todos-panel">
      <TodoForm
        fetcher={fetcher}
        onDone={() => setReloadKey((key) => key + 1)}
      />
      <TodoList fetcher={fetcher} key={reloadKey} />
    </div>
  );
}
