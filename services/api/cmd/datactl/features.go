package main

import (
	"context"
	"fmt"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode"

	"pagdandi/api/internal/features"
	"pagdandi/api/internal/geo"
	"pagdandi/api/internal/raster"
)

// Tags that matter for scenery, destinations and water. osmium keeps any
// object matching one of these.
var featureFilters = []string{
	"nwr/natural=water,wood,scrub,grassland,heath,peak,waterfall,cave_entrance,hot_spring,beach,cliff",
	"nwr/water",
	"nwr/waterway=river,riverbank,waterfall,dam",
	"nwr/landuse=forest,reservoir,basin,residential,commercial,industrial,retail,meadow,grass,orchard,farmland,recreation_ground,village_green",
	"nwr/leisure=park,garden,nature_reserve",
	"nwr/boundary=protected_area,national_park",
	"nwr/tourism=viewpoint,attraction,picnic_site,camp_site",
	"nwr/historic=fort,castle,ruins,monument,archaeological_site,palace,city_gate,memorial",
	"nwr/amenity=place_of_worship",
}

var freshHighways = []string{
	"motorway", "trunk", "primary", "secondary", "tertiary", "unclassified", "residential", "service",
	"track", "living_street", "road", "path", "footway", "cycleway", "bridleway",
}

const cellM = 50.0

