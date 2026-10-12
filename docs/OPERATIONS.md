# Operations

How to run, build data for, test, deploy and look after Pagdandi. The product and
architecture live in [SPEC.md](SPEC.md); choices made along the way are in
[DECISIONS.md](DECISIONS.md); status is in [PROGRESS.md](PROGRESS.md).

## 1. Two ways to run

| | Docker (preferred) | Native |
|---|---|---|
| When | Linux, macOS, Windows with WSL2 and CPU virtualization | Windows without virtualization (the original build machine) |
| Postgres + PostGIS | `postgis/postgis:17-3.5` container | portable PostgreSQL 18.4 + PostGIS 3.6.2 in `data/build/tools/pgsql` |
| Redis | `redis:7.4-alpine` | redis-windows 8.10 in `data/build/tools/redis` |
| GraphHopper | `infra/graphhopper/Dockerfile` (11.1) | `data/build/bin/graphhopper-web-11.1.jar` on local Java 17+ |
| osmium / pyosmium | apt / brew | conda-forge env in `data/build/tools/osmenv` |

`scripts/*.sh` pick Docker when `docker info` works, otherwise native. Force one with
`PAGDANDI_RUNTIME=native` or `PAGDANDI_RUNTIME=docker`. Run `make` from a POSIX shell (Linux,
macOS, WSL2, or Git Bash on Windows).

To enable Docker on a Windows machine that reports `HCS_E_HYPERV_NOT_INSTALLED`: enter the BIOS/UEFI
setup, enable Intel VT-x (or AMD-V/SVM), boot, then run `wsl --install --no-distribution` and restart
Docker Desktop.

## 2. First run

```sh
make bootstrap              # tools, .env (with a fresh SESSION_SECRET), Postgres+Redis, migrations, npm install
make data REGION=indore     # Indore + Mhow + Ujjain: ~10 min first time on 8 GB RAM
make seed-dev               # demo shortcuts near Bilawali (titled "Demo: …")
make dev                    # web :5173, api :8080, gh-in :8989 (gh-se :8990 when built)
```

Sign in during development: request a magic link in the app; the link is printed in
`data/build/logs/api.log` (no email is sent while `SMTP_HOST` is empty).

Make yourself a moderator: `make admin EMAIL=you@example.com` (default role `admin`; `ROLE=moderator`).

Phone testing with GPS: `make tunnel` and open the printed `https://…trycloudflare.com` URL.

## 3. Data builds

`make data REGION=<id>` runs `datactl all --region <id>`:

1. `coverage` – 64-sided 150 km circles plus corridors; clusters (one centre inside the other's
   circle); writes `data/build/coverage_{in,se}.geojson`, `clusters.json`, `selection.json`.
2. `download` – the Geofabrik India zones that intersect coverage (or Sweden); 3 retries with
   backoff, then `download.openstreetmap.fr`. Files are reused for 7 days.
3. `extract` – `osmium extract -s complete_ways` to coverage, merged across zones.
4. `overrides` – `infra/tools/osm_overrides.py`: India expressway bans from
   `data/overrides/in_2w_restrictions.yaml`, `motorcycle=no` → `motor_vehicle=no`; Sweden tracks
   without explicit motor access → `motor_vehicle=no` (mopeds only).
5. `areas` – military polygons → `data/build/gh/<c>/models/military.json` (blocked in every profile).
6. `nodes` – routable-way nodes and barriers → Postgres (`osm_nodes`, `osm_barriers`) for snapping
   and gate warnings.
7. `overlay` – trusted shortcuts (confidence ≥ 0.75, ≥ 5 confirmations) baked in as OSM ways with
   negative ids. Nothing is uploaded to OpenStreetMap.
8. `graph` – GraphHopper import into a new `graph-<timestamp>` directory, then the
   `current` pointer and `config.yml` are switched. The previous graph is kept for rollback.
9. `tiles` – Protomaps daily build → `data/build/tiles/<c>.pmtiles` (z0–15 in coverage) and 25 km
   offline packs per region.
10. `seed` – regions, coverage and destinations. Geocoding goes through Photon then Nominatim at
    ≤ 1 req/s; a result must be within ±40 % of `approx_km` (2 km floor) of the centre, in the
    right country and inside coverage. Failures stay unverified, without coordinates, and are
    listed in `data/build/geocode-report.md`. Results are cached in `data/destinations.resolved.json`.

Each step is idempotent and skips work whose outputs are newer than its inputs; add `--force` to
redo. Run steps individually: `cd services/api && go run ./cmd/datactl <step> --country in`.

Resource needs:

| Build | RAM | Disk | Time |
|---|---|---|---|
| `REGION=indore` | 8 GB machine, `GH_JAVA_XMX=2g` | ~2 GB | ~10 min |
| `REGION=stockholm` | 8 GB machine, `GH_JAVA_XMX=3g` | ~3 GB | ~20 min |
| `REGION=all` | 16 GB+, `GH_JAVA_XMX=12g` | ~60 GB | hours |

Adding a city: append it to `data/regions.yaml`, then `make data REGION=<id>`.

