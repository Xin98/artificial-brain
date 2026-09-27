create table conversation.sessions (
  id uuid primary key,
  workspace_id uuid not null,
  user_id uuid not null,
  title text not null,
  created_at timestamptz not null default now(),
  updated_at timestamptz not null default now()
);

create index sessions_workspace_user_updated_at_idx
  on conversation.sessions (workspace_id, user_id, updated_at desc);

-- Nullable so pre-session audit rows (migrations 005-009) stay valid;
-- session-scoped history only ever reads rows with a session_id.
alter table conversation.messages
  add column session_id uuid null
    references conversation.sessions (id) on delete cascade;

create index messages_session_id_idx
  on conversation.messages (session_id, id);

---- create above / drop below ----

drop index if exists conversation.messages_session_id_idx;
alter table conversation.messages drop column if exists session_id;
drop index if exists conversation.sessions_workspace_user_updated_at_idx;
drop table if exists conversation.sessions;
