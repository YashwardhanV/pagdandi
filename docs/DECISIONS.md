# Decisions (v2)

Made autonomously, as the owner asked. Each with the reason.

1. **Start over, keep the plumbing.** v1's product (community shortcuts, trust scores, moderation, accounts) was
   the wrong product. Kept: the GraphHopper + osmium + Protomaps pipeline idea and the geo/sun helpers.
2. **No database, no accounts.** Nothing in v2 needs to be stored server-side. Recents and preferences live in
   the browser. Running it went from five services to three (GraphHopper, API, Caddy).
3. **One permissive base profile per mode of travel, characters per request.** Four LM-prepared profiles instead
   of a profile per character keeps the 8 GB laptop build at ~1 minute and lets new characters ship without a
   re-import. Request models only lower priorities, so LM stays exact (unit-tested).
4. **"Less taken" is estimated, not measured.** No traffic data exists for this. Road class busyness × built-up
   share is honest and explainable ("82 % quiet").
5. **"Newer" = first added to OSM in the last 18 months** (way version 1). Real-world construction dates aren't
   knowable; OSM history is. Labelled "new on the map since <month>".
6. **Scenic from OSM land cover + elevation, not satellite ML.** A 50 m land grid makes scoring O(1) per sample,
   explainable ("Along Sirpur Lake 1.2 km"), and cheap. Satellite imagery is offered as a map base instead.
7. **In towns only lakes are scenic.** Indore's Saraswati and Kanh are mapped as rivers but are drains in the
   city; scoring "along the river" there would be dishonest. GraphHopper's urban_density plus the built-up grid
   decides "in town".
8. **Detour budget as one slider.** "How far will you stray?" (0–40 km, auto ≈ 35 % of the usual) covers both
   "shorter" and "adventurous" without a settings page.
9. **Wander radius on an exponential scale.** 1–100 km on a linear slider makes 1–10 km unusable; exponential
   gives the same thumb travel to 1→5, 5→25 and 25→100.
10. **Jetski routes on the land grid's water cells** (A* with shore-distance costs, string-pulled). GraphHopper
    can't route water. 50 m cells lose very narrow channels — acceptable for lakes and the Narmada. Each trip
    stays on one water body; the UI says so plainly when it can't.
11. **Two-wheelers: motorways blocked by road class.** Indian expressways ban two-wheelers; v1's name-matched
    override list had no entries near Indore. Cars may use motorways.
12. **Tracks without car access are allowed for motor vehicles** unless access=private/no. OSM rarely tags
    motor access on Indian tracks; the label ("track, dirt") carries the honesty.
13. **Military areas blocked in every profile** (polygons from OSM, simplified to 8 m) — e.g. Mhow cantonment.
14. **Satellite = Esri World Imagery, terrain = AWS Terrarium tiles.** Free with attribution for this use; switch
    to a licensed provider (e.g. MapTiler) before any commercial launch.
15. **Fonts self-hosted** (Fontsource) so the PWA works offline and no third-party font CDN sees users.
16. **Map labels in English.** MapLibre can't shape Devanagari; the UI shows Hindi names in HTML instead.
17. **Search = local gems first, then Photon.** Gems answer instantly client-side while Photon (public, ≤ 1 req/s)
    fills in addresses.
18. **Name fixes from names.** OSM around Indore often tags waterfalls as cliffs and forts as monuments; a
    name-based refinement (falls/kund → waterfall, garh/qila → fort …) fixes most. Company names tagged as
    attractions are dropped.
