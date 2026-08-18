create schema if not exists todo;

create table todo.todos (
  id uuid primary key,
  workspace_id uuid not null,
  owner_user_id uuid not null,
  title text not null,
  description text,
  due_at_utc timestamptz,
  timezone_at_input text,
  status text not null default 'pending',
  reminder_version integer not null default 1,
  version integer not null default 1,
  created_at timestamptz not null default now(),
  updated_at timestamptz not null default now(),
  completed_at timestamptz,
  deleted_at timestamptz,
  constraint todos_title_length_check check (char_length(title) between 1 and 200),
  constraint todos_status_check check (status in ('pending', 'completed', 'deleted'))
);

create index todos_workspace_owner_status_idx
  on todo.todos (workspace_id, owner_user_id, status);

create index todos_workspace_owner_due_idx
  on todo.todos (workspace_id, owner_user_id, due_at_utc);

---- create above / drop below ----

drop table if exists todo.todos;
drop schema if exists todo;
