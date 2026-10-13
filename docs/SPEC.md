# Pagdandi v2: specification

Status: implemented (see PROGRESS.md). v1 (a community-shortcut app with accounts, Postgres and moderation) was
rejected by the owner and removed. This document is the source of truth for v2.

## 1. Product

**One line:** Google-Maps-like routing that also shows the lesser-taken, shorter, newer, wilder and prettier
ways, plus a "surprise me within N km" mode.

**Principles**
1. *The usual is always shown.* Every alternative is measured against it (Δ time, Δ km).
2. *Every trail earns its place.* A trail is only shown if it is genuinely different (≤ 85 % cell overlap with
   any kept trail) and genuinely what it claims (a Kaccha trail has ≥ 500 m more unpaved or ≥ 1.5 km more back
   roads than the usual; Quiet Lanes is ≥ 8 points quieter; Scenic scores ≥ 4 points higher).
3. *Honest labels, accepted risk.* No route is hidden for being rough; it is labelled (surface ribbon, warnings).
   Hard limits only where the law or safety is unambiguous: no motorways on two-wheelers, no private/no-access
   roads, no military areas, no ferries.
4. *Beautiful and calm.* Map first; one bottom sheet; few, large controls.

**Region:** Indore, 125 km radius (`data/region.yaml`). Wander reaches 100 km; 25 km spare for the road home.

## 2. Modes

### 2.1 Go (A → B)
Input: from (GPS / searched / dropped pin), to, vehicle, detour budget `stray_km` (default auto = 35 % of the usual,
2–25 km; slider 0–40). Output: trails in order — The Usual, Shortcut, Scenic, Kaccha, Quiet Lanes, New Ground —
after budget filter, honesty filter and dedupe (a duplicate adds its kind to `also`).

| Kind | How it's made |
|---|---|
| usual | base profile + model preferring main roads (tracks ×0.08, unpaved ×0.35), distance_influence 40 |
| shortest | distance_influence 600 |
| kaccha | main roads ×0.3, secondary ×0.45, tertiary ×0.65, paved ×0.7 |
| quiet | main ×0.25, secondary ×0.4, tertiary ×0.7, city ×0.7 |
| scenic | A → anchor → B through up to 5 diverse scenic anchors in the budget ellipse; best scenic score minus detour penalty |
| fresh | A → new way → B through up to 4 newly mapped ways in the ellipse; most fresh metres |

### 2.2 Wander
Input: origin, vehicle, radius 1–100 km (walk 30), flavour (any, water, hills, forest, heritage, wild, fresh), tab
(places | loops), seed.
- **Places:** gems within the radius, filtered by flavour, weighted `score × (0.75 + 0.25·d/r) × 1.15 if featured`,
  sampled with Efraimidis–Spirakis weighted random sampling when a seed is set (🎲 Surprise me). Fresh flavour
  turns long new ways into destinations. Jetski lists water bodies ≥ 0.3 km².
- **Loops:** pairs of anchors/gems in the 0.3r–0.95r annulus, 30–110° apart, whose triangle perimeter ≈ 2.2r/1.3,
  routed start → a → b → start (pass-through), up to 3 diverse loops; topped up with GraphHopper round trips.
  Jetski: a circuit round the lake if it fits the radius, else out along the shore and back across.

### 2.3 Ride
Turn-by-turn from GraphHopper instructions; projection onto the trail; next manoeuvre; "ahead" line (surface
change within 1.2 km, highlight within 2.5 km); voice (Web Speech, en-IN) at 350 m and at the turn; off-route
after 3 fixes > max(45 m, 1.5 × accuracy) → reroute same kind; wake lock; arrival card. **Preview** simulates
the ride in ~60 s.

## 3. Vehicles

| id | GraphHopper profile | per-request tweaks |
|---|---|---|
| bike | moto | — (grade 4–5 tracks allowed at 12 km/h) |
| scooty | moto | 50 km/h cap, grade 4–5 ×0.05, grade 3 ×0.5, sand ×0.3, soft ground 15 km/h |
| car | car | grade 3 ×0.5 (grade 4–5 blocked in base) |
| cycle | cycle | trunk ×0.35, primary ×0.6 (except Shortcut) |
| walk | walk | Kaccha prefers paths and tracks |
| jetski | — (water grid) | open water / shoreline, 35 km/h cruise |

Base profiles (`infra/graphhopper/models/pd_*.json`) are permissive; request models only multiply priority by
≤ 1 and only cap speeds, so landmarks (LM) stay valid for every character (tested in `plan_test.go`).

