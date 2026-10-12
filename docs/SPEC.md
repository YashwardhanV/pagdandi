# Pagdandi: Hidden-Shortcut Maps for Two-Wheelers

> **Pagdandi** (पगडंडी) means "a narrow path worn by people walking it." This app finds those paths.
> This file is the complete product and architecture spec. It is the **single source of truth** for building the app.
> Owner: Yashwardhan Verma. Builder: Claude Code.

---

## 0. Instructions for Claude Code (read this first)

### 0.1 Autonomy: you have full permission
- **You have all the permissions you need to build this project end to end. Do not ask the owner for permission, confirmation, approval or clarification at any point. Operate fully on your own.**
- When something is ambiguous, follow this spec. If the spec is silent, choose the simplest robust option, record it as one line in `docs/DECISIONS.md` (date, decision, reason) and keep going.
- Never stop and wait for input. If a step fails (a download, a build, a test, a port conflict), diagnose it, retry, use the fallback listed in this spec, and continue. Log anything unresolved in `docs/PROGRESS.md` under "Open issues" and move on to the next task.
- Install whatever tools and dependencies you need (Go, Node, Docker images, osmium-tool, pmtiles CLI, Playwright browsers, etc.) without asking.
- Run the tests after every milestone and fix failures before moving on.
- Use git. Initialise the repo if needed and commit after each milestone with Conventional Commit messages. Do not push to a remote unless one is already configured.
- Keep `docs/PROGRESS.md` current: milestone status, what works, how to run it, open issues.

### 0.2 Operating boundaries (these keep autonomy safe; they are not reasons to ask questions)
- Create, modify and delete files only inside the project directory. Installing tools through package managers and Docker is fine.
- Never commit secrets. Generate dev secrets into `.env` (gitignored) and keep `.env.example` up to date.
- A production deploy needs the owner's cloud credentials. If they are not present in the environment, build the deploy scripts and docs, skip the actual deploy, note it in `PROGRESS.md`, and carry on. Do not ask for them.
- If the host is Windows, run everything inside WSL2 (Ubuntu). The data tools (osmium, GraphHopper, Docker volumes) are far more reliable there.

### 0.3 First actions
1. Create the repo layout in §12.
2. Copy this file to `docs/SPEC.md`.
3. Create `CLAUDE.md` at the repo root containing: "Source of truth: docs/SPEC.md. Follow §0 (full autonomy, never ask the owner anything). Track progress in docs/PROGRESS.md."
4. Execute the milestones in §15 in order.

---

## 1. Product

### 1.1 One-liner
A maps app for two-wheelers (and bicycles in Stockholm) that shows **the usual route side by side with a shorter "pagdandi" route** through lesser-known roads, tracks and shortcuts. Each shortcut carries an honest label: its surface, how recently riders confirmed it, and how confident the app is that it's passable today.

### 1.2 The founding example (Indore)
- From the Lal Bagh / Choithram Mandi area to Bilawali Lake View, Google Maps suggests about **7.1 km and 16–18 min** on a two-wheeler, going around via Khandwa Naka.
- A lesser-known kaccha (unpaved) track toward the Choudhary farm house side gets near the same spot in **about 9–11 min**.
- Google usually *knows* such tracks exist. It leaves them out because it can't confirm they're passable: they might be private, gated, a dead end, or mud in the monsoon.
- **Pagdandi's edge is freshness and trust.** Riders confirm that a path is passable *now*, and the app shows that evidence openly.

### 1.3 Users
- Daily two-wheeler commuters in Indian cities who know local gullies and want more of them.
- Weekend riders heading to nearby tourist spots (waterfalls, forts, lakes, hill stations).
- Cyclists and moped riders in Stockholm.

### 1.4 Principles
1. **Honest:** always show surface type, confidence and the date a shortcut was last confirmed. Never hide that a route is unpaved.
2. **Safe:** never route through `access=private`/`access=no`, military areas or roads where two-wheelers are banned. Be more cautious at night and in the monsoon or winter.
3. **Local:** follow regional rules, seasons and languages (English, Hindi, Swedish).
4. **Fast and calm:** get from opening the app to seeing a route in two taps. The UI stays quiet and readable, with nothing flashy.

### 1.5 v1 scope and non-goals
- **In scope:** two-wheeler routing (scooter/motorcycle) in India; bicycle and moped routing in Stockholm; community shortcuts; route comparison; basic turn-by-turn navigation; PWA with offline region packs.
- **Out of scope for v1:** cars, live traffic, public transport, native app-store builds (PWA first; a Capacitor wrap comes later).
- **Never use Google Maps data or tiles.** Google's Terms of Service forbid copying or deriving data from Google Maps. The only Google touchpoint allowed is an "Open in Google Maps" deep link.

---

## 2. Tech stack (decided)

| Layer | Choice | Why |
|---|---|---|
| Routing engine | **GraphHopper** (latest stable, Docker) | Mature open-source engine with **custom models**: per-profile rules on road class, surface, track grade and access. This is the core of "shortcut" routing. It has map matching built in and supports CH and LM speed-ups. Writing this from scratch in Go would take months. |
| API / orchestration | **Go** (latest stable, ≥1.23), standard `net/http` with ServeMux routing patterns | Fast, simple concurrency for calling the engine in parallel, single static binary, low memory. |
| DB | **PostgreSQL + PostGIS** (`postgis/postgis` image), `pg_trgm`, `citext` | Spatial queries for shortcuts, regions and candidate paths. |
| Cache / rate limit | **Redis** | Route cache and token-bucket rate limiting. |
| Go libraries | `pgx/v5`, `sqlc`, `goose` (migrations), `log/slog`, `oapi-codegen`, `go-redis`, `uber/h3-go`, `paulmach/orb` (geometry), `testcontainers-go` | Boring, proven choices. |
| Frontend | **React 19 + TypeScript (strict) + Vite** | As requested; fast builds. |
| Map rendering | **MapLibre GL JS** (via `react-map-gl/maplibre`) | Open-source vector maps with no API key. |
| Basemap tiles | **Protomaps PMTiles** extracts, self-hosted | One static file per country, served with HTTP range requests. Cheap and works offline. Dev fallback: OpenFreeMap hosted styles (no key). |
| Geocoding | Local destination index (Postgres trigram) first, then **Photon** (self-hosted per-country extract; dev fallback: public Photon at ≤1 req/s through the Go proxy, with cache) | Autocomplete-friendly and OSM-based. |
| UI | Tailwind CSS v4 (custom tokens only), Radix UI primitives, `vaul` (bottom sheet), Lucide icons, TanStack Query, Zustand, React Router, `openapi-fetch` client generated from OpenAPI | Accessible primitives with a custom look. |
| PWA | `vite-plugin-pwa` (Workbox) | Offline packs, installable. |
| Testing | Go `testing` + testcontainers; Vitest + Testing Library; Playwright (mobile viewports); axe-core; Lighthouse CI | Quality gates in §14. |
| Infra | Docker Compose (dev and prod profiles), Caddy (TLS, static, reverse proxy), Makefile | One-command dev. |

---

## 3. System architecture

