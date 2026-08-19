create table reminder.reminder_deliveries (
  id uuid primary key,
  workspace_id uuid not null,
  owner_user_id uuid not null,
  todo_id uuid not null,
  todo_reminder_version integer not null,
  plan_id uuid not null references reminder.reminder_plans (id),
  channel text not null,
  todo_title_snapshot text not null,
  idempotency_key text not null,
  state text not null default 'scheduled',
  suppression_reason text,
  attempt_count integer not null default 0,
  provider_job_id bigint,
  provider_message_id text,
  last_error_code text,
  scheduled_at timestamptz not null,
  created_at timestamptz not null default now(),
  submitted_at timestamptz,
  finalized_at timestamptz,
  receipt_state text,
  receipt_at timestamptz,
  receipt_error_code text,
  constraint reminder_deliveries_channel_check check (channel in ('email', 'sms')),
  constraint reminder_deliveries_state_check check (state in ('scheduled', 'sending', 'succeeded', 'failed', 'suppressed')),
  constraint reminder_deliveries_suppression_check check (suppression_reason in ('todo_completed', 'todo_deleted', 'version_stale', 'channel_unavailable', 'plan_revoked')),
  constraint reminder_deliveries_receipt_check check (receipt_state in ('received_ok', 'received_failed')),
  constraint reminder_deliveries_idempotency_unique unique (idempotency_key),
  constraint reminder_deliveries_todo_channel_unique unique (todo_id, todo_reminder_version, channel)
);

create index reminder_deliveries_workspace_state_idx on reminder.reminder_deliveries (workspace_id, state);
create index reminder_deliveries_provider_message_idx on reminder.reminder_deliveries (provider_message_id);
create index reminder_deliveries_plan_idx on reminder.reminder_deliveries (plan_id);

create table reminder.fake_outbox (
  id bigserial primary key,
  address text not null,
  channel text not null,
  todo_id uuid not null,
  body text not null,
  created_at timestamptz not null default now()
);

create index fake_outbox_address_created_at_idx on reminder.fake_outbox (address, created_at desc);

---- create above / drop below ----

drop index if exists reminder.fake_outbox_address_created_at_idx;
drop table if exists reminder.fake_outbox;
drop index if exists reminder.reminder_deliveries_plan_idx;
drop index if exists reminder.reminder_deliveries_provider_message_idx;
drop index if exists reminder.reminder_deliveries_workspace_state_idx;
drop table if exists reminder.reminder_deliveries;
