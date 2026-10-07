create schema investment;

create function investment.reject_version_update() returns trigger language plpgsql as $$
begin raise exception 'immutable investment version'; end;
$$;

create table investment.universe_versions (
 id uuid primary key, universe_id uuid not null, workspace_id uuid not null, owner_user_id uuid not null,
 name text not null check(char_length(name) between 1 and 100), mode text not null check(mode in ('fixture','alpaca_sec')),
 effective_at timestamptz not null, created_at timestamptz not null, historical_membership_known boolean not null default false,
 unique(workspace_id,owner_user_id,id)
);
create table investment.universe_members (
 workspace_id uuid not null, owner_user_id uuid not null, version_id uuid not null,
 instrument_id text not null, primary key(version_id,instrument_id),
 foreign key(workspace_id,owner_user_id,version_id) references investment.universe_versions(workspace_id,owner_user_id,id) on delete cascade
);
create trigger immutable_universe before update on investment.universe_versions for each row execute function investment.reject_version_update();
create trigger immutable_members before update on investment.universe_members for each row execute function investment.reject_version_update();
create table investment.strategy_versions (
 id uuid primary key,strategy_id text not null,workspace_id uuid not null,owner_user_id uuid not null,
 parameters jsonb not null,created_at timestamptz not null,unique(workspace_id,owner_user_id,id)
);
create trigger immutable_strategy before update on investment.strategy_versions for each row execute function investment.reject_version_update();
create table investment.accounts (
 id uuid primary key,workspace_id uuid not null,owner_user_id uuid not null,
 name text not null check(char_length(name) between 1 and 100),mode text not null check(mode in ('fixture','alpaca_sec')),
 strategy_version_id uuid,universe_version_id uuid,
 initial_cash bigint not null check(initial_cash>0 and initial_cash<=100000000000),
 available_cash bigint not null check(available_cash>=0),reserved_cash bigint not null default 0 check(reserved_cash>=0),
 unsettled_cash bigint not null default 0 check(unsettled_cash>=0),dividend_receivable bigint not null default 0 check(dividend_receivable>=0),
 version integer not null check(version>0),automation_enabled boolean not null default false,pause_reason text not null default '',
 risk_policy jsonb not null,pending_config jsonb,created_at timestamptz not null,
 unique(workspace_id,owner_user_id,id),
 foreign key(workspace_id,owner_user_id,strategy_version_id) references investment.strategy_versions(workspace_id,owner_user_id,id),
 foreign key(workspace_id,owner_user_id,universe_version_id) references investment.universe_versions(workspace_id,owner_user_id,id)
);
create index accounts_owner on investment.accounts(workspace_id,owner_user_id,created_at,id);
create table investment.datasets (
 version text primary key,mode text not null check(mode in ('fixture','alpaca_sec')),feed text not null,source text not null,created_at timestamptz not null,coverage jsonb not null
);
create trigger immutable_dataset before update on investment.datasets for each row execute function investment.reject_version_update();
create table investment.data_snapshots (
 id text not null,dataset_version text not null references investment.datasets(version),as_of timestamptz not null,
 projection jsonb not null,primary key(dataset_version,id)
);
create index snapshots_cutoff on investment.data_snapshots(dataset_version,as_of desc,id);
create trigger immutable_snapshot before update on investment.data_snapshots for each row execute function investment.reject_version_update();
create table investment.instruments (
 id text primary key,ticker text not null,name text not null,cik text not null default '',exchange text not null,sic text not null default '',kind text not null,tradable boolean not null
);
create table investment.instrument_facts (
 dataset_version text not null references investment.datasets(version),instrument_id text not null,source text not null,source_record_id text not null,
 effective_at timestamptz not null,available_at timestamptz not null,ingested_at timestamptz not null,projection jsonb not null,
 primary key(dataset_version,source,source_record_id)
);
create table investment.price_bars (
 dataset_version text not null references investment.datasets(version),instrument_id text not null,session_date date not null,
 available_at timestamptz not null,ingested_at timestamptz not null,source text not null,source_record_id text not null,
 open_price bigint not null check(open_price>0),high_price bigint not null check(high_price>0),low_price bigint not null check(low_price>0),close_price bigint not null check(close_price>0),volume bigint not null check(volume>=0),
 primary key(dataset_version,instrument_id,session_date,source_record_id,ingested_at)
);
create table investment.financial_facts (
 dataset_version text not null references investment.datasets(version),instrument_id text not null,accession text not null,concept text not null,
 value text not null,unit text not null,currency text not null,period_start timestamptz,period_end timestamptz not null,
 available_at timestamptz not null,ingested_at timestamptz not null,source text not null,source_record_id text not null,
 primary key(dataset_version,instrument_id,accession,source_record_id)
);
create table investment.news_items (
 dataset_version text not null references investment.datasets(version),id text not null,title text not null,summary text not null,url text not null,
 instrument_ids text[] not null,published_at timestamptz not null,available_at timestamptz not null,ingested_at timestamptz not null,source text not null,source_record_id text not null,
 primary key(dataset_version,id)
);
create table investment.corporate_actions (
 dataset_version text not null references investment.datasets(version),id text not null,instrument_id text not null,kind text not null,currency text not null,
 effective_at timestamptz not null,available_at timestamptz not null,pay_at timestamptz,ingested_at timestamptz not null,source text not null,source_record_id text not null,projection jsonb not null,
 primary key(dataset_version,id)
);
create table investment.data_sync_runs (
 id uuid primary key,workspace_id uuid not null,owner_user_id uuid not null,dataset_version text not null,mode text not null,
 state text not null check(state in ('queued','running','completed','failed')),request jsonb not null,result jsonb,error_code text not null default '',created_at timestamptz not null,completed_at timestamptz,
 unique(workspace_id,owner_user_id,id)
);
create table investment.evaluation_runs (
 id uuid primary key,workspace_id uuid not null,owner_user_id uuid not null,account_id uuid not null,session_date date not null,
 strategy_version_id uuid not null,universe_version_id uuid not null,dataset_version text not null,snapshot_id text not null,mode text not null,
 state text not null,issued_orders boolean not null default false,as_of timestamptz not null,projection jsonb not null,
 unique(workspace_id,owner_user_id,id),
 foreign key(workspace_id,owner_user_id,account_id) references investment.accounts(workspace_id,owner_user_id,id) on delete cascade,
 foreign key(dataset_version,snapshot_id) references investment.data_snapshots(dataset_version,id),
 foreign key(workspace_id,owner_user_id,strategy_version_id) references investment.strategy_versions(workspace_id,owner_user_id,id),
 foreign key(workspace_id,owner_user_id,universe_version_id) references investment.universe_versions(workspace_id,owner_user_id,id),
 unique(account_id,session_date,strategy_version_id,mode)
);
create unique index one_order_batch_per_session on investment.evaluation_runs(account_id,session_date) where issued_orders;
create table investment.signals (
 workspace_id uuid not null,owner_user_id uuid not null,run_id uuid not null,instrument_id text not null,score numeric(4,1) not null check(score between 0 and 100),rank integer not null,evidence jsonb not null,
 primary key(run_id,instrument_id),foreign key(workspace_id,owner_user_id,run_id) references investment.evaluation_runs(workspace_id,owner_user_id,id) on delete cascade
);
create table investment.recommendations (
 workspace_id uuid not null,owner_user_id uuid not null,run_id uuid not null,instrument_id text not null,projection jsonb not null,
 primary key(run_id,instrument_id),foreign key(workspace_id,owner_user_id,run_id) references investment.evaluation_runs(workspace_id,owner_user_id,id) on delete cascade
);
create table investment.backtest_runs (
 id uuid primary key,workspace_id uuid not null,owner_user_id uuid not null,state text not null check(state in ('queued','running','completed','failed')),
 dataset_version text not null,strategy_version_id uuid not null,universe_version_id uuid not null,request jsonb not null,result jsonb,error_code text not null default '',created_at timestamptz not null,
 unique(workspace_id,owner_user_id,id),
 foreign key(workspace_id,owner_user_id,strategy_version_id) references investment.strategy_versions(workspace_id,owner_user_id,id),
 foreign key(workspace_id,owner_user_id,universe_version_id) references investment.universe_versions(workspace_id,owner_user_id,id)
);
create table investment.orders (
 id uuid primary key,workspace_id uuid not null,owner_user_id uuid not null,account_id uuid not null,instrument_id text not null,
 side text not null check(side in ('buy','sell')),state text not null check(state in ('pending','awaiting_bar','filled','partially_filled_cancelled','expired','rejected','cancelled')),
 quantity bigint not null check(quantity>0 and quantity<=1000000000),reserved_quantity bigint not null check(reserved_quantity>=0),reserved_cash bigint not null check(reserved_cash>=0),capacity bigint not null check(capacity>=0),
 reason text not null default '',origin text not null,target_open_at timestamptz not null,expires_at timestamptz not null,created_at timestamptz not null,version integer not null check(version>0),
 unique(workspace_id,owner_user_id,account_id,id),foreign key(workspace_id,owner_user_id,account_id) references investment.accounts(workspace_id,owner_user_id,id) on delete cascade
);
create index orders_due on investment.orders(state,target_open_at,account_id,instrument_id);
create table investment.fills (
 id uuid primary key,workspace_id uuid not null,owner_user_id uuid not null,account_id uuid not null,order_id uuid not null unique,instrument_id text not null,
 quantity bigint not null check(quantity>0),price bigint not null check(price>0),gross bigint not null check(gross>=0),fee bigint not null check(fee>0),effective_at timestamptz not null,recorded_at timestamptz not null,settles_at timestamptz,
 foreign key(workspace_id,owner_user_id,account_id,order_id) references investment.orders(workspace_id,owner_user_id,account_id,id) on delete cascade
);
create table investment.positions (
 workspace_id uuid not null,owner_user_id uuid not null,account_id uuid not null,instrument_id text not null,industry text not null,
 quantity bigint not null check(quantity>=0),reserved_quantity bigint not null check(reserved_quantity>=0 and reserved_quantity<=quantity),cost bigint not null check(cost>=0),
 primary key(account_id,instrument_id),foreign key(workspace_id,owner_user_id,account_id) references investment.accounts(workspace_id,owner_user_id,id) on delete cascade
);
create table investment.ledger_entries (
 id uuid primary key,workspace_id uuid not null,owner_user_id uuid not null,account_id uuid not null,event_key text not null,kind text not null,instrument_id text not null default '',
 available_delta bigint not null,reserved_delta bigint not null,unsettled_delta bigint not null,dividend_delta bigint not null,quantity_delta bigint not null,
 effective_at timestamptz not null,recorded_at timestamptz not null,
 unique(account_id,event_key),foreign key(workspace_id,owner_user_id,account_id) references investment.accounts(workspace_id,owner_user_id,id) on delete cascade
);
create table investment.nav_snapshots (
 workspace_id uuid not null,owner_user_id uuid not null,account_id uuid not null,session_date date not null,nav bigint not null check(nav>=0),gross_turnover bigint not null default 0 check(gross_turnover>=0),available_at timestamptz not null,quality_flags jsonb not null,
 primary key(account_id,session_date),foreign key(workspace_id,owner_user_id,account_id) references investment.accounts(workspace_id,owner_user_id,id) on delete cascade
);
create table investment.automation_events (
 id uuid primary key,workspace_id uuid not null,owner_user_id uuid not null,account_id uuid not null,event_key text not null,kind text not null,reason text not null,projection jsonb not null,effective_at timestamptz not null,recorded_at timestamptz not null,
 unique(account_id,event_key),foreign key(workspace_id,owner_user_id,account_id) references investment.accounts(workspace_id,owner_user_id,id) on delete cascade
);
create table investment.mutation_requests (
 workspace_id uuid not null,owner_user_id uuid not null,route text not null,key text not null check(char_length(key) between 1 and 200),request_hash text not null,response jsonb,created_at timestamptz not null default now(),
 primary key(workspace_id,owner_user_id,route,key)
);

---- create above / drop below ----

drop schema investment cascade;