```
                    ┌──────────────────────────────────────────────┐
  Phone / Browser   │  React PWA (MapLibre)                        │
                    │  - search, route compare, navigation         │
                    │  - draw / record shortcut, verify            │
                    │  - offline packs (PMTiles + cached routes)   │
                    └───────────────┬──────────────────────────────┘
                                    │ HTTPS (JSON, OpenAPI)
                    ┌───────────────▼──────────────────────────────┐
                    │  Caddy  (TLS, static web, /tiles range, /api)│
                    └──────┬─────────────────┬─────────────────────┘
                           │                 │
             ┌─────────────▼──────┐   ┌──────▼─────────┐
             │ Go API  (cmd/api)  │   │ Tiles: PMTiles │
             │ - route orchestr.  │   │ in.pmtiles     │
             │ - shortcuts, auth  │   │ se.pmtiles     │
             │ - geocode proxy    │   └────────────────┘
             └──┬───────┬──────┬──┘
                │       │      │
   ┌────────────▼┐ ┌────▼───┐ ┌▼──────────────────────┐
   │ PostGIS     │ │ Redis  │ │ GraphHopper           │
   │ shortcuts,  │ │ cache, │ │  gh-in (India graph)  │
   │ reports,    │ │ limits │ │  gh-se (Sweden graph) │
   │ regions ... │ └────────┘ └───────────────────────┘
   └─────▲───────┘
         │
   ┌─────┴────────────────────────────────────────────┐
   │ Go worker (cmd/worker)                           │
   │ - confidence decay & status lifecycle (hourly)   │
   │ - desire-path mining from opt-in traces (nightly)│
   │ - shortcut "bake" export → OSM overlay (weekly)  │
   └──────────────────────────────────────────────────┘
   ┌──────────────────────────────────────────────────┐
   │ Go data CLI (cmd/datactl)                        │
   │ - coverage polygons, osmium extract, overrides,  │
   │   GraphHopper import, PMTiles extract, seeding   │
   └──────────────────────────────────────────────────┘
```

**Two GraphHopper instances:** `gh-in` (India coverage, two-wheeler profiles) and `gh-se` (Sweden coverage, bicycle and moped profiles). The Go API picks the instance from the request's coverage region.

---

## 4. Coverage regions

### 4.1 Coverage rules
- Each city gets a **150 km radius circle**, approximated as a 64-sided polygon around its centre.
- **Corridors:** famous destinations just outside 150 km get a corridor, a 20 km buffer each side of the straight line from the city centre to the destination. This lets riders route there end to end.
- **Clustering:** `datactl` merges overlapping circles and corridors (union-find over geometry intersection) into clusters, then unions everything per country into one coverage polygon. Overlap is common: Indore/Mhow/Ujjain, Delhi/Gurugram, Rishikesh/Mussoorie, Mumbai/Pune, Jaipur/Pushkar.
- Requests whose origin or destination falls outside coverage return `422 OUT_OF_COVERAGE`, with the nearest region and its distance.
- **Destinations are geocoded at seed time** through Photon or Nominatim (≤1 req/s, custom User-Agent `Pagdandi/0.1 (contact in .env)`). Accept a result only if it lies within ±40% of `approx_km` from the city centre and inside coverage. Otherwise mark it `verified: false` and log it in PROGRESS.md. Never invent coordinates. Cache the results in `data/destinations.resolved.json`.
- `approx_km` values are approximate road distances, used only as a geocoding sanity check.

### 4.2 `data/regions.yaml` (create exactly this, then extend)

