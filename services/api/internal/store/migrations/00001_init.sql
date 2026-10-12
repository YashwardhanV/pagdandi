-- +goose Up
create extension if not exists postgis;
create extension if not exists pg_trgm;
create extension if not exists citext;

create table regions (
  id text primary key,
  name text not null,
  country char(2) not null,
  timezone text not null,
  center geography(Point,4326) not null,
  radius_km int not null,
  coverage geometry(MultiPolygon,4326) not null,
  config jsonb not null default '{}'
);
create index on regions using gist (coverage);

create table users (
  id uuid primary key default gen_random_uuid(),
  email citext unique,
  display_name text not null,
  home_region text references regions(id),
  trust real not null default 1.0 check (trust between 0.5 and 2.0),
  role text not null default 'rider' check (role in ('rider','moderator','admin')),
  traces_consent_at timestamptz,
  created_at timestamptz not null default now()
);

create table sessions (
  id uuid primary key default gen_random_uuid(),
  user_id uuid not null references users(id) on delete cascade,
  token_hash bytea not null unique,
  expires_at timestamptz not null
);

create table destinations (
  id uuid primary key default gen_random_uuid(),
  region_id text not null references regions(id),
  name text not null,
  name_local text,
  category text not null,
  geom geography(Point,4326),
  approx_km int,
  verified boolean not null default false
);
create index on destinations using gin (name gin_trgm_ops);

create table shortcuts (
  id uuid primary key default gen_random_uuid(),
  region_id text not null references regions(id),
  title text,
  geom geometry(LineString,4326) not null,
  length_m int not null,
  start_osm_node bigint not null,
  end_osm_node bigint not null,
  oneway boolean not null default false,
  surface text not null check (surface in ('paved','unpaved','track')),
  vehicles text[] not null default '{scooter,motorcycle}',
  flags text[] not null default '{}',
  status text not null default 'pending' check (status in ('pending','active','stale','blocked','removed')),
  alpha real not null default 1, beta real not null default 1,
  confidence real not null default 0.5,
  last_confirmed_at timestamptz,
  created_by uuid references users(id) on delete set null,
  created_at timestamptz not null default now(),
  updated_at timestamptz not null default now()
);
create index on shortcuts using gist (geom);
create index on shortcuts (region_id, status);

create table shortcut_reports (
  id uuid primary key default gen_random_uuid(),
  shortcut_id uuid not null references shortcuts(id) on delete cascade,
  user_id uuid not null references users(id) on delete cascade,
  verdict text not null check (verdict in ('passable','blocked','partly')),
  reason text check (reason in ('gate_locked','flooded','construction','private','unsafe','missing','other')),
  note text,
  photo_key text,
  created_at timestamptz not null default now(),
  report_day date generated always as ((created_at at time zone 'UTC')::date) stored,
  unique (shortcut_id, user_id, report_day)
);

create table traces (
  id uuid primary key default gen_random_uuid(),
  user_id uuid not null references users(id) on delete cascade,
  region_id text references regions(id),
  geom geometry(LineString,4326) not null,
  timestamps timestamptz[] not null,
  purpose text not null check (purpose in ('shortcut_submission','desire_mining')),
  created_at timestamptz not null default now(),
  expires_at timestamptz not null default now() + interval '90 days'
);
create index on traces using gist (geom);

create table candidate_paths (
  id uuid primary key default gen_random_uuid(),
  region_id text not null references regions(id),
  geom geometry(LineString,4326) not null,
  distinct_users int not null,
  first_seen timestamptz not null,
  last_seen timestamptz not null,
  yes_count int not null default 0,
  no_count int not null default 0,
  status text not null default 'new' check (status in ('new','prompted','promoted','rejected'))
);
create index on candidate_paths using gist (geom);

create table route_stats (            -- analytics, no precise coordinates stored
  id bigserial primary key,
  region_id text not null,
  h3_origin_r7 text, h3_dest_r7 text,
  chosen text not null,
  saved_m int, saved_s int,
  created_at timestamptz not null default now()
);

-- Additions beyond SPEC §9 (see docs/DECISIONS.md).

-- Routable-way nodes for shortcut endpoint snapping (§7.1), loaded by `datactl nodes`.
create table osm_nodes (
  id bigint primary key,
  country char(2) not null,
  geom geometry(Point,4326) not null
);
create index on osm_nodes using gist (geom);

-- Barrier nodes on routable ways, for gate warnings (§4.3).
create table osm_barriers (
  id bigint primary key,
  country char(2) not null,
  kind text not null,
  access text,
  geom geometry(Point,4326) not null
);
create index on osm_barriers using gist (geom);

-- Magic-link sign-in tokens (§8.4).
create table magic_links (
  id uuid primary key default gen_random_uuid(),
  email citext not null,
  token_hash bytea not null unique,
  expires_at timestamptz not null,
  used_at timestamptz,
  created_at timestamptz not null default now()
);

-- One answer per user per desire-path candidate (§7.7).
create table candidate_answers (
  candidate_id uuid not null references candidate_paths(id) on delete cascade,
  user_id uuid not null references users(id) on delete cascade,
  answer boolean not null,
  created_at timestamptz not null default now(),
  primary key (candidate_id, user_id)
);

-- Saved Home/Work; traces are cut 300 m around them (§17).
create table user_places (
  user_id uuid not null references users(id) on delete cascade,
  kind text not null check (kind in ('home','work')),
  geom geography(Point,4326) not null,
  primary key (user_id, kind)
);

-- Off-network trace segments awaiting mining (§7.7); kept no longer than the trace.
create table offnet_segments (
  id bigserial primary key,
  trace_id uuid not null references traces(id) on delete cascade,
  user_id uuid not null references users(id) on delete cascade,
  region_id text,
  cells text[] not null,
  geom geometry(LineString,4326) not null,
  created_at timestamptz not null default now()
);
create index on offnet_segments (created_at);

-- Moderation log.
create table moderation_actions (
  id bigserial primary key,
  shortcut_id uuid references shortcuts(id) on delete set null,
  moderator_id uuid references users(id) on delete set null,
  action text not null,
  note text,
  created_at timestamptz not null default now()
);

-- +goose Down
drop table if exists moderation_actions, offnet_segments, user_places, candidate_answers, magic_links,
  osm_barriers, osm_nodes, route_stats, candidate_paths, traces, shortcut_reports, shortcuts,
  destinations, sessions, users, regions cascade;
