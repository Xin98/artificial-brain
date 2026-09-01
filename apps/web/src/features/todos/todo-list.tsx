"use client";

import { useEffect, useState } from "react";

import { listTodos } from "./fetch-todos";
import type { Todo, TodoFilters } from "./fetch-todos";
import { TodoActions } from "./todo-actions";

// formatDue renders a compact local due instant ("8月19日 15:00"), adding the
// year when it differs from the current one.
function formatDue(dueAtUtc: string): string {
  const due = new Date(dueAtUtc);
  if (Number.isNaN(due.getTime())) {
    return dueAtUtc;
  }
  const sameYear = due.getFullYear() === new Date().getFullYear();
  return due.toLocaleString(undefined, {
    year: sameYear ? undefined : "numeric",
    month: "short",
    day: "numeric",
    hour: "2-digit",
    minute: "2-digit",
  });
}

// TodoList renders filtered todos with combinable AND filters; deleted todos
// never arrive from the API. Loading shows skeleton rows and an empty result
// renders a composed empty state.
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
  const [loading, setLoading] = useState(true);
  const [reloadKey, setReloadKey] = useState(0);
  const [filters, setFilters] = useState<TodoFilters>({});

  useEffect(() => {
    let cancelled = false;
    void listTodos("", fetcher, filters).then((result) => {
      if (cancelled) {
        return;
      }
      setLoading(false);
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
    setLoading(true);
    setFilters({
      keyword: keyword === "" ? undefined : keyword,
      status: status === "" ? undefined : status,
      noDue: noDue || undefined,
    });
  }

  return (
    <section aria-label="待办列表" className="todo-list">
      <form className="todo-filters" onSubmit={applyFilters}>
        <div className="filter-field">
          <label htmlFor="todo-filter-keyword">关键词</label>
          <input
            id="todo-filter-keyword"
            onChange={(event) => setKeyword(event.target.value)}
            type="text"
            value={keyword}
          />
        </div>
        <div className="filter-field">
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
        </div>
        <span className="filter-check">
          <label htmlFor="todo-filter-nodue">
            <input
              checked={noDue}
              id="todo-filter-nodue"
              onChange={(event) => setNoDue(event.target.checked)}
              type="checkbox"
            />
            无到期时间
          </label>
        </span>
        <button className="btn-primary" type="submit">
          筛选
        </button>
      </form>
      {error ? (
        <p aria-live="polite" className="todo-error" role="alert">
          {error}
        </p>
      ) : null}
      {loading ? (
        <ul aria-label="加载中" className="list-skeleton">
          {Array.from({ length: 3 }, (_unused, index) => (
            <li key={index}>
              <span className="skeleton skeleton-line" />
            </li>
          ))}
        </ul>
      ) : error === null && todos.length === 0 ? (
        <p className="list-empty">暂无待办。新建一条,或调整筛选条件。</p>
      ) : (
        <ul>
          {todos.map((todo) => (
            <li
              className={
                todo.status === "completed"
                  ? "todo-item todo-item-done"
                  : "todo-item"
              }
              key={todo.id}
            >
              <span className="todo-main">
                <span className="todo-title">{todo.title}</span>
                {todo.dueAtUtc ? (
                  <time className="todo-due" dateTime={todo.dueAtUtc}>
                    {formatDue(todo.dueAtUtc)}
                  </time>
                ) : (
                  <span className="todo-nodue">无到期时间</span>
                )}
              </span>
              {todo.overdue ? (
                <span className="badge badge-danger">已逾期</span>
              ) : null}
              <TodoActions
                fetcher={fetcher}
                onChanged={() => {
                  setLoading(true);
                  setReloadKey((key) => key + 1);
                }}
                todo={todo}
              />
            </li>
          ))}
        </ul>
      )}
    </section>
  );
}
