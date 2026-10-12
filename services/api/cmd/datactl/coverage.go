package main

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"pagdandi/api/internal/geo"
	"pagdandi/api/internal/regions"
	"pagdandi/api/internal/store"
)

// Cluster is a set of regions whose coverage overlaps (SPEC §4.1).
type Cluster struct {
	ID      string   `json:"id"`
	Country string   `json:"country"`
	Regions []string `json:"regions"`
}

// Selection is what this build covers.
type Selection struct {
	Region    string    `json:"region"` // "" = all
	Regions   []string  `json:"regions"`
	Countries []string  `json:"countries"`
	Clusters  []Cluster `json:"clusters"`
}

func ringsGeoJSON(rings [][][2]float64) []string {
	out := make([]string, len(rings))
	for i, r := range rings {
		b, _ := json.Marshal(map[string]any{"type": "Polygon", "coordinates": [][][2]float64{r}})
		out[i] = string(b)
	}
	return out
}

// unionGeoJSON unions GeoJSON geometries in PostGIS and returns GeoJSON.
func unionGeoJSON(ctx context.Context, db *store.DB, geoms []string) (string, error) {
	var out string
	err := db.Pool.QueryRow(ctx, `
select ST_AsGeoJSON(ST_Multi(ST_Union(ST_MakeValid(ST_SetSRID(ST_GeomFromGeoJSON(g), 4326)))), 6)
from unnest($1::text[]) as g`, geoms).Scan(&out)
	return out, err
}

func writeFeature(path, geometry string, props map[string]any) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	f := map[string]any{"type": "Feature", "properties": props, "geometry": json.RawMessage(geometry)}
	b, err := json.Marshal(f)
	if err != nil {
		return err
	}
	return os.WriteFile(path, b, 0o644)
}

