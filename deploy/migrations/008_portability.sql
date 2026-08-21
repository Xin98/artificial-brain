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

---- create above / drop below ----

alter table reminder.reminder_deliveries drop constraint if exists reminder_deliveries_origin_check;
alter table reminder.reminder_deliveries drop column if exists origin;
alter table reminder.reminder_deliveries alter column plan_id set not null;
drop table if exists portability.portability_source_records;
drop table if exists portability.portability_imports;
drop table if exists public.instance_meta;
drop schema if exists portability;
