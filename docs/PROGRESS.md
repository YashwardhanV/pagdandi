# Progress

Source of truth: [SPEC.md](SPEC.md). Decisions: [DECISIONS.md](DECISIONS.md).

## Milestones

| # | Milestone | Status |
|---|---|---|
| M0 | Repo, tooling, compose skeleton | Mostly done: Makefile, scripts, native runtime, compose, Caddy, CI written. `make dev` is untested end to end. |
| M1 | Data pipeline, single region | **Done** for Indore cluster (Indore+Mhow+Ujjain): coverage, extract, overrides, military areas, 2.39M nodes, graph (38 s import), PMTiles (75 MB + packs), seed. |
| M2 | Go API core | **Done** except the web-side generated client being wired into screens. OpenAPI written, contract test passes. |
| M3 | Web app core and design system | **Started**: deps installed, Vite/TS/ESLint config, tokens, base CSS, `lib/` (api, geo, storage, time), `en.json`. **The app does not build yet:** no `main.tsx`, components or screens. |
| M4 | Community layer | Backend done and tested (create/snap/report/confidence/lifecycle/moderation). Web UI not started. |
| M5 | Navigation | Backend data ready (instructions, surface spans). Web UI not started. |
| M6 | All 18 regions | Not started (`make data REGION=all` needs ~16 GB RAM; this machine has 8 GB). Sweden graph not built. |
| M7 | Polish, PWA, perf, a11y | Not started. |
| M8 | Desire paths and moderation | Backend done with unit tests (mining, trust, admin API). Web UI not started. |
| M9 | Deploy and docs | Deploy script skips without credentials. OPERATIONS.md and README not written. |

## What works now

- `go test ./...` (unit) passes.
- `./scripts/test-gh.sh` builds the synthetic-town graph and runs the integration suite. All pass: profiles, gate warning, API flows, auth, privacy trimming, deletion, OpenAPI contract.
- Indore GraphHopper serves on :8989 (`scripts/gh-serve.sh in`). Postgres and Redis start with `scripts/services-up.sh`.

## Next steps (in order)

1. Web: `src/main.tsx`, app shell/router, UI components, map (PMTiles at `/tiles/in.pmtiles`), search, route compare (M3). Also `hi.json`/`sv.json`, `scripts/check-tokens.mjs`, `scripts/check-size.mjs`, `playwright.config.ts`, `src/test/setup.ts`, `public/` icons.
2. Then M4–M5 web screens, M7 PWA/offline packs, OPERATIONS.md and README.

## Open issues

- **Docker/WSL2 unavailable on the build machine**: CPU virtualization is disabled in firmware
  (`HCS_E_HYPERV_NOT_INSTALLED`). Everything runs natively (see DECISIONS.md). Enable Intel VT-x / AMD-V in BIOS to use Docker.
- 5 of 28 Indore-cluster destinations are unverified (fail the ±40 % check): Indore/Dewas Tekri, Mhow/Mandu, Ujjain/Sandipani Ashram, Ujjain/Kal Bhairav Temple, Ujjain/Dewas Tekri. See `data/build/geocode-report.md`.
- `tw_adventure` cannot use grade 4/5 tracks: GraphHopper 11's car access parser drops them.
- The founding-example kaccha track (Choithram → Bilawali) is not in OSM. `make seed-dev` adds clearly-labelled **demo** shortcuts there.
- `GEOCODER_CONTACT_EMAIL` is unset. Set your own contact in `.env` for public geocoders.
