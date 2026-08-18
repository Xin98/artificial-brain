"use client";

import { useEffect, useState } from "react";

import { listTodos } from "./fetch-todos";
import type { Todo, TodoFilters } from "./fetch-todos";
import { TodoActions } from "./todo-actions";

// TodoList renders filtered todos with combinable AND filters; deleted todos
// never arrive from the API.
export function TodoList({
  fetcher = fetch,
}: {
  fetcher?: typeof fetch;
}): React.JSX.Element {
  const [todos, setTodos] = useState<Todo[]>([]);
  const [keyword, setKeyword] = useState("");
  const [status, setStatus] = useState("");
  const [noDue, setNoDue] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [reloadKey, setReloadKey] = useState(0);
  const [filters, setFilters] = useState<TodoFilters>({});

  useEffect(() => {
    let cancelled = false;
    void listTodos("", fetcher, filters).then((result) => {
      if (cancelled) {
        return;
      }
      if (result === null) {
        setError("待办加载失败,请稍后再试。");
        return;
      }
      setError(null);
      setTodos(result);
    });
    return () => {
      cancelled = true;
    };
  }, [fetcher, filters, reloadKey]);

  function applyFilters(event: React.FormEvent): void {
    event.preventDefault();
    setFilters({
      keyword: keyword === "" ? undefined : keyword,
      status: status === "" ? undefined : status,
      noDue: noDue || undefined,
    });
  }

  return (
    <section aria-label="待办列表" className="todo-list">
      <form className="todo-filters" onSubmit={applyFilters}>
        <label htmlFor="todo-filter-keyword">关键词</label>
        <input
          id="todo-filter-keyword"
          onChange={(event) => setKeyword(event.target.value)}
          type="text"
          value={keyword}
        />
        <label htmlFor="todo-filter-status">状态</label>
        <select
          id="todo-filter-status"
          onChange={(event) => setStatus(event.target.value)}
          value={status}
        >
          <option value="">全部</option>
          <option value="pending">待处理</option>
          <option value="completed">已完成</option>
        </select>
        <label htmlFor="todo-filter-nodue">
          <input
            checked={noDue}
            id="todo-filter-nodue"
            onChange={(event) => setNoDue(event.target.checked)}
            type="checkbox"
          />
          无到期时间
        </label>
        <button type="submit">筛选</button>
      </form>
      {error ? (
        <p aria-live="polite" className="todo-error" role="alert">
          {error}
        </p>
      ) : null}
      <ul>
        {todos.map((todo) => (
          <li className="todo-item" key={todo.id}>
            <span className="todo-title">{todo.title}</span>
            {todo.dueAtUtc ? (
              <time dateTime={todo.dueAtUtc}>
                {new Date(todo.dueAtUtc).toLocaleString()}
              </time>
            ) : null}
            {todo.overdue ? <span className="todo-overdue">已逾期</span> : null}
            <TodoActions
              fetcher={fetcher}
              onChanged={() => setReloadKey((key) => key + 1)}
              todo={todo}
            />
          </li>
        ))}
      </ul>
    </section>
  );
}