```yaml
defaults:
  radius_km: 150
  corridor_buffer_km: 20
  timezone: Asia/Kolkata
  country: IN
  langs: [en, hi]
  seasons:
    - {name: monsoon, from: "06-01", to: "09-30", unpaved_penalty: 1.6, confidence_half_life_days: 7}

regions:
  - id: bengaluru
    name: Bengaluru
    center: [12.9716, 77.5946]   # [lat, lng]
    langs: [en, kn, hi]
    destinations:
      - {name: "Bannerghatta National Park", approx_km: 22, type: wildlife}
      - {name: "Muthyala Maduvu Falls", approx_km: 40, type: waterfall}
      - {name: "Ramanagara", approx_km: 50, type: rocks}
      - {name: "Nandi Hills", approx_km: 60, type: viewpoint}
      - {name: "Savandurga", approx_km: 60, type: trek}
      - {name: "Skandagiri", approx_km: 65, type: trek}
      - {name: "Chunchi Falls", approx_km: 90, type: waterfall}
      - {name: "Lepakshi", approx_km: 120, type: heritage}
      - {name: "Srirangapatna", approx_km: 125, type: heritage}
      - {name: "Shivanasamudra Falls", approx_km: 130, type: waterfall}
      - {name: "Mysuru Palace", approx_km: 145, type: heritage}
      - {name: "Melukote", approx_km: 150, type: heritage}

  - id: pune
    name: Pune
    center: [18.5204, 73.8567]
    langs: [en, mr, hi]
    destinations:
      - {name: "Sinhagad Fort", approx_km: 35, type: fort}
      - {name: "Mulshi Dam", approx_km: 45, type: lake}
      - {name: "Tamhini Ghat", approx_km: 50, type: ghat}
      - {name: "Pawna Lake", approx_km: 55, type: lake}
      - {name: "Rajgad Fort", approx_km: 55, type: fort}
      - {name: "Lohagad Fort", approx_km: 60, type: fort}
      - {name: "Lavasa", approx_km: 60, type: town}
      - {name: "Lonavala", approx_km: 65, type: hill}
      - {name: "Shivneri Fort", approx_km: 95, type: fort}
      - {name: "Panchgani", approx_km: 100, type: hill}
      - {name: "Bhimashankar", approx_km: 110, type: temple}
      - {name: "Mahabaleshwar", approx_km: 120, type: hill}

  - id: indore
    name: Indore
    center: [22.7196, 75.8577]
    langs: [en, hi]
    destinations:
      - {name: "Bilawali Lake", approx_km: 8, type: lake, note: "founding test case"}
      - {name: "Ralamandal Wildlife Sanctuary", approx_km: 15, type: wildlife}
      - {name: "Tincha Falls", approx_km: 25, type: waterfall}
      - {name: "Patalpani Waterfall", approx_km: 35, type: waterfall}
      - {name: "Choral Dam", approx_km: 40, type: lake}
      - {name: "Dewas Tekri (Chamunda Mata)", approx_km: 40, type: temple}
      - {name: "Janapav Kuti", approx_km: 45, type: hill}
      - {name: "Ujjain Mahakaleshwar", approx_km: 55, type: temple}
      - {name: "Omkareshwar", approx_km: 77, type: temple}
      - {name: "Maheshwar Fort", approx_km: 90, type: heritage}
      - {name: "Mandu", approx_km: 95, type: heritage}
      - {name: "Hanuwantiya", approx_km: 150, type: lake}

  - id: hyderabad
    name: Hyderabad
    center: [17.3850, 78.4867]
    langs: [en, te, hi]
    destinations:
      - {name: "Golconda Fort", approx_km: 11, type: fort}
      - {name: "Ramoji Film City", approx_km: 30, type: attraction}
      - {name: "Bhongir Fort", approx_km: 50, type: fort}
      - {name: "Kondapochamma Sagar", approx_km: 60, type: lake}
      - {name: "Yadagirigutta Temple", approx_km: 65, type: temple}
      - {name: "Ananthagiri Hills", approx_km: 80, type: hill}
      - {name: "Medak Cathedral", approx_km: 95, type: heritage}
      - {name: "Pocharam Wildlife Sanctuary", approx_km: 115, type: wildlife}
      - {name: "Warangal Fort", approx_km: 145, type: heritage}
      - {name: "Nagarjuna Sagar Dam", approx_km: 150, type: lake}

  - id: gurugram
    name: Gurugram
    center: [28.4595, 77.0266]
    langs: [en, hi]
    destinations:
      - {name: "Sultanpur National Park", approx_km: 15, type: wildlife}
      - {name: "Farrukhnagar Sheesh Mahal", approx_km: 25, type: heritage}
      - {name: "Damdama Lake", approx_km: 25, type: lake}
      - {name: "Sohna", approx_km: 30, type: town}
      - {name: "Pataudi Palace", approx_km: 30, type: heritage}
      - {name: "Surajkund", approx_km: 40, type: lake}
      - {name: "Bhindawas Bird Sanctuary", approx_km: 75, type: wildlife}
      - {name: "Neemrana Fort Palace", approx_km: 80, type: heritage}

  - id: delhi
    name: Delhi
    center: [28.6139, 77.2090]
    langs: [en, hi]
    destinations:
      - {name: "Okhla Bird Sanctuary", approx_km: 15, type: wildlife}
      - {name: "Surajkund", approx_km: 25, type: lake}
      - {name: "Sultanpur National Park", approx_km: 45, type: wildlife}
      - {name: "Damdama Lake", approx_km: 55, type: lake}
      - {name: "Hastinapur", approx_km: 110, type: heritage}
      - {name: "Garhmukteshwar", approx_km: 110, type: river}
      - {name: "Neemrana Fort Palace", approx_km: 120, type: heritage}
    corridors:
      - {to: "Vrindavan", approx_km: 170}
      - {to: "Mathura", approx_km: 180}
      - {to: "Taj Mahal, Agra", approx_km: 230}

  - id: coimbatore
    name: Coimbatore
    center: [11.0168, 76.9558]
    langs: [en, ta]
    seasons:
      - {name: sw_monsoon, from: "06-01", to: "09-30", unpaved_penalty: 1.5, confidence_half_life_days: 7}
      - {name: ne_monsoon, from: "10-15", to: "12-15", unpaved_penalty: 1.5, confidence_half_life_days: 7}
    destinations:
      - {name: "Adiyogi, Isha Yoga Center", approx_km: 30, type: temple}
      - {name: "Kovai Kutralam Falls", approx_km: 35, type: waterfall}
      - {name: "Siruvani Dam", approx_km: 40, type: lake}
      - {name: "Palakkad Fort", approx_km: 55, type: fort}
      - {name: "Coonoor", approx_km: 70, type: hill}
      - {name: "Topslip", approx_km: 80, type: wildlife}
      - {name: "Ooty", approx_km: 85, type: hill}
      - {name: "Masinagudi", approx_km: 100, type: wildlife}
      - {name: "Valparai", approx_km: 105, type: hill}
    corridors:
      - {to: "Munnar", approx_km: 160}
      - {to: "Kodaikanal", approx_km: 170}

  - id: chennai
    name: Chennai
    center: [13.0827, 80.2707]
    langs: [en, ta]
    seasons:
      - {name: ne_monsoon, from: "10-15", to: "12-31", unpaved_penalty: 1.7, confidence_half_life_days: 5}
    destinations:
      - {name: "DakshinaChitra", approx_km: 25, type: heritage}
      - {name: "Covelong Beach", approx_km: 40, type: beach}
      - {name: "Mahabalipuram Shore Temple", approx_km: 55, type: heritage}
      - {name: "Pulicat Lake", approx_km: 60, type: lake}
      - {name: "Kanchipuram", approx_km: 75, type: temple}
      - {name: "Vedanthangal Bird Sanctuary", approx_km: 85, type: wildlife}
      - {name: "Ubbalamadugu Falls", approx_km: 90, type: waterfall}
      - {name: "Tirupati", approx_km: 135, type: temple}
      - {name: "Puducherry", approx_km: 150, type: town}

  - id: mumbai
    name: Mumbai
    center: [19.0760, 72.8777]
    langs: [en, mr, hi]
    destinations:
      - {name: "Elephanta Caves", approx_km: 10, type: heritage, note: "ferry from Gateway of India; route to the jetty"}
      - {name: "Sanjay Gandhi National Park", approx_km: 35, type: wildlife}
      - {name: "Karnala Bird Sanctuary", approx_km: 60, type: wildlife}
      - {name: "Matheran", approx_km: 85, type: hill, note: "vehicles stop at Dasturi; town is vehicle-free"}
      - {name: "Lonavala", approx_km: 85, type: hill}
      - {name: "Alibaug", approx_km: 95, type: beach}
      - {name: "Igatpuri", approx_km: 120, type: hill}
      - {name: "Kashid Beach", approx_km: 130, type: beach}
      - {name: "Murud-Janjira Fort", approx_km: 150, type: fort}

  - id: rishikesh
    name: Rishikesh
    center: [30.0869, 78.2676]
    langs: [en, hi]
    seasons:
      - {name: monsoon, from: "07-01", to: "09-30", unpaved_penalty: 2.0, confidence_half_life_days: 4, landslide_risk: true}
    destinations:
      - {name: "Neer Garh Waterfall", approx_km: 5, type: waterfall}
      - {name: "Shivpuri", approx_km: 16, type: river}
      - {name: "Rajaji National Park (Chilla)", approx_km: 20, type: wildlife}
      - {name: "Kunjapuri Temple", approx_km: 25, type: temple}
      - {name: "Haridwar Har Ki Pauri", approx_km: 25, type: temple}
      - {name: "Dehradun", approx_km: 45, type: town}
      - {name: "Devprayag", approx_km: 70, type: river}
      - {name: "Kanatal", approx_km: 75, type: hill}
      - {name: "Mussoorie", approx_km: 80, type: hill}
      - {name: "Tehri Dam", approx_km: 85, type: lake}

  - id: mussoorie
    name: Mussoorie
    center: [30.4598, 78.0644]
    langs: [en, hi]
    seasons:
      - {name: monsoon, from: "07-01", to: "09-30", unpaved_penalty: 2.0, confidence_half_life_days: 4, landslide_risk: true}
      - {name: winter_ice, from: "12-15", to: "02-15", unpaved_penalty: 1.8, confidence_half_life_days: 5}
    destinations:
      - {name: "Landour", approx_km: 5, type: hill}
      - {name: "George Everest's House", approx_km: 6, type: heritage}
      - {name: "Kempty Falls", approx_km: 15, type: waterfall}
      - {name: "Dhanaulti", approx_km: 25, type: hill}
      - {name: "Robber's Cave (Guchhupani)", approx_km: 30, type: nature}
      - {name: "Surkanda Devi Temple", approx_km: 35, type: temple}
      - {name: "Dehradun", approx_km: 35, type: town}
      - {name: "Tehri Lake", approx_km: 70, type: lake}
      - {name: "Rishikesh", approx_km: 80, type: town}
      - {name: "Chakrata", approx_km: 90, type: hill}

  - id: kolkata
    name: Kolkata
    center: [22.5726, 88.3639]
    langs: [en, bn, hi]
    destinations:
      - {name: "Chandannagar", approx_km: 40, type: heritage}
      - {name: "Bandel Church", approx_km: 50, type: heritage}
      - {name: "Diamond Harbour", approx_km: 50, type: river}
      - {name: "Taki", approx_km: 75, type: river}
      - {name: "Godkhali (Sundarbans gateway)", approx_km: 100, type: wildlife}
      - {name: "Bakkhali", approx_km: 130, type: beach}
      - {name: "Mayapur", approx_km: 130, type: temple}
    corridors:
      - {to: "Shantiniketan", approx_km: 160}
      - {to: "Mandarmani", approx_km: 175}
      - {to: "Digha", approx_km: 185}

  - id: jaipur
    name: Jaipur
    center: [26.9124, 75.7873]
    langs: [en, hi]
    destinations:
      - {name: "Jal Mahal", approx_km: 8, type: heritage}
      - {name: "Amer Fort", approx_km: 11, type: fort}
      - {name: "Nahargarh Fort", approx_km: 15, type: fort}
      - {name: "Sambhar Salt Lake", approx_km: 80, type: lake}
      - {name: "Bhangarh Fort", approx_km: 85, type: fort}
      - {name: "Chand Baori, Abhaneri", approx_km: 95, type: heritage}
      - {name: "Sariska Tiger Reserve", approx_km: 110, type: wildlife}
      - {name: "Ajmer Sharif Dargah", approx_km: 135, type: heritage}
      - {name: "Pushkar", approx_km: 145, type: temple}
    corridors:
      - {to: "Ranthambore National Park", approx_km: 160}

  - id: udaipur
    name: Udaipur
    center: [24.5854, 73.7125]
    langs: [en, hi]
    destinations:
      - {name: "Eklingji Temple", approx_km: 22, type: temple}
      - {name: "Haldighati", approx_km: 40, type: heritage}
      - {name: "Nathdwara", approx_km: 50, type: temple}
      - {name: "Jaisamand Lake", approx_km: 50, type: lake}
      - {name: "Kumbhalgarh Fort", approx_km: 85, type: fort}
      - {name: "Ranakpur Jain Temple", approx_km: 95, type: temple}
      - {name: "Chittorgarh Fort", approx_km: 115, type: fort}
    corridors:
      - {to: "Mount Abu", approx_km: 165}

  - id: pushkar
    name: Pushkar
    center: [26.4897, 74.5511]
    langs: [en, hi]
    destinations:
      - {name: "Savitri Mata Temple", approx_km: 3, type: temple}
      - {name: "Ana Sagar Lake", approx_km: 14, type: lake}
      - {name: "Ajmer Sharif Dargah", approx_km: 15, type: heritage}
      - {name: "Taragarh Fort, Ajmer", approx_km: 18, type: fort}
      - {name: "Kishangarh", approx_km: 40, type: town}
      - {name: "Sambhar Salt Lake", approx_km: 90, type: lake}
      - {name: "Jaipur", approx_km: 145, type: town}

  - id: mhow
    name: Mhow (Dr. Ambedkar Nagar)
    center: [22.5524, 75.7565]
    langs: [en, hi]
    notes: "Cantonment town: military areas must never be routed through (see §4.3)."
    destinations:
      - {name: "Patalpani Waterfall", approx_km: 10, type: waterfall}
      - {name: "Janapav Kuti", approx_km: 20, type: hill}
      - {name: "Choral Dam", approx_km: 20, type: lake}
      - {name: "Tincha Falls", approx_km: 20, type: waterfall}
      - {name: "Indore", approx_km: 25, type: town}
      - {name: "Omkareshwar", approx_km: 60, type: temple}
      - {name: "Maheshwar Fort", approx_km: 70, type: heritage}
      - {name: "Mandu", approx_km: 75, type: heritage}

  - id: ujjain
    name: Ujjain
    center: [23.1765, 75.7885]
    langs: [en, hi]
    destinations:
      - {name: "Mahakaleshwar Temple", approx_km: 1, type: temple}
      - {name: "Ram Ghat", approx_km: 1, type: river}
      - {name: "Sandipani Ashram", approx_km: 4, type: heritage}
      - {name: "Kal Bhairav Temple", approx_km: 5, type: temple}
      - {name: "Dewas Tekri (Chamunda Mata)", approx_km: 35, type: temple}
      - {name: "Indore", approx_km: 55, type: town}
      - {name: "Omkareshwar", approx_km: 140, type: temple}
      - {name: "Mandu", approx_km: 150, type: heritage}

  - id: stockholm
    name: Stockholm
    country: SE
    timezone: Europe/Stockholm
    center: [59.3293, 18.0686]
    langs: [sv, en]
    default_vehicle: bicycle
    seasons:
      - {name: winter, from: "11-01", to: "03-15", unpaved_penalty: 1.8, confidence_half_life_days: 7}
      - {name: tjallossning, from: "03-15", to: "05-01", unpaved_penalty: 1.5, confidence_half_life_days: 10, note: "spring thaw: gravel roads go soft"}
    destinations:
      - {name: "Drottningholm Palace", approx_km: 12, type: heritage}
      - {name: "Tyresta National Park", approx_km: 25, type: nature}
      - {name: "Vaxholm", approx_km: 35, type: town}
      - {name: "Sigtuna", approx_km: 45, type: town}
      - {name: "Nynäshamn", approx_km: 60, type: town}
      - {name: "Gripsholm Castle, Mariefred", approx_km: 65, type: heritage}
      - {name: "Uppsala Cathedral", approx_km: 70, type: heritage}
      - {name: "Västerås", approx_km: 110, type: town}
```

