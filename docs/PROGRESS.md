# Progress

Source of truth: [SPEC.md](SPEC.md). Decisions: [DECISIONS.md](DECISIONS.md). How to run: [OPERATIONS.md](OPERATIONS.md).

Last updated: 2026-10-12.

## Milestones

| # | Milestone | Status | Notes |
|---|---|---|---|
| M0 | Repo, tooling, compose skeleton | **Done** (native runtime) | Makefile, scripts, compose, Caddy, CI. Docker path written but unexercised: this machine cannot run Docker (see Open issues). |
| M1 | Data pipeline, single region | **Done** | Indore cluster: coverage, extract, overrides, military areas, 2.39 M nodes, graph, PMTiles + offline packs, seed. |
| M2 | Go API core | **Done** | All §8.2 endpoints, OpenAPI contract test, generated TS client, fixture profile tests. |
| M3 | Web app core and design system | **Done** | Tokens-only Tailwind, map style, search, route compare, mobile + desktop layouts; E2E search → compare passes; §11.7 items for these screens pass. |
| M4 | Community layer | **Done** | Auth, draw/record, snapping, reports, confidence, lifecycle worker, overlay routing with community legs; tests at every layer. |
| M5 | Navigation | **Done** | Turn-by-turn, off-route reroute, voice en/hi/sv, wake lock, surface-ahead warnings, night profile, seasons, post-ride prompt. Not yet field-tested on a phone with real GPS. |
| M6 | All 18 regions | **Partial** | Built and serving: Indore cluster (gh-in :8989) and Stockholm (gh-se :8990: bike + moped, Off-Road Driving Act rule, 100 military areas, 328 MB tiles, 7/8 destinations verified). hi/sv i18n done with an overflow test. `REGION=all` (all-India graph) needs ~16 GB RAM; this machine has 8 GB. |
| M7 | Polish, PWA, performance, accessibility | **Mostly done** | PWA + offline packs, axe clean on every screen (Lighthouse a11y 100), best practices 96–100, initial JS 162 KB gzip, hi/sv overflow test, Home/Work places. **Lighthouse performance fails** (see Open issues). |
| M8 | Desire paths and moderation | **Done** | Consent flow, privacy trimming, nightly mining (H3, k ≥ 3), candidate prompt, admin queue, trust updates. |
| M9 | Deploy and docs | **Done except the deploy** | Prod compose profile, Caddy, nightly backups, weekly refresh script, OPERATIONS.md, README with screenshots. No cloud credentials, so `make deploy` skips (as §0.2 requires). |

## What works now (verified)

- **Go**: `go test ./...` passes (unit: geo, regions, suncalc, confidence/lifecycle/trust, privacy
  trimming, router orchestration with a fake GraphHopper, mining, cache).
- **Integration** (`make test-gh`): builds the synthetic-town graph through the real pipeline and
  checks: standard avoids the track, shortcut takes it, every profile avoids private/motorway/military,
  gate warning, full API flows (magic-link auth, create/snap/report/activate shortcut, moderation,
  traces with Home cut-out, account deletion, rate limits), OpenAPI contract.
- **Web**: Vitest 23 tests; Playwright 16 tests on Pixel 7 and iPhone 14 (search → compare → ride →
  arrive → post-ride prompt; draw shortcut; offline banner; axe on every screen; light/dark snapshots;
  no text overflow in Hindi or Swedish).
- **Override rules**: `infra/tools/test_osm_overrides.py` checks the India expressway/`motorcycle=no`
  and Sweden track rules on a fixture (part of `make test-gh`).
- **Real data**: Indore → Bilawali routes in ~15 ms per GraphHopper call; the usual way is
  8.1 km / 14 min, consistent with §1.2's 16–18 min. Stockholm centre → Drottningholm: bicycle
  11.6 km / 43 min on cycleways, moped 12.1 km / 24 min on roads only, Swedish instructions.
  `/readyz` is green for Postgres, Redis, gh-in and gh-se.

## How to run

See README "Quick start" and [OPERATIONS.md](OPERATIONS.md). On this machine everything runs natively:
`./scripts/services-up.sh`, `./scripts/gh-serve.sh in`, `make dev`.

## Open issues

1. **Docker/WSL2 unavailable on the build machine.** CPU virtualization is disabled in firmware
   (`HCS_E_HYPERV_NOT_INSTALLED`). Everything runs natively instead. Enable Intel VT-x / AMD-V in
   BIOS to use the Docker path; `infra/docker-compose.yml` has not been exercised end to end.
2. **Lighthouse performance below the ≥ 90 budget.** Mobile scores are 33–36 with the map, 84
   without it. The WebGL map costs ~450 ms of real main-thread time (shader compilation and painter
   setup) on software rendering, which Lighthouse multiplies 4×; a run with hardware WebGL on this
   machine scored no better (31). Accessibility is 100 and best practices 96–100. CI reports
   Lighthouse without blocking until this is fixed. Options, none taken yet because each is a product trade-off:
   (a) a static first frame (pre-rendered tile image) swapped for the live map on first interaction;
   (b) a lighter style at low zoom; (c) measuring on a GPU-backed runner.
   Pixel-ratio capping and no label fade are already applied.
3. **`REGION=all` not built**: the all-India GraphHopper import needs ~12 GB heap (§5). Build on
   the 32 GB production VM.
4. **Unverified destinations (6)** fail the ±40 % rule and are kept without coordinates; see
   `data/build/geocode-report.md`. They are Indore/Dewas Tekri, Mhow/Mandu, Ujjain/Dewas Tekri,
   Ujjain/Sandipani Ashram, Ujjain/Kal Bhairav Temple and Stockholm/Vaxholm. Several are road-vs-
   straight-line mismatches (Mandu, Vaxholm); some are wrong geocoder hits that the checks correctly
   rejected (Kal Bhairav at 904 km).
5. **`tw_adventure` cannot use grade 4/5 tracks**: GraphHopper 11's car access parser drops them.
6. **The founding-example kaccha track is not in OSM.** `make seed-dev` adds demo shortcuts
   (titled "Demo: …") that follow real ground; real riders' shortcuts are needed to show the
   founding route's savings.
7. **`GEOCODER_CONTACT_EMAIL` is unset.** Set your own contact in `.env` before heavier use of the
   public Photon/Nominatim services.
8. Snapshot baselines exist for Windows only; Linux CI records its own on first run.
