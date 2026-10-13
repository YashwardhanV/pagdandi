# Progress

Last updated: 2026-10-13. v2 rebuilt from scratch on branch `worktree-redesign-v2`.

## Done and verified on real Indore data

- **Data pipeline:** Indore extract (3.95 M nodes, 487 k ways), 3 military areas, 101 gems, 12.6 k scenic anchors,
  244 named water bodies (Narmada pieces named and merged), 4,162 ways new on the map since Apr 2025, graph with
  elevation in ~1 min, 45 MB basemap. OSM data as of 12 Oct 2026.
- **Go:** e.g. Indore → Patalpani Falls by bike: The Usual 30 km / 42 min, Scenic (past Lal Bagh Palace), Quiet
  Lanes, New Ground (3.9 km of road mapped since Apr 2025) in ~300 ms. All six vehicles plan.
- **Wander:** places (waterfalls, forts, Ralamandal, lakes…), loops (three 50–62 km loops for 25 km), fresh-road
  destinations, jetski water bodies and lake circuits (Bilawali Lake).
- **Web:** both moods, satellite, 3D terrain, new-roads layer, search, ride with GPS + preview, PWA build.
- **Tests:** Go (planner with fake GraphHopper, LM-safety of models, water router, raster), Vitest (sun, geo,
  format, navigation engine).

## Known gaps

1. **OSM is thin around Indore.** Few surface tags (most roads assumed paved), ~100 gems. Kaccha trails often show
   as "village roads and tracks" rather than mapped dirt. Better data = better trails; mapping parties help.
2. **Yeshwant Sagar** has no name on its OSM polygon, so it appears as "Unnamed reservoir".
3. **Not field-tested on a phone with real GPS while riding.** Preview mode is tested.
4. **Search** depends on the public Photon instance (rate-limited). Self-host for production.
5. **Jetski** routes on 50 m cells; channels narrower than ~100 m are not navigable in the model.
6. **No deploy yet** — `make deploy` needs a VM and domain.
