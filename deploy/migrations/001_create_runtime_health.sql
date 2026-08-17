create schema if not exists runtime;

create table runtime.worker_heartbeats (
  instance_id text primary key,
  service_version text not null,
  started_at timestamptz not null,
  last_heartbeat_at timestamptz not null
);

---- create above / drop below ----

drop table if exists runtime.worker_heartbeats;
drop schema if exists runtime;