### 4.3 Regional rules (implement as data, not code branches)
- **Two-wheeler bans (India).** Block `highway=motorway` in all Indian two-wheeler profiles by default: Indian expressways are access-controlled and generally bar two-wheelers. Also respect `motorcycle=no` and `motor_vehicle=no`. Add `data/overrides/in_2w_restrictions.yaml` and seed it with the entries below, each marked `verify: true` because rules change. `datactl` resolves each entry to OSM way IDs (name match + bounding box) and **injects `motor_vehicle=no`** into those ways in the India extract. This is safe because the India graph is two-wheeler-only. Seed list:
  - Mumbai–Pune Expressway
  - Bandra–Worli Sea Link
  - Atal Setu (Mumbai Trans Harbour Link)
  - Bengaluru–Mysuru Expressway (main carriageway)
  - Delhi–Meerut Expressway (expressway lanes)
  - Eastern Peripheral Expressway
  - Western Peripheral (KMP) Expressway
  - Hyderabad Outer Ring Road (main carriageway)
- **Military and cantonment areas.** Mhow is a cantonment, and Delhi Cantt and others have restricted roads. `datactl` extracts `landuse=military` and `military=*` polygons inside coverage and adds them as custom-model `areas` with `multiply_by: 0` in every profile.
- **Gates.** Treat `barrier=gate` with unknown access as a **warning** ("Gate, may be closed at night"), not a block. With `access=private` or `access=no`, it's a block.
- **Sweden.** The Off-Road Driving Act (*terrängkörningslagen*) bans motor vehicles on land off roads. The right of public access (*allemansrätten*) covers walking and cycling, not motor vehicles. So:
  - Stockholm **moped** profiles never use `highway=path`, or `highway=track` without explicit motor access.
  - **Bicycle** profiles may use paths and tracks unless they're tagged otherwise.
  - Private roads (*enskild väg*) are often open to traffic. Follow the OSM `access` tags.
- **Seasons** come from `regions.yaml`. Seasonal penalties only make routes *more* conservative.
- **Languages and local words.** In UI copy, Hindi-region users see "Kaccha road" (unpaved) chips and Swedish users see "Grusväg" (gravel road). Map labels show `name:en` together with the local `name`.

---

## 5. Data pipeline (`cmd/datactl`)

All steps are idempotent and cached under `data/build/`. Commands:

```
datactl coverage   --regions data/regions.yaml           → data/build/coverage_{in,se}.geojson, clusters.json
datactl download   --country in|se                       → Geofabrik extracts (prefer India zone sub-extracts if available; else india-latest), sweden-latest
datactl extract    --country in|se                       → osmium extract -p coverage_*.geojson → coverage_*.osm.pbf
datactl overrides  --country in                          → inject motor_vehicle=no per §4.3 (pyosmium or Go osm lib)
datactl areas      --country in|se                       → military polygons → custom model areas JSON
datactl overlay    --country in|se                       → (weekly) baked community shortcuts merged into extract (§7.6)
datactl graph      --country in|se                       → run GraphHopper import (Docker) → graph-cache-{in,se}
datactl tiles      --country in|se                       → pmtiles extract <protomaps daily build> --region=coverage.geojson --maxzoom=15
datactl seed       --regions data/regions.yaml           → regions, coverage, destinations (geocode with checks) into Postgres
```

- `make data REGION=indore` builds **only that region's cluster** (Indore + Mhow + Ujjain). This is the dev default: fast, and fits about 8 GB of RAM.
- `make data REGION=all` builds everything. Expect hours, ~16 GB of RAM for the India import (set `GH_JAVA_OPTS=-Xmx12g`) and about 60 GB of disk.
- **Fallbacks:**
  - If a Geofabrik download fails, retry 3 times with backoff, then try `download.openstreetmap.fr` extracts.
  - If the Protomaps build download fails, use OpenFreeMap hosted styles in dev (`VITE_TILES_MODE=remote`).
- **Graph refresh:** weekly (Sunday 02:00 local). Build into a new directory, health-check it, then swap (blue/green) with zero downtime.

---

## 6. Routing

### 6.1 Profiles

| Graph | Profile | Purpose |
|---|---|---|
| gh-in | `tw_standard` | What a normal map would suggest: paved, public, no tracks. |
| gh-in | `tw_shortcut` | Allows unclassified/service roads and tracks (grade 1–3), with mild penalties. |
| gh-in | `tw_shortcut_night` | Like shortcut, but tracks are penalised heavily and `lit=yes` is preferred. Used between sunset and sunrise. |
| gh-in | `tw_adventure` | Opt-in: allows grade 4–5 tracks and any unpaved surface. Never the default. |
| gh-se | `bike_standard`, `bike_shortcut`, `moped_standard`, `moped_shortcut` | Stockholm. Moped profiles follow §4.3. |

