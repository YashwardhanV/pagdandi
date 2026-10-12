package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"pagdandi/api/internal/store"
)

// cmdSeed loads the selected regions, their coverage and geocoded destinations
// into Postgres. Destinations failing the sanity checks are stored unverified and
// listed in data/build/geocode-report.md (SPEC §4.1).
func cmdSeed(ctx context.Context, o *opts) error {
	sel, err := loadSelection(o)
	if err != nil {
		return err
	}
	b, err := os.ReadFile(filepath.Join(o.build, "coverage", "regions.json"))
	if err != nil {
		return fmt.Errorf("run coverage first: %w", err)
	}
	cov := map[string]string{}
	if err := json.Unmarshal(b, &cov); err != nil {
		return err
	}
	resolved, err := loadResolved(o)
	if err != nil {
		return err
	}
	db, err := openDB(ctx, o)
	if err != nil {
		return err
	}
	defer db.Close()
	gc := newGeocoder(o)

	unverified := 0
	for _, id := range sel.Regions {
		reg, _ := o.regions.Get(id)
		cfg, _ := json.Marshal(map[string]any{
			"langs": reg.Langs, "seasons": reg.Seasons, "default_vehicle": reg.DefaultVehicle,
			"corridors": reg.Corridors, "notes": reg.Notes,
		})
		if err := db.UpsertRegion(ctx, store.RegionRow{
			ID: reg.ID, Name: reg.Name, Country: reg.Country, Timezone: reg.Timezone, Center: reg.Center,
			RadiusKm: int(reg.RadiusKm), Config: cfg,
		}, cov[id]); err != nil {
			return fmt.Errorf("region %s: %w", id, err)
		}
		var ds []store.Destination
		for _, d := range reg.Destinations {
			key := reg.ID + "/" + d.Name
			res, ok := resolved[key]
			if !ok || o.force {
				res = gc.resolve(ctx, reg, d.Name, d.ApproxKm, true)
				resolved[key] = res
				if err := saveResolved(o, resolved); err != nil {
					return err
				}
				o.log.Info("geocoded", "region", id, "name", d.Name, "verified", res.Verified, "straight_km", res.DistKm, "reason", res.Reason)
			}
			approx := int(d.ApproxKm)
			x := store.Destination{Name: d.Name, Category: d.Type, ApproxKm: &approx, Verified: res.Verified}
			if res.Verified {
				lat, lng := res.Lat, res.Lng
				x.Lat, x.Lng = &lat, &lng
			} else {
				unverified++
			}
			ds = append(ds, x)
		}
		if err := db.ReplaceDestinations(ctx, id, ds); err != nil {
			return fmt.Errorf("destinations %s: %w", id, err)
		}
		o.log.Info("seeded", "region", id, "destinations", len(ds))
	}
	o.log.Info("seed", "unverified_this_run", unverified)
	// The report covers every region seeded so far, not just this run.
	var keys []string
	for k, r := range resolved {
		if !r.Verified {
			keys = append(keys, k)
		}
	}
	sort.Strings(keys)
	var all strings.Builder
	all.WriteString("# Destination geocoding report\n\nUnverified destinations (kept without coordinates; never invented):\n\n")
	for _, k := range keys {
		r := resolved[k]
		fmt.Fprintf(&all, "- %s (approx %.0f km): %s\n", k, r.ApproxKm, r.Reason)
	}
	if len(keys) == 0 {
		all.WriteString("- none\n")
	}
	return os.WriteFile(filepath.Join(o.build, "geocode-report.md"), []byte(all.String()), 0o644)
}
