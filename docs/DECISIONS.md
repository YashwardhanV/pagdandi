# Decisions

One line per decision: date — decision — reason.

- 2026-10-12 — Dev runtime is native Windows (no Docker/WSL2) on the build machine; Docker Compose kept for prod and for hosts with virtualization. — CPU virtualization is disabled in firmware (`HCS_E_HYPERV_NOT_INSTALLED`), so Docker Desktop and WSL2 cannot start.
- 2026-10-12 — Portable PostgreSQL 18.4 + PostGIS 3.6.2 bundle, Redis 8.10 (msys2 build), osmium-tool via micromamba/conda-forge, go-pmtiles binary, GraphHopper 11.1 fat jar on local Java 25; all installed under `data/build/tools` (gitignored). — Same versions as the container images, no admin rights or Windows services needed.
- 2026-10-12 — GraphHopper pinned to 11.1 (latest stable on 2026-10-12). — §2 "latest stable".
- 2026-10-12 — Hand-written parameterised pgx queries in `internal/store` instead of sqlc. — sqlc's parser needs cgo/Docker on Windows and maps PostGIS geometry columns poorly; queries stay parameterised (the safety goal of §8.4).
- 2026-10-12 — `api/openapi.yaml` is hand-written and is the contract; TS types are generated with `openapi-typescript` and consumed by `openapi-fetch`. Go handlers use hand-written structs checked against the spec by a contract test (kin-openapi). — oapi-codegen server stubs add little over ServeMux patterns; the contract test keeps Go and the spec in sync.
- 2026-10-12 — Coverage polygon union and cluster detection run in PostGIS (`ST_Union`, `ST_Intersects`) driven from `datactl coverage`. — Robust polygon union; PostGIS is already required.
- 2026-10-12 — GraphHopper 11.1 has no `lit` encoded value: night profile skips the lighting preference (penalises tracks/dirt only). — §6.1 fallback.
- 2026-10-12 — Gates are detected from an `osm_barriers` table (loaded by `datactl nodes`) near the route geometry, not from GraphHopper. — GH has no barrier encoded value; GH itself already blocks gates tagged access=private/no.
- 2026-10-12 — Shortcut endpoint snapping uses an `osm_nodes` table of routable-way nodes loaded by `datactl nodes` (KNN, ≤30 m). — §7.1 needs real OSM node IDs so baked ways connect to the graph.
- 2026-10-12 — Military areas are written to `custom_models/generated/<country>/military.json` and listed in every profile's `custom_model_files` (GH merges the files). — Keeps the base models identical to §6.2.
- 2026-10-12 — India overrides also copy `motorcycle=no` into `motor_vehicle=no`; Sweden overrides add `motor_vehicle=no` to `highway=track` ways that lack explicit motor access. — `car_access` (used for both two-wheeler and moped profiles) honours `motor_vehicle` but not `motorcycle`; this encodes §4.3 as data, and each graph has no car profile it could harm.
- 2026-10-12 — Cache/rate-limit layer has a Redis implementation and an in-memory implementation (used only when `REDIS_URL` is empty, e.g. unit tests). — Tests and degraded dev keep working without Redis.
- 2026-10-12 — IDs for requests use ULIDs (`oklog/ulid`). — §8.3 example shows ULID-style ids.
- 2026-10-12 — App-local copy of the conda-forge MSVC runtime (14.51) is placed next to postgres.exe. — The system msvcp140.dll (14.36) is older than the one PostgreSQL 18 was built against and crashes initdb with 0xC0000005.
- 2026-10-12 — Community-leg costs use parallel GraphHopper /route calls (O→A, B→D) instead of /matrix. — The Matrix API is not part of open-source GraphHopper.
- 2026-10-12 — Clusters merge regions when one centre lies inside the other's circle (centre distance ≤ radius), not on any circle intersection. — Plain intersection chains 150 km circles from Indore to Delhi and Rishikesh; this rule reproduces the clusters §4.1 names and keeps REGION=indore = Indore+Mhow+Ujjain. REGION=all still unions all coverage per country.
- 2026-10-12 — Tokens adjusted for WCAG AA (§11.7 requires axe to pass): light --ink-3 #8A9099→#6B7079 (2.9:1→4.8:1 on --bg), dark --ink-3 #7D838A→#8F959C, dark --accent-ink white→#17191C (white on #FF7A3D is 2.6:1), brighter dark ok/warn/danger. Added --casing, --scrim, --shadow-float. — The spec values fail AA for small text.
- 2026-10-12 — Two-wheeler and moped speed models cap CITY roads at 30 km/h and RESIDENTIAL-density roads at 40 km/h using GraphHopper's urban_density (computed at import). — GH's car speeds put the founding 8.25 km city route at 11.5 min; §1.2 and Google put it at 16–18 min. Without this, no shortcut could ever win in a city.
- 2026-10-12 — Community-leg speed for scooters/motorcycles: 25 km/h paved, 20 unpaved, 15 track (was 20/15/12). — SPEC §1.2's founding example is 3.4 km of kaccha in 9–11 min (~20 km/h).
- 2026-10-12 — India map labels use English names; local names appear in UI lists. — MapLibre cannot shape Devanagari, so local-script labels would render broken.
- 2026-10-12 — Fonts are self-hosted via @fontsource, not Google Fonts. — Works offline in the PWA and keeps the CSP same-origin.
- 2026-10-12 — Screen primary actions live in a footer pinned to the bottom of the screen, outside the bottom sheet. — At the 52 % snap the CTA was below the fold; §11.1 wants it in the thumb zone.
- 2026-10-12 — Shortcut options that are both longer and slower than the usual way are not shown; duplicates are detected by geometry, and the duplicate carrying rider confirmations wins. — They would never be worth taking; profiles differ only in speed models, so durations can't identify duplicates.
- 2026-10-12 — Gate warnings require the barrier to be within 3 m of the route. — Gates on side roads at junctions triggered false warnings at 8 m.
- 2026-10-12 — E2E tests mock the API and block service workers. — Deterministic, offline-safe CI (§14 "synthetic fixture only"); a registered SW would bypass the mocks.