Use **LM (landmarks)** preparation for all profiles so the API can pass per-request season penalties. These only ever *increase* edge weights, which is what LM requires. Enable `path_details`: `surface`, `road_class`, `track_type`, `road_access`, `max_speed`, `lit` (add `lit` as an encoded value if the installed version supports it; otherwise skip the night lighting preference and log it).

### 6.2 Custom models (in `infra/graphhopper/custom_models/`)
Field names follow GraphHopper's encoded values. **Verify them against the installed version's `docs/core/custom-models.md` and adapt the syntax while keeping the intent.**

`tw_standard.json`
```json
{
  "priority": [
    { "if": "!car_access", "multiply_by": "0" },
    { "if": "road_access == PRIVATE || road_access == NO", "multiply_by": "0" },
    { "if": "road_class == MOTORWAY", "multiply_by": "0" },
    { "if": "road_class == TRACK || road_class == PATH || road_class == FOOTWAY || road_class == BRIDLEWAY || road_class == STEPS || road_class == PEDESTRIAN", "multiply_by": "0" },
    { "if": "surface == DIRT || surface == GROUND || surface == GRAVEL || surface == SAND || surface == GRASS || surface == UNPAVED", "multiply_by": "0.2" }
  ],
  "speed": [
    { "if": "true", "limit_to": "car_average_speed" },
    { "if": "true", "limit_to": "60" },
    { "if": "road_class == RESIDENTIAL || road_class == LIVING_STREET", "limit_to": "20" }
  ]
}
```

`tw_shortcut.json`
```json
{
  "priority": [
    { "if": "!car_access", "multiply_by": "0" },
    { "if": "road_access == PRIVATE || road_access == NO", "multiply_by": "0" },
    { "if": "road_class == MOTORWAY", "multiply_by": "0" },
    { "if": "road_class == PATH || road_class == FOOTWAY || road_class == BRIDLEWAY || road_class == STEPS || road_class == PEDESTRIAN", "multiply_by": "0" },
    { "if": "track_type == GRADE4 || track_type == GRADE5", "multiply_by": "0" },
    { "if": "road_class == TRACK", "multiply_by": "0.7" },
    { "if": "surface == DIRT || surface == GROUND || surface == SAND || surface == GRASS", "multiply_by": "0.75" },
    { "if": "surface == GRAVEL || surface == UNPAVED || surface == COMPACTED", "multiply_by": "0.85" }
  ],
  "speed": [
    { "if": "true", "limit_to": "car_average_speed" },
    { "if": "true", "limit_to": "55" },
    { "if": "road_class == RESIDENTIAL || road_class == LIVING_STREET || road_class == SERVICE", "limit_to": "20" },
    { "if": "road_class == TRACK", "limit_to": "15" },
    { "if": "surface == DIRT || surface == GROUND || surface == SAND", "limit_to": "15" }
  ]
}
```
- `tw_shortcut_night`: the same as `tw_shortcut`, plus `road_class == TRACK → multiply_by 0.3` and `surface DIRT/GROUND → 0.4`.
- `tw_adventure`: drops the grade 4/5 block (×0.5 instead).
- **Military areas** (from `datactl areas`) are appended to every model as `areas` plus `{ "if": "in_military_<n>", "multiply_by": "0" }`.
- **Season** is applied per request: if a season is active, the API adds `{ "if": "<unpaved surfaces>", "multiply_by": "<1/unpaved_penalty>" }`.

### 6.3 Route orchestration (Go, `internal/routing`)
`POST /v1/route` runs these steps:
1. Validate and snap origin/destination. Look up the region with `ST_Contains(coverage, point)`. Return 422 if outside coverage.
2. Compute context: country graph, active season (region time zone), and daylight (sunrise/sunset from lat/lng; use a Go suncalc implementation, written yourself if no maintained lib exists).
3. **In parallel:**
   - (a) `standard` route
   - (b) `shortcut` (or `shortcut_night`) route with `algorithm=alternative_route`, `max_paths=3`
   - (c) a PostGIS candidate query for community shortcuts.
