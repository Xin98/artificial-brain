create schema if not exists conversation;

create table conversation.confirmation_requests (
  id uuid primary key,
  workspace_id uuid not null,
  user_id uuid not null,
  intent text not null,
  todo_id uuid not null,
  todo_version integer not null,
  created_at timestamptz not null default now(),
  expires_at timestamptz not null,
  consumed_at timestamptz
);

create index confirmation_requests_workspace_user_idx
  on conversation.confirmation_requests (workspace_id, user_id);

create table conversation.messages (
  id bigserial primary key,
  workspace_id uuid not null,
  user_id uuid not null,
  role text not null,
  body text not null,
  resolved_intent text,
  created_at timestamptz not null default now()
);

create index messages_workspace_user_created_at_idx
  on conversation.messages (workspace_id, user_id, created_at);

---- create above / drop below ----

drop table if exists conversation.messages;
drop table if exists conversation.confirmation_requests;
drop schema if exists conversation;