func cmdFeatures(ctx context.Context, o *opts) error {
	in := extractPath(o)
	dir := featuresDir(o)
	regionFile := filepath.Join(o.root, "data", "region.yaml")
	if !o.force && fresh(filepath.Join(dir, features.MetaFile), in, regionFile) {
		o.log.Info("cached", "step", "features")
		return nil
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	_ = os.MkdirAll(tmpDir(o), 0o755)

	featPBF := filepath.Join(tmpDir(o), "features.osm.pbf")
	featSeq := filepath.Join(tmpDir(o), "features.geojsonseq")
	args := append([]string{"tags-filter", "--overwrite", "-o", featPBF, in}, featureFilters...)
	if err := sh(ctx, o, osmiumBin(o), args...); err != nil {
		return err
	}
	if err := sh(ctx, o, osmiumBin(o), "export", "--overwrite", "-f", "geojsonseq", "-u", "type_id", "-o", featSeq, featPBF); err != nil {
		return err
	}

	b := newBuilder(o)
	if err := readSeq(featSeq, b.add); err != nil {
		return err
	}
	b.finish()

	fresh, err := freshWays(ctx, o)
	if err != nil {
		return err
	}

	if err := b.grid.Write(filepath.Join(dir, features.RasterFile)); err != nil {
		return err
	}
	for name, v := range map[string]any{
		features.GemsFile: b.gems, features.AnchorsFile: b.anchors, features.WatersFile: b.waters, features.FreshFile: fresh,
	} {
		if err := features.WriteJSON(dir, name, v); err != nil {
			return err
		}
	}
	meta := features.Meta{
		BuiltAt:  time.Now().UTC().Format(time.RFC3339),
		OSMDate:  osmDate(ctx, o, sourcePath(o, o.reg.OSMSources[0].Name)),
		FreshFor: o.reg.FreshMonths,
		Counts: map[string]int{
			"gems": len(b.gems), "anchors": len(b.anchors), "waters": len(b.waters), "fresh": len(fresh),
			"water_cells": b.grid.Count(raster.Water), "forest_cells": b.grid.Count(raster.Forest),
			"builtup_cells": b.grid.Count(raster.BuiltUp),
		},
	}
	o.log.Info("features", "counts", meta.Counts, "osm_date", meta.OSMDate)
	return features.WriteJSON(dir, features.MetaFile, meta)
}

// osmDate reads the replication timestamp from the extract header.
func osmDate(ctx context.Context, o *opts, pbf string) string {
	out, err := exec.CommandContext(ctx, osmiumBin(o), "fileinfo", "-g", "header.option.osmosis_replication_timestamp", pbf).Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

type builder struct {
	o       *opts
	grid    *raster.Grid
	gems    []features.Gem
	anchors []features.Anchor
	waters  []features.Water
	rivers  map[string]*features.Water // riverbank pieces grouped by name
	center  geo.Point
}

func newBuilder(o *opts) *builder {
	return &builder{o: o, grid: raster.New(o.reg.BBox(), cellM), rivers: map[string]*features.Water{}, center: o.reg.CenterPoint()}
}

func (b *builder) add(f *gjFeature) {
	polys := f.Geometry.polygons()
	lines := f.Geometry.lines()
	pt, isPoint := f.Geometry.point()

	// 1. Paint the land grid.
	var flag byte
	switch {
	case isWater(f):
		flag = raster.Water
	case f.tag("landuse") == "forest" || f.tag("natural") == "wood":
		flag = raster.Forest
	case in(f.tag("landuse"), "residential", "commercial", "industrial", "retail"):
		flag = raster.BuiltUp
	case in(f.tag("leisure"), "park", "garden") || in(f.tag("landuse"), "meadow", "grass", "orchard", "recreation_ground", "village_green") ||
		in(f.tag("natural"), "grassland", "heath", "scrub"):
		flag = raster.Green
	case f.tag("leisure") == "nature_reserve" || in(f.tag("boundary"), "protected_area", "national_park"):
		flag = raster.Protected
	case f.tag("landuse") == "farmland":
		flag = raster.Farmland
	}
	if flag != 0 {
		// River areas also get the River flag: in town a "river" is often a drain,
		// so scoring only treats lakes and reservoirs as scenic there.
		if flag == raster.Water && (f.tag("water") == "river" || f.tag("waterway") == "riverbank" || f.tag("water") == "canal") {
			flag |= raster.River
		}
		for _, p := range polys {
			b.grid.FillPolygon(p, flag)
		}
	}
	if f.tag("waterway") == "river" {
		for _, l := range lines {
			b.grid.DrawLine(l, raster.River, 1)
		}
	}

	name := displayName(f)

	// 2. Water bodies and shore anchors.
	if flag&raster.Water != 0 && len(polys) > 0 {
		b.addWater(f, name, polys)
	}
	if f.tag("waterway") == "river" {
		for _, l := range lines {
			for _, p := range sampleLine(l, 1500)[1:] {
				b.anchors = append(b.anchors, features.Anchor{Lat: p.Lat, Lng: p.Lng, Kind: "river", Name: name, W: 0.5 + named(name)})
			}
		}
	}
	// 3. Forest and protected-area edges.
	if flag == raster.Forest || flag == raster.Protected {
		for _, p := range polys {
			area := polygonAreaM2(p) / 1e6
			if area < 0.5 {
				continue
			}
			w := clamp(0.4+0.12*math.Log10(area*2), 0.35, 0.75) + named(name)
			if flag == raster.Protected {
				w += 0.1
			}
			s := sampleLine(p[0], 1500)
			if len(s) > 30 {
				s = thin(s, 30)
			}
			for _, q := range s {
				b.anchors = append(b.anchors, features.Anchor{Lat: q.Lat, Lng: q.Lng, Kind: "forest", Name: name, W: w})
			}
		}
	}
	// 4. Destinations.
	b.addGem(f, name, polys, lines, pt, isPoint)
}

func isWater(f *gjFeature) bool {
	if in(f.tag("water"), "wastewater", "swimming_pool", "pond_fish", "fountain") {
		return false
	}
	return f.tag("natural") == "water" || f.tag("waterway") == "riverbank" || in(f.tag("landuse"), "reservoir", "basin") ||
		(f.tag("water") != "" && f.tag("natural") == "")
}

func (b *builder) addWater(f *gjFeature, name string, polys [][][]geo.Point) {
	var area float64
	var outer []geo.Point
	for _, p := range polys {
		a := polygonAreaM2(p)
		area += a
		if outer == nil || a > ringAreaM2(outer) {
			outer = p[0]
		}
	}
	km2 := area / 1e6
	river := f.tag("water") == "river" || f.tag("waterway") == "riverbank"
	kind := "lake"
	if river {
		kind = "river"
	} else if f.tag("water") == "reservoir" || f.tag("landuse") == "reservoir" {
		kind = "reservoir"
	}
	// Shore anchors: lakes ≥ 0.15 km², river banks ≥ 0.05 km².
	if km2 >= 0.15 || (river && km2 >= 0.05) {
		step := 700.0
		if river {
			step = 1000
		}
		s := sampleLine(outer, step)
		if len(s) > 60 {
			s = thin(s, 60)
		}
		w := clamp(0.45+0.15*math.Log10(km2*10), 0.4, 0.9) + named(name)
		for _, p := range s {
			b.anchors = append(b.anchors, features.Anchor{Lat: p.Lat, Lng: p.Lng, Kind: "water", Name: name, W: w})
		}
	}
	all := []geo.Point{}
	for _, p := range polys {
		all = append(all, p[0]...)
	}
	bb := bboxOf(all)
	shore := closest(outer, b.center)
	if river && name != "" {
		w := b.rivers[name]
		if w == nil {
			w = &features.Water{ID: "river:" + name, Name: name, Kind: "river", BBox: bb}
			b.rivers[name] = w
		}
		if km2 > w.AreaKm2/2 { // label on the biggest piece
			c := centroid(outer)
			w.Lat, w.Lng = c.Lat, c.Lng
		}
		w.AreaKm2 += km2
		w.BBox = geo.BBox{math.Min(w.BBox[0], bb[0]), math.Min(w.BBox[1], bb[1]), math.Max(w.BBox[2], bb[2]), math.Max(w.BBox[3], bb[3])}
		if w.Shore == nil || geo.Haversine(shore, b.center) < geo.Haversine(*w.Shore, b.center) {
			w.Shore = &shore
		}
		return
	}
	if km2 < 0.1 || (name == "" && km2 < 0.3) {
		return
	}
	c := centroid(outer)
	if name == "" {
		name = "Unnamed " + kind
	}
	b.waters = append(b.waters, features.Water{ID: f.ID, Name: name, Kind: kind, AreaKm2: round(km2, 3), Lat: c.Lat, Lng: c.Lng, BBox: bb, Shore: &shore})
}

type gemRule struct {
	kind  string
	score float64
}

func gemKind(f *gjFeature, name string, areaKm2 float64) (gemRule, bool) {
	t := f.tag
	switch {
	case t("waterway") == "waterfall" || t("natural") == "waterfall":
		return gemRule{"waterfall", 0.9}, true
	case t("tourism") == "viewpoint":
		return gemRule{"viewpoint", 0.72}, true
	case in(t("historic"), "fort", "castle"):
		if t("castle_type") == "palace" || t("castle_type") == "manor" {
			return gemRule{"palace", 0.75}, name != ""
		}
		return gemRule{"fort", 0.85}, name != ""
	case t("historic") == "palace":
		return gemRule{"palace", 0.75}, name != ""
	case in(t("historic"), "ruins", "archaeological_site"):
		return gemRule{"ruins", 0.62}, name != ""
	case in(t("historic"), "monument", "city_gate", "memorial"):
		return gemRule{"monument", 0.42}, name != "" && t("wikidata") != ""
	case t("natural") == "peak":
		return gemRule{"peak", 0.6}, name != ""
	case t("natural") == "cave_entrance":
		return gemRule{"cave", 0.72}, true
	case t("natural") == "hot_spring":
		return gemRule{"spring", 0.7}, true
	case t("natural") == "cliff":
		return gemRule{"cliff", 0.55}, name != ""
	case t("waterway") == "dam":
		return gemRule{"dam", 0.62}, name != ""
	case isWater(f) && t("water") != "river" && t("waterway") != "riverbank":
		return gemRule{"lake", clamp(0.5+0.12*math.Log10(areaKm2*5), 0.5, 0.85)}, name != "" && areaKm2 >= 0.15
	case t("leisure") == "nature_reserve" || in(t("boundary"), "protected_area", "national_park"):
		return gemRule{"wild", 0.7}, name != ""
	case t("amenity") == "place_of_worship":
		return gemRule{"temple", 0.52}, name != "" && (t("wikidata") != "" || t("tourism") != "" || t("historic") != "")
	case t("tourism") == "attraction":
		return gemRule{"attraction", 0.55}, name != ""
	case t("tourism") == "picnic_site":
		return gemRule{"picnic", 0.45}, true
	case t("tourism") == "camp_site":
		return gemRule{"camp", 0.5}, true
	case in(t("leisure"), "park", "garden"):
		return gemRule{"park", 0.35}, name != "" && areaKm2 >= 0.05
	case t("natural") == "beach":
		return gemRule{"ghat", 0.5}, true
	}
	return gemRule{}, false
}

// businessName spots companies mis-tagged as attractions.
func businessName(name string) bool {
	n := " " + strings.ToLower(name) + " "
	for _, w := range []string{" pvt", " ltd", " limited", " p ltd", " llp", " company", " industries", " enterprises", " private "} {
		if strings.Contains(n, w) {
			return true
		}
	}
	return false
}

// refineByName fixes vague kinds from the name: OSM around Indore often tags a
// waterfall as a cliff or an attraction, a fort as a monument, and so on.
func refineByName(r gemRule, name string) gemRule {
	if !in(r.kind, "attraction", "cliff", "monument", "ruins", "park", "picnic") {
		return r
	}
	n := strings.ToLower(name)
	has := func(words ...string) bool {
		for _, w := range words {
			if strings.Contains(n, w) {
				return true
			}
		}
		return false
	}
	switch {
	case has("fall", "prapat", "kund"):
		return gemRule{"waterfall", math.Max(r.score, 0.85)}
	case has("fort", "kila", "qila", "garh", "mahal"):
		return gemRule{"fort", math.Max(r.score, 0.75)}
	case has("lake", "talab", "talav", "sagar", "tank", "dam"):
		return gemRule{"lake", math.Max(r.score, 0.62)}
	case has("temple", "mandir", "mata", "devi", "mahadev", "dham"):
		return gemRule{"temple", math.Max(r.score, 0.55)}
	case has("point", "view", "tekri", "hill", "pahadi"):
		return gemRule{"viewpoint", math.Max(r.score, 0.65)}
	case has("ghat"):
		return gemRule{"ghat", math.Max(r.score, 0.55)}
	}
	return r
}

func (b *builder) addGem(f *gjFeature, name string, polys [][][]geo.Point, lines [][]geo.Point, pt geo.Point, isPoint bool) {
	var area float64
	var outer []geo.Point
	for _, p := range polys {
		a := polygonAreaM2(p)
		area += a
		if outer == nil || a > ringAreaM2(outer) {
			outer = p[0]
		}
	}
	km2 := area / 1e6
	rule, ok := gemKind(f, name, km2)
	if !ok || businessName(name) {
		return
	}
	rule = refineByName(rule, name)
	// Ways are exported both as lines and (when closed) polygons; keep one.
	if f.Geometry.Type == "LineString" && strings.HasPrefix(f.ID, "w") && len(lines) > 0 && closedLine(lines[0]) && rule.kind != "dam" {
		return
	}
	g := features.Gem{ID: f.ID, Name: name, NameHi: hindiName(f), Kind: rule.kind, Score: rule.score, Wiki: f.tag("wikidata") != "" || f.tag("wikipedia") != ""}
	switch {
	case isPoint:
		g.Lat, g.Lng = pt.Lat, pt.Lng
	case outer != nil:
		c := centroid(outer)
		g.Lat, g.Lng = c.Lat, c.Lng
		acc := closest(outer, b.center)
		g.Access = &acc
		g.AreaKm2 = round(km2, 3)
	case len(lines) > 0:
		l := lines[0]
		mid := l[len(l)/2]
		g.Lat, g.Lng = mid.Lat, mid.Lng
	default:
		return
	}
	if !b.o.reg.Contains(g.Point()) {
		return
	}
	if e, err := strconv.ParseFloat(strings.TrimSuffix(strings.TrimSpace(f.tag("ele")), " m"), 64); err == nil {
		g.EleM = e
	}
	if g.Name == "" {
		g.Name = defaultName(rule.kind)
		g.Score -= 0.15
	}
	if g.Wiki {
		g.Score += 0.12
	}
	if fe, ok := b.o.reg.MatchFeatured(name + " " + f.tag("name:en") + " " + f.tag("alt_name")); ok && name != "" {
		g.Featured = true
		g.Score += 0.2
		_ = fe
	}
	g.Score = round(clamp(g.Score, 0.05, 1), 3)
	b.gems = append(b.gems, g)

	// Point-like gems double as strong scenic anchors.
	akind := map[string]string{"waterfall": "waterfall", "viewpoint": "viewpoint", "peak": "peak", "fort": "heritage",
		"palace": "heritage", "ruins": "heritage", "dam": "water", "cliff": "viewpoint", "cave": "heritage", "ghat": "water"}[rule.kind]
	if akind != "" {
		p := g.Target()
		b.anchors = append(b.anchors, features.Anchor{Lat: p.Lat, Lng: p.Lng, Kind: akind, Name: g.Name, W: clamp(g.Score, 0.4, 1)})
	}
}

func (b *builder) finish() {
	b.grid.Dilate(raster.Water|raster.River, raster.NearWater, int(math.Round(200/cellM)))
	for _, w := range b.rivers {
		if w.AreaKm2 >= 0.2 {
			w.AreaKm2 = round(w.AreaKm2, 3)
			b.waters = append(b.waters, *w)
		}
	}
	b.waters = nameAndMergeWaters(b.waters, b.anchors, b.gems, b.center)
	sort.Slice(b.waters, func(i, j int) bool { return b.waters[i].AreaKm2 > b.waters[j].AreaKm2 })
	b.gems = dedupeGems(b.gems)
	sort.Slice(b.gems, func(i, j int) bool { return b.gems[i].Score > b.gems[j].Score })
	for i := range b.anchors {
		b.anchors[i].Lat, b.anchors[i].Lng = round(b.anchors[i].Lat, 6), round(b.anchors[i].Lng, 6)
		b.anchors[i].W = round(clamp(b.anchors[i].W, 0, 1), 3)
	}
}

// nameAndMergeWaters names unnamed water bodies after the nearest named river,
// lake or dam (OSM riverbank polygons are rarely named; the river line is), then
// merges same-named pieces that touch (a reservoir split into several polygons).
func nameAndMergeWaters(ws []features.Water, anchors []features.Anchor, gems []features.Gem, center geo.Point) []features.Water {
	var named []features.Anchor
	for _, a := range anchors {
		if a.Name != "" && (a.Kind == "water" || a.Kind == "river") {
			named = append(named, a)
		}
	}
	for _, g := range gems {
		if in(g.Kind, "dam", "lake") && g.Name != "" {
			t := g.Target()
			named = append(named, features.Anchor{Lat: t.Lat, Lng: t.Lng, Kind: "water", Name: g.Name})
		}
	}
	idx := features.NewBucket(named, 0.02, features.Anchor.Point)
	for i := range ws {
		w := &ws[i]
		if !strings.HasPrefix(w.Name, "Unnamed") {
			continue
		}
		probes := []geo.Point{{Lat: w.Lat, Lng: w.Lng}}
		if w.Shore != nil {
			probes = append(probes, *w.Shore)
		}
		for _, p := range probes {
			found := false
			for _, a := range idx.Near(p, 3000) {
				if w.Kind == "river" && a.Kind != "river" {
					continue
				}
				w.Name, found = a.Name, true
				break
			}
			if found {
				break
			}
		}
	}
	// Merge same-named pieces whose boxes are within ~2 km.
	sort.Slice(ws, func(i, j int) bool { return ws[i].AreaKm2 > ws[j].AreaKm2 })
	var out []features.Water
	pad := 0.02
	for _, w := range ws {
		merged := false
		for k := range out {
			o := &out[k]
			if waterKey(o.Name) != waterKey(w.Name) || strings.HasPrefix(w.Name, "Unnamed") {
				continue
			}
			if w.BBox[0] > o.BBox[2]+pad || w.BBox[2] < o.BBox[0]-pad || w.BBox[1] > o.BBox[3]+pad || w.BBox[3] < o.BBox[1]-pad {
				continue
			}
			o.AreaKm2 = round(o.AreaKm2+w.AreaKm2, 3)
			o.BBox = geo.BBox{math.Min(o.BBox[0], w.BBox[0]), math.Min(o.BBox[1], w.BBox[1]), math.Max(o.BBox[2], w.BBox[2]), math.Max(o.BBox[3], w.BBox[3])}
			if w.Shore != nil && (o.Shore == nil || geo.Haversine(*w.Shore, center) < geo.Haversine(*o.Shore, center)) {
				o.Shore = w.Shore
			}
			merged = true
			break
		}
		if !merged {
			out = append(out, w)
		}
	}
	return out
}

// waterKey normalises "Narmada River" and "Narmada" to the same key.
func waterKey(name string) string {
	n := strings.ToLower(strings.TrimSpace(name))
	for _, suf := range []string{" river", " nadi", " reservoir", " lake", " talab", " talao"} {
		n = strings.TrimSuffix(n, suf)
	}
	return n
}

// dedupeGems keeps the best of same-named gems within 1.5 km of each other.
func dedupeGems(gs []features.Gem) []features.Gem {
	sort.Slice(gs, func(i, j int) bool { return gs[i].Score > gs[j].Score })
	var out []features.Gem
	for _, g := range gs {
		dup := false
		for _, k := range out {
			if strings.EqualFold(k.Name, g.Name) && geo.Haversine(k.Point(), g.Point()) < 1500 {
				dup = true
				break
			}
		}
		if !dup {
			out = append(out, g)
		}
	}
	return out
}

// freshWays exports routable ways first created within the region's fresh window.
func freshWays(ctx context.Context, o *opts) ([]features.Fresh, error) {
	hwPBF := filepath.Join(tmpDir(o), "highways.osm.pbf")
	hwSeq := filepath.Join(tmpDir(o), "highways.geojsonseq")
	if err := sh(ctx, o, osmiumBin(o), "tags-filter", "--overwrite", "-o", hwPBF, extractPath(o), "w/highway="+strings.Join(freshHighways, ",")); err != nil {
		return nil, err
	}
	if err := sh(ctx, o, osmiumBin(o), "export", "--overwrite", "-f", "geojsonseq", "--geometry-types=linestring",
		"-a", "id,version,timestamp", "-o", hwSeq, hwPBF); err != nil {
		return nil, err
	}
	cutoff := time.Now().UTC().AddDate(0, -o.reg.FreshMonths, 0)
	var out []features.Fresh
	scanned, recent := 0, 0
	err := readSeq(hwSeq, func(f *gjFeature) {
		scanned++
		sec, err := strconv.ParseInt(f.tag("@timestamp"), 10, 64)
		if err != nil {
			return
		}
		ts := time.Unix(sec, 0).UTC()
		if ts.Before(cutoff) {
			return
		}
		recent++
		// Version 1 means OSM first saw this way at that timestamp: genuinely new on the map.
		if f.tag("@version") != "1" {
			return
		}
		id, err := strconv.ParseInt(f.tag("@id"), 10, 64)
		if err != nil {
			return
		}
		for _, l := range f.Geometry.lines() {
			n := geo.Length(l)
			if n < 60 {
				continue
			}
			s := geo.Simplify(l, 4)
			line := make([][2]float64, len(s))
			for i, p := range s {
				line[i] = [2]float64{round(p.Lng, 6), round(p.Lat, 6)}
			}
			out = append(out, features.Fresh{ID: id, Highway: f.tag("highway"), Month: ts.Format("2006-01"), Name: displayName(f), LenM: math.Round(n), Line: line})
		}
	})
	o.log.Info("fresh ways", "highways", scanned, "edited_recently", recent, "new", len(out), "since", cutoff.Format("2006-01"))
	return out, err
}

// --- small helpers -------------------------------------------------------------------

func in(v string, set ...string) bool {
	for _, s := range set {
		if v == s {
			return true
		}
	}
	return false
}

func clamp(v, lo, hi float64) float64 { return math.Max(lo, math.Min(hi, v)) }

func round(v float64, d int) float64 {
	p := math.Pow(10, float64(d))
	return math.Round(v*p) / p
}

func named(name string) float64 {
	if name != "" {
		return 0.08
	}
	return 0
}

func closest(ring []geo.Point, to geo.Point) geo.Point {
	best, bd := geo.Point{}, math.Inf(1)
	for _, p := range ring {
		if d := geo.Haversine(p, to); d < bd {
			best, bd = p, d
		}
	}
	return geo.Point{Lat: round(best.Lat, 6), Lng: round(best.Lng, 6)}
}

func closedLine(l []geo.Point) bool {
	return len(l) > 3 && l[0] == l[len(l)-1]
}

// thin keeps n roughly evenly spaced points.
func thin(pts []geo.Point, n int) []geo.Point {
	out := make([]geo.Point, 0, n)
	for i := 0; i < n; i++ {
		out = append(out, pts[i*len(pts)/n])
	}
	return out
}

func isDevanagari(s string) bool {
	for _, r := range s {
		if unicode.Is(unicode.Devanagari, r) {
			return true
		}
	}
	return false
}

// displayName prefers an English/Latin name, as the UI is English-first.
func displayName(f *gjFeature) string {
	if n := f.tag("name:en"); n != "" {
		return n
	}
	n := f.tag("name")
	if isDevanagari(n) {
		if alt := f.tag("int_name"); alt != "" {
			return alt
		}
	}
	return n
}

func hindiName(f *gjFeature) string {
	if n := f.tag("name:hi"); n != "" {
		return n
	}
	if n := f.tag("name"); isDevanagari(n) {
		return n
	}
	return ""
}

func defaultName(kind string) string {
	switch kind {
	case "viewpoint":
		return "Viewpoint"
	case "waterfall":
		return "Waterfall"
	case "cave":
		return "Cave"
	case "picnic":
		return "Picnic spot"
	case "camp":
		return "Campsite"
	case "spring":
		return "Hot spring"
	case "ghat":
		return "Riverside beach"
	}
	return strings.ToUpper(kind[:1]) + kind[1:]
}

var _ = fmt.Sprint
