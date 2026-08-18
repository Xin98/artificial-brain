create schema if not exists identity;

create table identity.workspaces (
  id uuid primary key,
  created_at timestamptz not null default now()
);

create table identity.users (
  id uuid primary key,
  workspace_id uuid not null references identity.workspaces (id),
  phone text not null,
  created_at timestamptz not null default now(),
  constraint users_phone_unique unique (phone)
);

create table identity.login_challenges (
  id uuid primary key,
  phone text not null,
  code_hash text not null,
  created_at timestamptz not null,
  expires_at timestamptz not null,
  consumed_at timestamptz,
  attempts integer not null default 0
);

create index login_challenges_phone_created_at_idx
  on identity.login_challenges (phone, created_at desc);

create table identity.sessions (
  id uuid primary key,
  user_id uuid not null references identity.users (id),
  workspace_id uuid not null,
  token_hash text not null,
  created_at timestamptz not null,
  expires_at timestamptz not null,
  revoked_at timestamptz,
  constraint sessions_token_hash_unique unique (token_hash)
);

create table identity.contact_channels (
  id uuid primary key,
  user_id uuid not null references identity.users (id),
  workspace_id uuid not null,
  kind text not null,
  address text not null,
  verified boolean not null default false,
  enabled boolean not null default true,
  code_hash text,
  code_expires_at timestamptz,
  created_at timestamptz not null default now(),
  constraint contact_channels_kind_check check (kind in ('email', 'sms')),
  constraint contact_channels_user_kind_address_unique unique (user_id, kind, address)
);

create table identity.message_outbox (
  id bigserial primary key,
  address text not null,
  channel text not null,
  purpose text not null,
  code text not null,
  created_at timestamptz not null default now()
);

create index message_outbox_address_created_at_idx
  on identity.message_outbox (address, created_at desc);

---- create above / drop below ----

drop table if exists identity.message_outbox;
drop table if exists identity.contact_channels;
drop table if exists identity.sessions;
drop table if exists identity.login_challenges;
drop table if exists identity.users;
drop table if exists identity.workspaces;
drop schema if exists identity;