## 4. Scoring a trail (`internal/plan/analyze.go`)

From GraphHopper path details (road_class, surface, track_type, osm_way_id, urban_density) and 3D geometry:
- **Surface:** paved | rough | dirt per segment (surface tag, else track grade, else paved). Ribbon + mix.
- **Quiet** = 1 − Σ busyness(road_class)·len / L, × (1 − 0.35 × built-up share).
- **Scenic:** a 50 m sample walk over the land grid — near water 1.0, protected 0.9, forest 0.85, green 0.5,
  fields 0.3, else 0.12; ×0.35 in town (where only lakes count as water, not drains) — plus hills (climb/km) and
  named viewpoints/waterfalls/heritage within 300 m.
- **Highlights:** runs ≥ 400 m of water/forest/wild named from nearby anchors ("Along Sirpur Lake"), "Past X",
  "Climbs N m", "Twisty".
- **Twisty:** bend per km on open roads (turns ≤ 45°, not town streets).
- **Back roads:** km of track/path/unclassified outside towns.
- **Fresh:** metres on ways first added to OSM (version 1) in the last 18 months.
- **Warnings:** unpaved ≥ 0.8 km; monsoon (Jun–Sep) with dirt; sunset before arrival; grade ≥ 9 %; rough tracks.

## 5. Data (`datactl`)

coverage → download (Geofabrik central-zone) → extract (osmium, complete ways) → areas (military → GH areas)
→ features → graph (GraphHopper 11.1 import + SRTM/Skadi elevation, verified with sample routes, blue/green
pointer) → tiles (Protomaps daily build, z0–15).

`features` writes `data/build/features/`: `land.grid.gz` (byte flags per 50 m cell: Water, NearWater ≤ 200 m,
Forest, BuiltUp, Green, Protected, River, Farmland), `gems.json`, `anchors.json` (shore/edge samples and
point gems), `waters.json` (named, merged water bodies), `fresh.json`, `meta.json`.

## 6. API (`services/api`)

| Endpoint | |
|---|---|
| `GET /v1/meta` | region, vehicles, flavours, data dates, tiles URL |
| `GET /v1/search?q&lat&lng` | local gems first, then Photon (≤ 1 req/s public, cached 24 h) |
| `GET /v1/reverse?lat&lng` | name a dropped pin |
| `POST /v1/go` | `{from,to,vehicle,stray_km?,depart?}` → `{trails[], stray_km, notes}` |
| `POST /v1/wander` | `{from,vehicle,radius_km,flavour,kind,seed}` → `{places[] | loops[]}` |
| `GET /v1/gems?bbox&limit` · `GET /v1/fresh?bbox` · `GET /v1/waters` | map layers |
| `GET /tiles/{file}.pmtiles` | basemap with HTTP range |
| `/healthz`, `/readyz` | liveness, GraphHopper reachability |

Errors: `{error:{code,message}}` with codes `bad_request`, `outside`, `no_route`, `different_water`, `no_water`,
`unavailable`. Responses are gzipped; plans cached in memory 15 min.

## 7. Design system (`apps/web`)

- **Moods:** Dusk (ink #0B0F15, parchment text, marigold accent #FFB547) and Day (paper #F4EFE4, ink text,
  burnt-orange accent). Auto follows sunset (NOAA), 20 min margin.
- **Type:** Fraunces (soft, wonky italic for names and numbers) + Manrope (UI) + Tiro Devanagari (wordmark, Hindi names).
- **Trail colours:** usual moonlight silver, shortcut cyan, kaccha terracotta, quiet violet, scenic jade, new
  ground marigold, loop rose, open water blue, shoreline aqua.
- **Map:** custom Protomaps flavours, hillshade under roads, glowing trail strands (blur glow + casing + core),
  animated flow on the selected trail (disabled with reduced motion), gem dots by kind, Wander radius lens.
- **Layout:** phone: draggable bottom sheet (peek/half/full) with the Go/Wander switch; desktop: 420 px glass panel.
- **Components:** vehicle dial, segmented pill, range slider, trail card (strand, serif name, Hindi name,
  tagline, big time, Δ, surface ribbon, elevation sparkline, quiet/scenic meters, badges), trail detail
  (along-the-way timeline, heads-up notes, surface mix, elevation chart, turns), place card, ride HUD.

## 8. Non-goals (for now)
Accounts, saved trips in the cloud, community edits (contribute to OpenStreetMap instead), live traffic, regions
beyond Indore, offline routing.