## 4. Weekly graph refresh (blue/green)

`make refresh-graph` (cron: Sunday 02:00 local) re-downloads, rebuilds and imports into a new
directory, then serves it on a spare port and routes centre → 3 km for every region (`--verify`).
Only if that passes is `current` switched. In production run two GraphHopper containers per country
and list both in `GH_IN_URL` (`http://gh-in-a:8989,http://gh-in-b:8989`): the API fails over between
them, so restarting one onto the new graph drops no requests. Roll back by pointing
`data/build/gh/<c>/current` at the previous `graph-*` directory and restarting.

## 5. Tests

```sh
make test       # Go unit + integration (integration needs TEST_DATABASE_URL), Vitest
make test-gh    # builds the synthetic-town graph through the real pipeline and runs integration tests
make e2e        # Playwright: Pixel 7 + iPhone 14, mocked API and GPS, axe on every screen, light/dark snapshots
make lint       # go vet, staticcheck, eslint, tsc, design-token check
cd apps/web && npm run check:size   # initial JS ≤ 200 KB gzip
cd apps/web && npx lhci autorun     # Lighthouse (set CHROME_PATH if Chrome is not installed)
```

Snapshot baselines are stored per platform (`*-win32.png`, `*-linux.png`); a missing baseline is
recorded on first run rather than failing.

## 6. Production (SPEC §16)

One VM in an India region (4 vCPU, 32 GB RAM, 100 GB SSD), Docker Compose `prod` profile: Caddy
(automatic TLS), api, worker, gh-in, gh-se, Postgres, Redis, nightly backup.

```sh
# on the server, once
git clone … /opt/pagdandi && cd /opt/pagdandi
cp .env.example .env    # set APP_ENV=prod, SESSION_SECRET (48+ chars), SMTP_*, GEOCODER_CONTACT_EMAIL,
                        # PHOTON_URL (self-hosted), GOOGLE_OAUTH_* (optional), POSTGRES_PASSWORD
make bootstrap && make data REGION=all
# from your machine
DEPLOY_HOST=… DEPLOY_USER=… DEPLOY_DOMAIN=pagdandi.example make deploy
```

`make deploy` builds the web app, rsyncs the repo (never `.env` or `data/build`) and runs
`docker compose --profile prod up -d --build`. It exits cleanly, doing nothing, when the
`DEPLOY_*` variables are missing.

The web app can instead go on Vercel with `VITE_API_BASE=https://api.<domain>`; the API and
GraphHopper must stay on the VM (GraphHopper is a long-running JVM).

### Backups

The `backup` service writes `pg_dump -Fc` at 02:00 IST to the `backups` volume, keeps 14 days, and
copies to `BACKUP_S3_URL` when set. Restore: `pg_restore -h localhost -U pagdandi -d pagdandi --clean <file>`.

### Monitoring

- `GET /healthz` – process up. `GET /readyz` – Postgres, Redis, every GraphHopper (503 if any is down).
  Check it every minute.
- `GET /metrics` (Prometheus): `pagdandi_route_duration_seconds{profiles,cached}`,
  `pagdandi_graphhopper_duration_seconds`, `pagdandi_route_cache_total{result}`,
  `pagdandi_shortcuts{status}`, `pagdandi_http_*`. Alert when
  `histogram_quantile(0.95, rate(pagdandi_route_duration_seconds_bucket{cached="false"}[10m])) > 1.5`.
- Logs are JSON (`slog`) with `request_id` on every line; the same id is returned in `X-Request-ID`.

## 7. Moderation

Moderators see `/admin` (link in Profile): new shortcuts and private/unsafe reports. Approve,
remove or dismiss; every action is logged in `moderation_actions`. Removing a shortcut hides it
everywhere immediately.

## 8. Privacy operations (DPDP Act 2023, GDPR)

- Account deletion is self-service (Profile → Delete) and removes the user, sessions, reports,
  traces and saved places in one transaction; shortcuts they added stay, without the creator link.
- Traces: opt-in only, trimmed 200 m at both ends and 300 m around Home/Work before storage,
  deleted after 90 days by the worker's retention job. Desire paths need ≥ 3 distinct users.
- Analytics (`route_stats`) store only H3 resolution-7 cells, never coordinates.

## 9. Troubleshooting

| Symptom | Fix |
|---|---|
| `initdb` crashes with `0xC0000005` on Windows | copy the MSVC runtime DLLs from `data/build/tools/osmenv` into `pgsql/bin` (done by `install-tools.sh`) |
| Docker Desktop never starts, log says `HCS_E_HYPERV_NOT_INSTALLED` | enable virtualization in firmware (section 1) or use the native runtime |
| GraphHopper import runs out of memory | raise `GH_JAVA_XMX`, or build one region at a time |
| Routes fail with `ROUTING_UNAVAILABLE` | `curl localhost:8989/health`; start it with `make gh-in` |
| Map is blank in dev | no local tiles yet: run `datactl tiles`, or set `VITE_TILES_MODE=remote` |
| A destination is missing on the map | it failed the geocoding checks; see `data/build/geocode-report.md` and fix its name or `approx_km` |
