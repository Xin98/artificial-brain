"use client";

import { useEffect, useState } from "react";

import { listTodos } from "./fetch-todos";
import type { Todo, TodoFilters } from "./fetch-todos";
import { TodoActions } from "./todo-actions";
import { TodoForm } from "./todo-form";

export function filtersForView(view: string, now = new Date()): TodoFilters {
  if (view === "completed7d")
    return {
      status: "completed",
      completedSince: new Date(now.getTime() - 7 * 86400000).toISOString(),
    };
  if (view === "noDue") return { status: "pending", noDue: true };
  if (view === "today" || view === "overdue") {
    const start = new Date(now.getFullYear(), now.getMonth(), now.getDate());
    const end = new Date(start);
    end.setDate(end.getDate() + 1);
    return view === "today"
      ? {
          status: "pending",
          dueFrom: start.toISOString(),
          dueTo: new Date(end.getTime() - 1000).toISOString(),
        }
      : { status: "pending", dueTo: now.toISOString() };
  }
  return view === "pending" ? { status: "pending" } : {};
}

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
  reloadVersion = 0,
  initialView = "",
}: {
  fetcher?: typeof fetch;
  reloadVersion?: number;
  initialView?: string;
}): React.JSX.Element {
  const [todos, setTodos] = useState<Todo[]>([]);
  const [keyword, setKeyword] = useState("");
  const [status, setStatus] = useState(
    filtersForView(initialView).status ?? "",
  );
  const [noDue, setNoDue] = useState(initialView === "noDue");
  const [view, setView] = useState(initialView);
  const [editing, setEditing] = useState<Todo | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [loading, setLoading] = useState(true);
  const [reloadKey, setReloadKey] = useState(0);
  const [filters, setFilters] = useState<TodoFilters>(() =>
    filtersForView(initialView),
  );

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
  }, [fetcher, filters, reloadKey, reloadVersion, view]);

  function refresh(): void {
    setLoading(true);
    setReloadKey((key) => key + 1);
  }

  function applyFilters(event: React.FormEvent): void {
    event.preventDefault();
    setLoading(true);
    setView("");
    setFilters({
      keyword: keyword === "" ? undefined : keyword,
      status: status === "" ? undefined : status,
      noDue: noDue || undefined,
    });
  }

  return (
    <section aria-label="待办列表" className="todo-list">
      {view ? (
        <p className="todo-view-label">
          当前视图：
          {{
            pending: "待处理",
            today: "今日到期",
            overdue: "已逾期",
            noDue: "无到期时间",
            completed7d: "近 7 天完成",
          }[view] ?? "全部"}
        </p>
      ) : null}
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
      {error ? (
        <button className="btn-ghost" onClick={refresh} type="button">
          重试加载
        </button>
      ) : null}
      {editing ? (
        <TodoForm
          editing={editing}
          fetcher={fetcher}
          key={`${editing.id}:${editing.version}`}
          onCancel={() => setEditing(null)}
          onDone={() => {
            setEditing(null);
            refresh();
          }}
        />
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
                {todo.description ? (
                  <span className="todo-description">{todo.description}</span>
                ) : null}
                <span className="todo-due">
                  {todo.status === "completed" ? "已完成" : "待处理"}
                </span>
                {todo.reminderScheduled !== undefined &&
                todo.status === "pending" ? (
                  <span className="todo-due">
                    {todo.reminderScheduled
                      ? `提醒已安排${todo.reminderChannels?.length ? ` · ${todo.reminderChannels.map((channel) => (channel === "email" ? "邮箱" : "短信")).join("、")}` : ""}`
                      : "提醒未安排，请验证并启用联系方式"}
                  </span>
                ) : null}
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
              <button
                className="btn-ghost"
                onClick={() => setEditing(todo)}
                type="button"
              >
                编辑
              </button>
              <TodoActions fetcher={fetcher} onChanged={refresh} todo={todo} />
            </li>
          ))}
        </ul>
      )}
    </section>
  );
}
