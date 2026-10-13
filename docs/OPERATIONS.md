# Operations

## Run locally

```sh
make bootstrap   # Java 17+, Go 1.24+, Node 20+ required; installs GraphHopper 11.1, osmium, pmtiles into data/build/tools
make data        # coverage → download → extract → areas → features → graph → tiles
make dev         # GraphHopper :8989, API :8080, web :5173 (logs in data/build/logs)
make stop
```

Pieces on their own: `make gh`, `make api`, `make web`. Phone testing with GPS: `make tunnel`.

## Data build

`cd services/api && go run ./cmd/datactl <step> [--force]`. Steps skip work whose outputs are newer than their inputs.

| Step | Output | Time (8 GB laptop) |
|---|---|---|
| download | `data/build/osm/src/central-zone-latest.osm.pbf` (~250 MB, reused 7 days) | 1–4 min |
| extract | `data/build/osm/indore.osm.pbf` | 10 s |
| areas | `data/build/gh/models/military.json` | 1 s |
| features | `data/build/features/*` | 6 s |
| graph | `data/build/gh/graph-*` + `config.yml`; elevation cache in `data/build/elevation` | ~1 min |
| tiles | `data/build/tiles/indore.pmtiles` (~45 MB) | ~40 s |

- `GH_JAVA_XMX` (default 3g) sets the import heap. `--elevation=false` (or `GH_ELEVATION=false`) skips elevation if
  the Skadi tile host is unreachable.
- The graph step serves the new graph on a spare port, routes 4 km with every profile, and only then switches
  `data/build/gh/current` — a failed import never replaces a working graph. The previous graph is kept.
- Change the region or favourites in `data/region.yaml`, then `make data`.
- Refresh weekly: `cd services/api && go run ./cmd/datactl all --force`, then restart GraphHopper and the API.

## Production

One VM (2 vCPU, 4 GB RAM is enough for Indore), Docker Compose:

```sh
git clone … /opt/pagdandi && cd /opt/pagdandi && cp .env.example .env   # set APP_ENV=prod, GEOCODER_CONTACT_EMAIL
make bootstrap && make data
DEPLOY_HOST=… DEPLOY_USER=… DEPLOY_DOMAIN=pagdandi.example make deploy   # from your machine
```

Caddy terminates TLS, serves `apps/web/dist`, and proxies `/v1`, `/tiles`, `/healthz`, `/readyz` to the API.
The API serves PMTiles with HTTP range requests. Self-host Photon (`PHOTON_URL`) for real traffic.

## Health

- `GET /healthz` — process up. `GET /readyz` — GraphHopper reachable (503 otherwise).
- Logs: one line per `/v1` request with status and milliseconds. Go plans take 60–550 ms on the laptop.

## Tests

```sh
make test     # Go (fake GraphHopper; needs no data) + Vitest
make lint     # go vet, gofmt, tsc
```