4. **Community shortcut overlay:** candidates are `status='active'` and `confidence ≥ 0.5`, with both endpoints inside the ellipse whose foci are origin and destination and whose major axis is 1.25 × the standard route distance.
   - Lower bound for each candidate: `LB = hav(O,A) + len(S) + hav(B,D)`.
   - Discard candidates with `LB ≥ best_so_far`.
   - Keep the top 5 by LB.
   - For each one, call the GraphHopper **matrix** endpoint once for `O→A` and `B→D` (both directions of a shortcut if it's two-way). Total = matrix legs + shortcut length / profile speed.
   - Pick the best. v1 allows **at most one** community shortcut per route.
5. Assemble the options. For each, compute `surface_breakdown` (paved / unpaved / track metres) from path details. Collect warnings: unpaved stretch, gate, steep, seasonal, night, low confidence.
6. **Recommendation:** recommend the shortcut if it saves ≥2 min **or** ≥20% of the distance, its confidence is ≥0.6 (or it is OSM-only), and it isn't a night track without lighting. Otherwise recommend standard and show the shortcut as an alternative. Never hide the standard option.
7. Cache in Redis under (origin/destination rounded to 5 decimal places, profile set, season, daylight) for 10 minutes.
8. Return results within p95 < 700 ms (see §14).

### 6.4 Navigation
- Use GraphHopper `instructions=true` with locale `en`, `hi` or `sv`.
- On the client, render maneuvers, detect going off-route (>40 m from the route for 3 consecutive fixes) and reroute through the API.
- Voice uses the Web Speech API (`en-IN`, `hi-IN`, `sv-SE`).
- Use the Screen Wake Lock API while navigating.
- Announce surface changes ahead ("Kaccha road in 200 m, 1.2 km long").

---

## 7. Community layer

### 7.1 Shortcut
A shortcut is a LineString contributed by a rider, either **drawn** on the map or **recorded** as a GPS trace while riding. Its endpoints are snapped to the nearest routable OSM nodes (≤30 m; otherwise the submission is rejected with a helpful message). It has these attributes:
- `surface`: paved, unpaved, track
- `vehicles`: scooter, motorcycle, bicycle
- `flags`: gate, floods_in_rain, steep, narrow, no_lights, livestock
- an optional title and photo

### 7.2 Reports (verification)
Riders can mark a shortcut **Passable**, **Blocked** (reason: gate locked, flooded, construction, private/no entry, unsafe, doesn't exist) or **Partly passable**. After every navigation that used a shortcut, the app asks one question: "Was the shortcut passable?" with Yes and No buttons. Limit: one report per user per shortcut per day.

### 7.3 Confidence (Bayesian with time decay)
```
α = 1, β = 1                                  # prior
for each report r:
    w = 0.5 ^ (age_days(r) / half_life)        # half_life from active season, default 30
    t = trust(user) ∈ [0.5, 2.0]
    if r.passable: α += w·t
    if r.blocked:  β += w·t
    if r.partly:   α += 0.5·w·t; β += 0.5·w·t
confidence = α / (α + β)
```
UI bands: **High** ≥0.75, **Medium** 0.5–0.75, **Low** <0.5. Low-confidence shortcuts appear only in adventure mode.

### 7.4 Status lifecycle (worker, hourly)
- `pending` → `active`: ≥2 independent passable reports, or the creator's trust is ≥1.5.
- `active` → `stale`: no reports for 60 days (shown with a "needs a fresh check" chip).
- Any state → `blocked`: ≥2 blocked reports in 7 days with confidence <0.4.
- → `removed`: a moderator confirms a private/no-entry or unsafe report.

### 7.5 Trust and moderation
- Trust starts at 1.0. It goes up when a user's reports agree with the eventual consensus and down when they don't. Range 0.5 to 2.0.
- Admin page (`/admin`, moderator role): queue of new shortcuts and "private/unsafe" reports. Actions: approve, remove, edit flags.
- Rate limits: 10 new shortcuts per user per day, 100 reports per day.

### 7.6 Baking (weekly)
- Active shortcuts with confidence ≥0.75 and ≥5 confirmations are exported as OSM ways (negative IDs; endpoints reuse the snapped OSM node IDs; tags `highway=track` or `unclassified` per surface, plus `pagdandi:shortcut=yes`).
- They're merged into the extract before the weekly graph build, so they become native edges and the overlay step gets cheaper.
- **Don't upload them to OSM automatically.** Instead, offer the user a "Suggest to OpenStreetMap" link that opens the OSM editor at that location.

### 7.7 Desire-path mining (nightly, opt-in traces only)
1. Map-match the traces with GraphHopper's `/match`.
2. Find stretches where the trace stays >25 m from any routable edge for ≥100 m: these are *off-network* segments.
3. Index the off-network points as H3 cells (resolution 11). Link consecutive cells into sequences, and count **distinct users** per sequence over 30 days.
4. Sequences with ≥3 distinct users become `candidate_paths` (k-anonymity ≥3).
5. In the app, nearby riders see a prompt: "Riders seem to cut through here. Is this a usable path?" Two "yes" answers promote it to a `pending` shortcut.

---

## 8. Backend (Go)

### 8.1 Layout
```
services/api/
  cmd/api/main.go           # HTTP server
  cmd/worker/main.go        # background jobs (hourly/nightly/weekly via internal scheduler)
  cmd/datactl/main.go       # data pipeline CLI
  internal/config           # env parsing, validation
  internal/httpx            # router, middleware (request id, slog, recover, CORS, rate limit, auth)
  internal/routing          # orchestration, GH client, suncalc, season, recommendation
  internal/shortcuts        # CRUD, snapping, confidence, lifecycle
  internal/geocode          # local index + Photon client + cache
  internal/auth             # magic link, Google OAuth, sessions
  internal/regions          # coverage, lookup
  internal/mining           # desire paths
  internal/store            # sqlc generated + migrations (goose)
  api/openapi.yaml          # source of truth for HTTP API → generate server types + TS client
```

### 8.2 Endpoints (all under `/v1`, JSON, errors as `{"error":{"code":"...","message":"..."}}`)

| Method | Path | Auth | Purpose |
|---|---|---|---|
| GET | `/regions` | - | Regions, centres, coverage bounding boxes, seasons. |
| GET | `/regions/{id}/destinations` | - | Curated destinations. |
| GET | `/geocode?q=&lat=&lng=&lang=` | - | Autocomplete: local index first, then Photon. |
| GET | `/reverse?lat=&lng=` | - | Reverse geocode. |
| POST | `/route` | - | Body `{from, to, vehicle, mode: "auto"\|"standard"\|"shortcut"\|"adventure", depart_at?, lang}`. |
| GET | `/route/{id}/gpx` | - | GPX export (for OsmAnd and similar apps). |
| GET | `/shortcuts?bbox=` | - | Shortcuts in view (simplified geometry by zoom). |
| GET | `/shortcuts/{id}` | - | Detail plus recent reports (no user identities, just counts and dates). |
| POST | `/shortcuts` | user | Create from a drawn line or a recorded trace. |
| PATCH | `/shortcuts/{id}` | owner/mod | Edit flags or title. |
| POST | `/shortcuts/{id}/reports` | user | Passable / blocked / partly. |
| POST | `/traces` | user + consent | Opt-in ride trace upload. |
| GET | `/candidates?bbox=` | user | Desire-path prompts. |
| POST | `/candidates/{id}/answer` | user | Yes/no. |
| POST | `/auth/magic-link`, GET `/auth/callback`, POST `/auth/logout`, GET `/auth/google/*` | - | Auth. |
| GET | `/me`, `/me/contributions`, DELETE `/me` | user | Profile, contributions, account and data deletion. |
| GET | `/admin/queue`, POST `/admin/shortcuts/{id}/action` | mod | Moderation. |
| GET | `/healthz`, `/readyz`, `/metrics` | - | Ops (Prometheus format). |

### 8.3 Route response (shape)
```json
{
  "request_id": "01J...",
  "region": "indore",
  "context": { "daylight": true, "season": null, "vehicle": "scooter" },
  "options": [
    {
      "id": "std", "kind": "standard",
      "distance_m": 7100, "duration_s": 1020,
      "polyline": "<encoded, precision 5>",
      "surface_breakdown": { "paved_m": 7100, "unpaved_m": 0, "track_m": 0 },
      "instructions": [ { "text": "Turn right onto ...", "distance_m": 350, "sign": 2, "interval": [0, 12] } ],
      "warnings": []
    },
    {
      "id": "sc1", "kind": "shortcut",
      "distance_m": 3400, "duration_s": 600,
      "polyline": "...",
      "legs": [
        { "source": "osm", "from_idx": 0, "to_idx": 40 },
        { "source": "community", "shortcut_id": "uuid", "from_idx": 40, "to_idx": 71 },
        { "source": "osm", "from_idx": 71, "to_idx": 120 }
      ],
      "surface_breakdown": { "paved_m": 2200, "unpaved_m": 1200, "track_m": 0 },
      "confidence": 0.82, "last_confirmed_at": "2026-10-09T08:10:00Z", "confirmations_30d": 4,
      "warnings": [ { "type": "unpaved", "at_m": 1500, "length_m": 1200 }, { "type": "gate", "at_m": 2100 } ],
      "savings": { "distance_m": 3700, "duration_s": 420 },
      "instructions": []
    }
  ],
  "recommended": "sc1",
  "attribution": "© OpenStreetMap contributors"
}
```

### 8.4 Cross-cutting
- **Auth:** email magic link (SMTP from env; in dev, links are logged to the console) plus Google OAuth when its env vars are set. Sessions are HTTP-only, Secure, SameSite=Lax cookies; token hashes are stored in Postgres; sessions last 30 days.
- **Rate limits** (Redis token bucket): anonymous `/route` 60/min per IP; signed-in 120/min.
- **Security:** CSP, CORS allow-list, request body size limits, input validation on all coordinates (inside coverage, finite numbers), parameterised SQL only (sqlc).
- **Config:** env vars only, documented in `.env.example`: `DATABASE_URL`, `REDIS_URL`, `GH_IN_URL`, `GH_SE_URL`, `PHOTON_URL`, `PUBLIC_BASE_URL`, `SMTP_*`, `GOOGLE_OAUTH_*`, `SESSION_SECRET`, `GEOCODER_CONTACT_EMAIL`.
- **Observability:** structured `slog` JSON logs with request IDs, Prometheus metrics (route latency per profile, GraphHopper latency, cache hit rate, shortcut counts), and `/readyz` that checks DB, Redis and both GraphHopper instances.

---

## 9. Database schema (initial migration)

```sql
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
```

---

## 10. Frontend (React)

### 10.1 Structure
```
apps/web/src/
  app/            router, providers (QueryClient, i18n, theme), error boundary
  features/
    map/          MapView, layers (routes, shortcuts, candidates, user dot), style loader
    search/       SearchBar, Results, recents & saved places (localStorage w/ try/catch)
    route/        RouteSheet, RouteCompareCard, SurfaceBar, ConfidenceChip, WarningsList
    navigation/   NavScreen, ManeuverBanner, offroute + reroute, voice, wake lock, PostRidePrompt
    shortcuts/    ShortcutDetail, DrawShortcut, RecordRide, ReportSheet
    explore/      RegionPicker, DestinationList (by type), DestinationCard
    profile/      Contributions, Settings (language, units, vehicle, theme, traces consent)
    offline/      OfflinePacks (download region PMTiles subset + recent routes)
    admin/        ModerationQueue
  components/ui/  Button, IconButton, Sheet, Chip, SegmentedControl, ListRow, Toast, Skeleton, EmptyState, ErrorState, Kbd
  lib/            api (generated client), geo (polyline decode, haversine), format (km, min), i18n, time (suncalc)
  styles/         tokens.css, base.css
  i18n/           en.json, hi.json, sv.json
```

### 10.2 Screens and flows (mobile first)
1. **Home/Map.** The map is full-bleed. At the top sits a search pill ("Where to?") with a vehicle icon button. Below it are recent destinations as chips. A locate button sits bottom-right. The bottom sheet starts collapsed and shows "Nearby shortcuts" (count) plus the region's top destinations.
2. **Search.** Opens as a full-height sheet: results grouped as Saved, Recent, Destinations (curated) and Places. One tap on a result goes straight to the route.
3. **Route compare (the core screen).** The sheet opens at half height with two rows:
   - "Usual way · 7.1 km · 18 min"
   - "Pagdandi · 3.4 km · 9 min · saves 9 min" with a *Recommended* tag if applicable.
   - Each row shows a slim surface bar (paved / kaccha / track proportions), a confidence chip ("High · confirmed 3 days ago by 4 riders") and warning chips (Gate, Kaccha 1.2 km, Night).
   - Tapping a row highlights that route on the map. Primary CTA: **Ride this way**. Secondary actions: GPX export, Open in Google Maps.
4. **Navigation.** A large maneuver banner on top (distance plus arrow plus street), ETA and remaining distance at the bottom, a surface warning banner when unpaved road is within 300 m, and a mute toggle. On arrival, the **one-question post-ride prompt** appears if a community shortcut was used.
5. **Shortcut detail.** Geometry on the map, surface, flags, a confidence history sparkline (last 90 days), photo, and Report buttons.
6. **Add shortcut.** Two modes:
   - **Draw:** tap to add points, the endpoints auto-snap with visual feedback, undo is available.
   - **Record ride:** start/stop, live trace, trim the ends.
   - Then a short form (surface, flags, vehicles) and submit. Explain that it goes live after two riders confirm it.
7. **Explore.** A region picker (18 regions) and destinations filtered by type (waterfall, fort, lake, temple, hill, wildlife, heritage, beach). Each card shows the shortcut count near it.
8. **Profile and settings:** contributions, trust level ("Trusted rider" badge once ≥1.5), language, theme (auto switches with sunset), vehicle, traces consent (off by default), offline packs, and account deletion.

**Desktop:** a 400 px left panel holds the sheet content and the map fills the rest. Same components.

### 10.3 Behaviour details
- Initial route request uses `mode: "auto"`. Show skeletons in under 100 ms and results when ready. Never block the map.
- Every async view has proper **loading, empty, error and offline states**.
- Geolocation: ask only when the user taps Locate or Ride. Explain why before the browser prompt appears.
- **PWA:**
  - Cache the app shell.
  - "Offline pack" downloads a region's PMTiles subset (z0–15 within 25 km of the city) plus the last 20 routes.
  - Routing offline isn't supported in v1. Show a clear message.
- **Performance budgets:** initial JS ≤ 200 KB gzip (lazy-load the navigation, draw and admin features), LCP < 2.5 s on a mid-range Android over 4G, route render < 100 ms after the response arrives.

---

## 11. Design system ("sleek, clean, not vibe-coded")

### 11.1 Principles
- **The map is the hero.** UI chrome is minimal, flat and quiet. Colour is reserved for routes and status.
- **One accent colour** (earth orange), used for the shortcut route and the primary CTA. Everything else is neutral.
- **Information density done right:** numbers are big and tabular; labels are small and muted.
- **One-hand use:** primary actions in the bottom 40% of the screen, tap targets ≥48 px (gloves).

### 11.2 Tokens (`styles/tokens.css`)
```css
:root {
  --bg: #F6F5F2;  --surface: #FFFFFF;  --surface-2: #F0EEEA;
  --ink: #17191C; --ink-2: #4A4F57;   --ink-3: #8A9099;
  --line: #E4E1DB;
  --accent: #C2410C;         /* pagdandi route + primary CTA */
  --accent-ink: #FFFFFF;
  --route-standard: #3B4A5A; /* usual route, slate */
  --ok: #2F7D32; --warn: #B45309; --danger: #B42318;
  --radius-sm: 8px; --radius-md: 12px; --radius-lg: 16px;
  --shadow-sheet: 0 -1px 0 var(--line), 0 -8px 24px rgba(0,0,0,.06);
  --space: 4px;              /* spacing scale = multiples of 4 */
  --dur-fast: 120ms; --dur: 200ms; --ease: cubic-bezier(.2,.8,.2,1);
}
:root[data-theme="dark"], .theme-dark {
  --bg: #0E1012; --surface: #16191C; --surface-2: #1D2125;
  --ink: #ECEDEE; --ink-2: #B4B9BF; --ink-3: #7D838A;
  --line: #2A2F35;
  --accent: #FF7A3D; --route-standard: #9AA8B8;
}
```
- **Typography:** "Instrument Sans" for UI and "IBM Plex Mono" for numbers (distances, times) with `font-variant-numeric: tabular-nums`. Use "Noto Sans Devanagari" for Hindi. Type scale: 12 / 14 / 16 / 20 / 24 / 32. Weights 400/500/600 only.
- **Icons:** Lucide only, 20 px, stroke 1.75, never mixed with other sets. No emoji in the UI.

### 11.3 Map style
- Start from the Protomaps light/dark basemap and **customise** it:
  - lower saturation
  - water `#CFDFE6`, parks `#DCE6D3`
  - roads white with a `#D9D5CE` casing
  - halve the POI label density
  - hide minor POIs below z15
- **Route rendering:**
  - Standard: 6 px `--route-standard` with a 2 px white casing.
  - Shortcut: 7 px `--accent` with a white casing.
  - Unpaved segments are **dashed**; tracks are **dotted**.
  - Community-shortcut legs get a small "Rider shortcut" label at the midpoint.
- **Don't rely on colour alone:** dash patterns plus labels carry the meaning.
- Dark map style is applied automatically between local sunset and sunrise, unless the user overrides it.

### 11.4 Components (key specs)
- **Bottom sheet** (vaul): snap points 18% / 52% / 92%, grabber 36×4 px, `--radius-lg` top corners, no backdrop at the 18% and 52% snaps.
- **RouteCompareCard:** a 2-row list; the selected row gets a 2 px left border in the route's colour. Primary number 24 px mono, secondary 14 px `--ink-2`.
- **SurfaceBar:** 6 px tall with rounded ends. Segments: paved `--ink-3` at 40% opacity, kaccha `--warn`, track `--danger` at 70%. Legend on tap.
- **ConfidenceChip:** dot plus text ("High · 3d ago"), with neutral background and colours from the band.
- **Buttons:** primary (accent fill), secondary (surface with line border), ghost. Height 48 px on mobile and 40 px on desktop. No gradients.

### 11.5 Motion
- Durations 120–200 ms with ease-out. Sheet snapping uses a spring.
- Route lines draw on with a 300 ms line-progress animation once per route load.
- Respect `prefers-reduced-motion`.

### 11.6 Copy tone
Short, plain and local: "Saves 9 min", "Kaccha road · 1.2 km", "Confirmed by 4 riders this week", "Gate here may be closed after dark". Avoid marketing language and exclamation marks.

### 11.7 Anti-vibe-code checklist (must pass before M7 is done)
- [ ] No purple/blue gradients, glassmorphism or glow shadows; no "hero + 3 feature cards" layouts.
- [ ] Only design tokens are used; no ad-hoc hex values in components (enforce with a lint rule or grep check in CI).
- [ ] Consistent 4 px spacing grid, one radius scale, one icon set.
- [ ] Every list has empty, loading (skeleton) and error states with a recovery action.
- [ ] Numbers are tabular and units always shown (km, min).
- [ ] Contrast meets WCAG AA in both themes (axe passes).
- [ ] Tap targets are ≥48 px and the main CTA is reachable with a thumb.
- [ ] No layout shift when the sheet content loads (reserved heights).
- [ ] Hindi and Swedish strings don't overflow (test with the longest strings).

---

## 12. Repo layout
```
pagdandi/
  CLAUDE.md
  README.md
  Makefile
  .env.example
  docs/ SPEC.md  PROGRESS.md  DECISIONS.md  OPERATIONS.md
  data/ regions.yaml  overrides/in_2w_restrictions.yaml  build/ (gitignored)
  infra/
    docker-compose.yml          # profiles: dev, prod
    caddy/Caddyfile
    graphhopper/config-in.yml  config-se.yml  custom_models/*.json
  services/api/                 # Go module (see §8.1)
  apps/web/                     # React app (see §10.1)
  tests/fixtures/synthetic-town.osm   # tiny hand-built OSM file for deterministic profile tests
  .github/workflows/ci.yml
```

---

## 13. Operating instructions (for the owner)

### 13.1 Prerequisites
- Docker (Docker Desktop on Windows with the WSL2 backend; allocate ≥8 GB of memory for single-region dev, ≥16 GB for `REGION=all`).
- Windows users: run everything inside **WSL2 Ubuntu**.
- Git, Make, latest stable Go, Node LTS (Claude Code installs anything missing).
- About 15 GB of free disk for single-region dev, about 60 GB for all regions.

### 13.2 Commands
```
make bootstrap              # install deps, create .env from .env.example, start Postgres+Redis, run migrations
make data REGION=indore     # build Indore cluster (Indore+Mhow+Ujjain): extract, graph, tiles, seed  (~15–30 min first time)
make data REGION=all        # build all 18 regions (hours; needs RAM per §5)
make dev                    # run api, worker, graphhopper, web with hot reload
                            #   web  → http://localhost:5173
                            #   api  → http://localhost:8080/v1
                            #   gh   → http://localhost:8989 (in), :8990 (se)
make tunnel                 # HTTPS tunnel (cloudflared quick tunnel) so you can test on your phone with GPS
make test                   # Go + web unit/integration tests
make e2e                    # Playwright (mobile viewports, mocked GPS)
make lint                   # go vet, staticcheck, eslint, tsc, token check
make seed-dev               # sample shortcuts + reports around Bilawali, Indore for demo
make admin EMAIL=you@x.com  # make a user moderator/admin
make refresh-graph          # weekly OSM refresh with blue/green swap
make deploy                 # production deploy (§16); skips if credentials missing
```
- **Testing on your phone:** browsers only allow GPS on HTTPS, so use `make tunnel` and open the printed `https://…trycloudflare.com` URL on your phone.
- **Adding a city:** append it to `data/regions.yaml`, then run `make data REGION=<id>` (or `all`). Nothing else changes.
- **Logging in during dev:** request a magic link; the link is printed in the API logs.

---

## 14. Testing and quality gates
- **Profile tests (deterministic):** `tests/fixtures/synthetic-town.osm` is a small grid with:
  - a paved ring road
  - a diagonal `highway=track` shortcut
  - a private road
  - a motorway
  - a military polygon
  - a gate

  Assert that standard avoids the track, shortcut takes it, both avoid private, motorway and military, and the gate produces a warning.
- **Go unit tests:** confidence maths (including decay and trust), status lifecycle, ellipse candidate filter, lower-bound pruning, recommendation rules, season and daylight selection, coverage lookup, polyline/leg stitching.
- **Integration:** testcontainers (PostGIS, Redis) plus a GraphHopper container on the synthetic fixture.
- **Region acceptance (after `REGION=all`):** for each region, route city centre → 3 destinations with both modes. Every call returns 200, p95 < 700 ms, and the standard route never uses track/private/motorway.
- **Web:**
  - Vitest component tests for RouteCompareCard, SurfaceBar, ConfidenceChip, the sheet and formatters.
  - Playwright on Pixel 7 and iPhone 14 viewports: search → compare → ride → arrive → post-ride prompt; draw shortcut; offline banner.
  - axe on every screen.
  - Visual snapshots of the core screens in light and dark themes.
- **Lighthouse CI (mobile):** performance ≥ 90, accessibility ≥ 95, best practices ≥ 95, PWA installable.
- **CI** (`.github/workflows/ci.yml`): lint → unit → integration → build → e2e (synthetic fixture only).

---

## 15. Milestones (do them in order; each needs green tests and a commit)

| # | Milestone | Acceptance criteria |
|---|---|---|
| M0 | Repo, tooling, compose skeleton | `make bootstrap && make dev` boots all services with health checks green; CI passes lint and empty tests. |
| M1 | Data pipeline, single region | `make data REGION=indore` produces coverage, extract, overrides, military areas, GraphHopper graph and PMTiles; GraphHopper answers Indore → Bilawali with `tw_standard` and `tw_shortcut`. |
| M2 | Go API core | `/regions`, `/destinations` (geocoded with checks), `/geocode`, `/route` (standard + shortcut + recommendation + warnings + cache), OpenAPI spec, generated TS client; synthetic-fixture profile tests pass. |
| M3 | Web app core and design system | Tokens, map style, search, route compare, desktop + mobile layouts; Playwright flow search → compare passes; anti-vibe-code checklist items for these screens pass. |
| M4 | Community layer | Auth, draw/record shortcut, snapping, reports, confidence, lifecycle worker, overlay routing with community legs; tests for each. |
| M5 | Navigation | Turn-by-turn, off-route reroute, voice (en/hi/sv), wake lock, surface warnings, night profile, seasons, post-ride prompt. |
| M6 | All 18 regions | `make data REGION=all`, Stockholm bike/moped profiles and the Sweden rules, i18n hi/sv, region acceptance tests pass. |
| M7 | Polish, PWA, performance, accessibility | Offline packs, Lighthouse budgets met, axe clean, the full §11.7 checklist passes, empty/error states everywhere. |
| M8 | Desire paths and moderation | Trace consent flow, nightly mining, candidate prompts, admin queue, trust updates. |
| M9 | Deploy and docs | Prod compose profile, Caddy, backups, weekly refresh job, `docs/OPERATIONS.md`, README with screenshots; deploy only if credentials exist. |

---

## 16. Deployment
- **Backend:** one VM in an India region (e.g. Mumbai) with 4 vCPU, 32 GB RAM and 100 GB SSD runs the Docker Compose `prod` profile: Caddy (automatic TLS), API, worker, gh-in, gh-se, Postgres, Redis. GraphHopper needs a long-running JVM with lots of RAM, so **the backend can't run on serverless platforms** like Vercel functions.
- **Frontend:** served as static files by Caddy. Optionally use Vercel for the web app alone, with `VITE_API_BASE` pointing at the VM.
- **Backups:** nightly `pg_dump` to object storage with 14-day retention.
- **Weekly graph refresh:** blue/green, as described in §5.
- **Monitoring:** `/readyz` uptime check every minute; alert if route p95 is above 1.5 s for 10 minutes.
- **Secrets:** `.env` on the server, never in git.

---

## 17. Privacy, legal and safety
- **Attribution:** show "© OpenStreetMap contributors" (ODbL) and the tile attribution on the map at all times. Derived databases from OSM data (the baked shortcuts) must stay ODbL-compatible.
- **India DPDP Act 2023 and GDPR (Stockholm users):**
  - Give a clear consent notice for location traces (off by default).
  - Use data only for the stated purpose.
  - Let users delete their account and data with one action (`DELETE /v1/me`).
  - Keep traces for at most 90 days.
- **Trace privacy:** trim the first and last 200 m of every trace, and cut 300 m around user-saved Home/Work. Desire paths need ≥3 distinct users before anything is shown.
- **Safety:**
  - Show a persistent disclaimer on shortcut routes: "Unverified roads can change. Ride at your own judgement."
  - Never route through private, no-access or military areas.
  - Be more conservative at night and in monsoon or winter.
  - Warn about gates and flooding flags.
- **No Google data, ever** (see §1.5).

---

## 18. Roadmap after v1
- Native app builds via Capacitor (Play Store first); background GPS for ride recording.
- Chaining two community shortcuts in one route.
- Offline routing on device for downloaded packs (GraphHopper's offline approach or a WASM router).
- Photo verification with on-device blur of faces and number plates.
- More cities through `regions.yaml` alone.
- A "Suggest to OSM" flow that lets trusted riders contribute verified tracks back to OpenStreetMap.