func cmdCoverage(ctx context.Context, o *opts) error {
	db, err := openDB(ctx, o)
	if err != nil {
		return err
	}
	defer db.Close()
	gc := newGeocoder(o)
	resolved, err := loadResolved(o)
	if err != nil {
		return err
	}

	// 1. Per-region coverage: 64-gon circle plus corridors.
	cov := map[string]string{}
	for _, r := range o.regions.Regions {
		reg := r
		resolve := func(name string) (geo.Point, bool) {
			key := reg.ID + "/corridor/" + name
			if res, ok := resolved[key]; ok && res.Verified {
				return geo.Point{Lat: res.Lat, Lng: res.Lng}, true
			}
			var approx float64
			for _, c := range reg.Corridors {
				if c.To == name {
					approx = c.ApproxKm
				}
			}
			res := gc.resolve(ctx, reg, name, approx, false)
			resolved[key] = res
			if !res.Verified {
				o.log.Warn("corridor destination not verified; corridor skipped", "region", reg.ID, "to", name, "reason", res.Reason)
				return geo.Point{}, false
			}
			return geo.Point{Lat: res.Lat, Lng: res.Lng}, true
		}
		u, err := unionGeoJSON(ctx, db, ringsGeoJSON(reg.CoverageRings(resolve)))
		if err != nil {
			return fmt.Errorf("coverage %s: %w", reg.ID, err)
		}
		cov[reg.ID] = u
	}
	if err := saveResolved(o, resolved); err != nil {
		return err
	}

	// 2. Clusters: union-find within a country, merging regions whose circles
	// overlap substantially: one centre lies inside the other's circle. Plain
	// intersection would chain 150 km circles across half of India
	// (Indore→Ujjain→Udaipur→Pushkar→Jaipur→Delhi); this rule yields the
	// clusters SPEC §4.1 names (Indore/Mhow/Ujjain, Delhi/Gurugram, ...).
	ids := make([]string, 0, len(cov))
	for id := range cov {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	parent := map[string]string{}
	for _, id := range ids {
		parent[id] = id
	}
	var find func(string) string
	find = func(x string) string {
		for parent[x] != x {
			parent[x] = parent[parent[x]]
			x = parent[x]
		}
		return x
	}
	for i := 0; i < len(ids); i++ {
		for j := i + 1; j < len(ids); j++ {
			ri, _ := o.regions.Get(ids[i])
			rj, _ := o.regions.Get(ids[j])
			if ri.Country != rj.Country {
				continue
			}
			d := geo.Haversine(ri.CenterPoint(), rj.CenterPoint()) / 1000
			if d <= math.Max(ri.RadiusKm, rj.RadiusKm) {
				parent[find(ids[i])] = find(ids[j])
			}
		}
	}
	groups := map[string][]string{}
	for _, id := range ids {
		groups[find(id)] = append(groups[find(id)], id)
	}
	var clusters []Cluster
	for _, members := range groups {
		sort.Strings(members)
		r, _ := o.regions.Get(members[0])
		clusters = append(clusters, Cluster{ID: strings.Join(members, "+"), Country: r.Country, Regions: members})
	}
	sort.Slice(clusters, func(i, j int) bool { return clusters[i].ID < clusters[j].ID })

	// 3. Selection: one cluster (--region) or everything.
	sel := Selection{Region: o.region}
	for _, c := range clusters {
		if o.region == "" || contains(c.Regions, o.region) {
			sel.Clusters = append(sel.Clusters, c)
			sel.Regions = append(sel.Regions, c.Regions...)
			if !contains(sel.Countries, c.Country) {
				sel.Countries = append(sel.Countries, c.Country)
			}
		}
	}
	sort.Strings(sel.Countries)

	// 4. Write per-region coverage, cluster and country files.
	if err := writeJSON(filepath.Join(o.build, "coverage", "regions.json"), cov); err != nil {
		return err
	}
	if err := writeJSON(filepath.Join(o.build, "clusters.json"), map[string]any{"clusters": clusters}); err != nil {
		return err
	}
	if err := writeJSON(filepath.Join(o.build, "selection.json"), sel); err != nil {
		return err
	}
	old, _ := filepath.Glob(filepath.Join(o.build, "coverage", "cluster-*.geojson"))
	for _, f := range old {
		_ = os.Remove(f)
	}
	for _, c := range clusters {
		var gs []string
		for _, id := range c.Regions {
			gs = append(gs, cov[id])
		}
		u, err := unionGeoJSON(ctx, db, gs)
		if err != nil {
			return err
		}
		if err := writeFeature(filepath.Join(o.build, "coverage", "cluster-"+c.ID+".geojson"), u, map[string]any{"cluster": c.ID}); err != nil {
			return err
		}
	}
	for _, country := range []string{"IN", "SE"} {
		var gs []string
		for _, id := range sel.Regions {
			if r, _ := o.regions.Get(id); r.Country == country {
				gs = append(gs, cov[id])
			}
		}
		path := filepath.Join(o.build, "coverage_"+strings.ToLower(country)+".geojson")
		if len(gs) == 0 {
			_ = os.Remove(path)
			continue
		}
		u, err := unionGeoJSON(ctx, db, gs)
		if err != nil {
			return err
		}
		// Only rewrite when the geometry changes, so downstream steps stay cached.
		if old, err := os.ReadFile(path); err == nil {
			var f struct {
				Geometry json.RawMessage `json:"geometry"`
			}
			if json.Unmarshal(old, &f) == nil && string(f.Geometry) == u {
				continue
			}
		}
		if err := writeFeature(path, u, map[string]any{"country": country, "regions": sel.Regions}); err != nil {
			return err
		}
	}
	o.log.Info("coverage", "clusters", len(clusters), "selected_regions", sel.Regions, "countries", sel.Countries)
	for _, c := range clusters {
		o.log.Info("cluster", "id", c.ID, "country", c.Country)
	}
	return nil
}

func loadSelection(o *opts) (*Selection, error) {
	var s Selection
	b, err := os.ReadFile(filepath.Join(o.build, "selection.json"))
	if err != nil {
		return nil, fmt.Errorf("run `datactl coverage` first: %w", err)
	}
	return &s, json.Unmarshal(b, &s)
}

func writeJSON(path string, v any) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(b, '\n'), 0o644)
}

func contains(ss []string, s string) bool {
	for _, x := range ss {
		if x == s {
			return true
		}
	}
	return false
}

var _ = regions.Region{}
