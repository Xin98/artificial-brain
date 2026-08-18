create schema if not exists reminder;

create table reminder.reminder_plans (
  id uuid primary key,
  workspace_id uuid not null,
  todo_id uuid not null,
  todo_reminder_version integer not null,
  scheduled_at_utc timestamptz not null,
  requested_channels text[] not null default '{}',
  status text not null default 'planned',
  created_at timestamptz not null default now(),
  revoked_at timestamptz,
  constraint reminder_plans_status_check check (status in ('planned', 'revoked')),
  constraint reminder_plans_todo_version_unique unique (todo_id, todo_reminder_version)
);

create index reminder_plans_workspace_todo_idx
  on reminder.reminder_plans (workspace_id, todo_id);

---- create above / drop below ----

drop table if exists reminder.reminder_plans;
drop schema if exists reminder;
