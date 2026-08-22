create schema if not exists portability;

create table public.instance_meta (
  key text primary key,
  value text not null,
  created_at timestamptz not null default now()
);

create table portability.portability_imports (
  id uuid primary key,
  workspace_id uuid not null,
  state text not null default 'pending',
  source_instance_id text not null,
  bundle bytea not null,
  preview jsonb,
  report jsonb,
  created_at timestamptz not null default now(),
  committed_at timestamptz,
  constraint portability_imports_state_check check (state in ('pending', 'committed', 'expired'))
);

create table portability.portability_source_records (
  workspace_id uuid not null,
  source_instance_id text not null,
  source_record_id text not null,
  target_kind text not null,
  target_id text not null,
  content_fingerprint text not null,
  created_at timestamptz not null default now(),
  constraint portability_source_records_target_check check (target_kind in ('todo', 'channel', 'delivery')),
  constraint portability_source_records_identity_unique unique (source_instance_id, source_record_id)
);

alter table reminder.reminder_deliveries alter column plan_id drop not null;
alter table reminder.reminder_deliveries add column origin text not null default 'local';
alter table reminder.reminder_deliveries add constraint reminder_deliveries_origin_check check (origin in ('local', 'imported'));

-- Imported deliveries are history, not plans: the bundle wire shape carries
-- no reminder version, so every delivery of a rescheduled todo restores as
-- (todo, 0, channel) and would collide on migration 007's global constraint.
-- Scope the fallback uniqueness to locally-planned rows only; imported rows
-- stay unique through their import idempotency key.
alter table reminder.reminder_deliveries drop constraint if exists reminder_deliveries_todo_channel_unique;
create unique index reminder_deliveries_todo_channel_local_unique
  on reminder.reminder_deliveries (todo_id, todo_reminder_version, channel)
  where origin = 'local';

---- create above / drop below ----

drop index if exists reminder.reminder_deliveries_todo_channel_local_unique;
alter table reminder.reminder_deliveries add constraint reminder_deliveries_todo_channel_unique unique (todo_id, todo_reminder_version, channel);
alter table reminder.reminder_deliveries drop constraint if exists reminder_deliveries_origin_check;
alter table reminder.reminder_deliveries drop column if exists origin;
alter table reminder.reminder_deliveries alter column plan_id set not null;
drop table if exists portability.portability_source_records;
alter table portability.portability_imports drop column if exists preview;
drop table if exists portability.portability_imports;
drop table if exists public.instance_meta;
drop schema if exists portability;
