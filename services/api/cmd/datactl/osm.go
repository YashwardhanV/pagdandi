package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"pagdandi/api/internal/geo"
)

func osmDir(o *opts) string       { return filepath.Join(o.build, "osm") }
func tmpDir(o *opts) string       { return filepath.Join(o.build, "osm", "tmp") }
func coveragePath(o *opts) string { return filepath.Join(o.build, "coverage.geojson") }
func extractPath(o *opts) string  { return filepath.Join(osmDir(o), o.reg.ID+".osm.pbf") }
func modelsDir(o *opts) string    { return filepath.Join(o.build, "gh", "models") }
func featuresDir(o *opts) string  { return filepath.Join(o.build, "features") }
func sourcePath(o *opts, name string) string {
	return filepath.Join(osmDir(o), "src", name+"-latest.osm.pbf")
}

// cmdCoverage writes the coverage circle as a GeoJSON Feature (osmium and pmtiles read it).
func cmdCoverage(o *opts) error {
	ring := o.reg.Ring()
	f := map[string]any{
		"type":       "Feature",
		"properties": map[string]any{"id": o.reg.ID, "radius_km": o.reg.RadiusKm},
		"geometry":   map[string]any{"type": "Polygon", "coordinates": [][][2]float64{ring}},
	}
	b, _ := json.Marshal(f)
	if old, err := os.ReadFile(coveragePath(o)); err == nil && bytes.Equal(old, b) {
		o.log.Info("cached", "file", "coverage.geojson")
		return nil
	}
	o.log.Info("coverage", "region", o.reg.ID, "radius_km", o.reg.RadiusKm)
	return os.WriteFile(coveragePath(o), b, 0o644)
}

func cmdDownload(ctx context.Context, o *opts) error {
	for _, s := range o.reg.OSMSources {
		dst := sourcePath(o, s.Name)
		if err := download(ctx, o, s.URL, dst); err != nil {
			if s.Mirror == "" {
				return err
			}
			o.log.Warn("primary download failed; trying mirror", "src", s.Name, "err", err)
			if err := download(ctx, o, s.Mirror, dst); err != nil {
				return fmt.Errorf("all mirrors failed for %s: %w", s.Name, err)
			}
		}
	}
	return nil
}

func cmdExtract(ctx context.Context, o *opts) error {
	if _, err := os.Stat(coveragePath(o)); err != nil {
		return fmt.Errorf("run `datactl coverage` first")
	}
	inputs := []string{coveragePath(o)}
	for _, s := range o.reg.OSMSources {
		inputs = append(inputs, sourcePath(o, s.Name))
	}
	out := extractPath(o)
	if !o.force && fresh(out, inputs...) {
		o.log.Info("cached", "file", filepath.Base(out))
		return nil
	}
	_ = os.MkdirAll(tmpDir(o), 0o755)
	var cuts []string
	for _, s := range o.reg.OSMSources {
		cut := filepath.Join(tmpDir(o), s.Name+".cut.osm.pbf")
		// complete_ways keeps every node of a way that crosses the circle, so roads
		// at the rim stay routable; timestamps/versions are kept for "fresh" roads.
		if err := sh(ctx, o, osmiumBin(o), "extract", "-p", coveragePath(o), "-s", "complete_ways", "--overwrite", "-o", cut, sourcePath(o, s.Name)); err != nil {
			return err
		}
		cuts = append(cuts, cut)
	}
	if len(cuts) == 1 {
		_ = os.Remove(out)
		return os.Rename(cuts[0], out)
	}
	args := append([]string{"merge", "--overwrite", "-o", out}, cuts...)
	if err := sh(ctx, o, osmiumBin(o), args...); err != nil {
		return err
	}
	for _, c := range cuts {
		_ = os.Remove(c)
	}
	return nil
}

// --- GeoJSON sequence reading -----------------------------------------------------

// gjFeature is one feature from `osmium export -f geojsonseq`.
type gjFeature struct {
	ID         string            `json:"id"`
	Properties map[string]any    `json:"properties"`
	Geometry   gjGeometry        `json:"geometry"`
	tags       map[string]string // string-valued properties
}

type gjGeometry struct {
	Type        string          `json:"type"`
	Coordinates json.RawMessage `json:"coordinates"`
}

func (f *gjFeature) tag(k string) string { return f.tags[k] }

// polygons returns the geometry as polygons (each a list of rings).
func (g gjGeometry) polygons() [][][]geo.Point {
	switch g.Type {
	case "Polygon":
		var c [][][2]float64
		if json.Unmarshal(g.Coordinates, &c) != nil {
			return nil
		}
		return [][][]geo.Point{ringsOf(c)}
	case "MultiPolygon":
		var c [][][][2]float64
		if json.Unmarshal(g.Coordinates, &c) != nil {
			return nil
		}
		out := make([][][]geo.Point, 0, len(c))
		for _, p := range c {
			out = append(out, ringsOf(p))
		}
		return out
	}
	return nil
}

// lines returns the geometry as polylines.
func (g gjGeometry) lines() [][]geo.Point {
	switch g.Type {
	case "LineString":
		var c [][2]float64
		if json.Unmarshal(g.Coordinates, &c) != nil {
			return nil
		}
		return [][]geo.Point{pointsOf(c)}
	case "MultiLineString":
		var c [][][2]float64
		if json.Unmarshal(g.Coordinates, &c) != nil {
			return nil
		}
		var out [][]geo.Point
		for _, l := range c {
			out = append(out, pointsOf(l))
		}
		return out
	}
	return nil
}

// point returns a Point geometry.
func (g gjGeometry) point() (geo.Point, bool) {
	if g.Type != "Point" {
		return geo.Point{}, false
	}
	var c [2]float64
	if json.Unmarshal(g.Coordinates, &c) != nil {
		return geo.Point{}, false
	}
	return geo.Point{Lat: c[1], Lng: c[0]}, true
}

func pointsOf(c [][2]float64) []geo.Point {
	out := make([]geo.Point, len(c))
	for i, p := range c {
		out[i] = geo.Point{Lat: p[1], Lng: p[0]}
	}
	return out
}

func ringsOf(c [][][2]float64) [][]geo.Point {
	out := make([][]geo.Point, len(c))
	for i, r := range c {
		out[i] = pointsOf(r)
	}
	return out
}

// readSeq streams a GeoJSON text sequence (RFC 8142) or newline-delimited GeoJSON.
func readSeq(path string, fn func(*gjFeature)) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1<<20), 512<<20)
	for sc.Scan() {
		line := bytes.TrimLeft(sc.Bytes(), "\x1e \t")
		if len(line) == 0 {
			continue
		}
		var ft gjFeature
		if err := json.Unmarshal(line, &ft); err != nil {
			continue
		}
		ft.tags = make(map[string]string, len(ft.Properties))
		for k, v := range ft.Properties {
			switch t := v.(type) {
			case string:
				ft.tags[k] = t
			case float64:
				ft.tags[k] = strconv.FormatFloat(t, 'f', -1, 64)
			}
		}
		fn(&ft)
	}
	return sc.Err()
}

// --- geometry helpers --------------------------------------------------------------

// ringAreaM2 is the planar area of a ring (equirectangular, fine at city scale).
func ringAreaM2(r []geo.Point) float64 {
	if len(r) < 4 {
		return 0
	}
	lat0 := r[0].Lat * math.Pi / 180
	kx := 111320.0 * math.Cos(lat0)
	ky := 110540.0
	var s float64
	for i := 0; i+1 < len(r); i++ {
		s += (r[i].Lng*kx)*(r[i+1].Lat*ky) - (r[i+1].Lng*kx)*(r[i].Lat*ky)
	}
	return math.Abs(s) / 2
}

// polygonAreaM2 is the outer ring minus holes.
func polygonAreaM2(p [][]geo.Point) float64 {
	if len(p) == 0 {
		return 0
	}
	a := ringAreaM2(p[0])
	for _, h := range p[1:] {
		a -= ringAreaM2(h)
	}
	return math.Max(0, a)
}

// centroid is the vertex average of the outer ring (good enough for a label).
func centroid(r []geo.Point) geo.Point {
	var c geo.Point
	n := 0
	for i := 0; i+1 < len(r); i++ {
		c.Lat += r[i].Lat
		c.Lng += r[i].Lng
		n++
	}
	if n == 0 {
		return geo.Point{}
	}
	return geo.Point{Lat: c.Lat / float64(n), Lng: c.Lng / float64(n)}
}

func bboxOf(pts []geo.Point) geo.BBox {
	b := geo.BBox{math.Inf(1), math.Inf(1), math.Inf(-1), math.Inf(-1)}
	for _, p := range pts {
		b[0], b[1] = math.Min(b[0], p.Lng), math.Min(b[1], p.Lat)
		b[2], b[3] = math.Max(b[2], p.Lng), math.Max(b[3], p.Lat)
	}
	return b
}

// sampleLine returns points every stepM metres along a line (including the start).
func sampleLine(line []geo.Point, stepM float64) []geo.Point {
	if len(line) == 0 {
		return nil
	}
	out := []geo.Point{line[0]}
	next, acc := stepM, 0.0
	for i := 0; i+1 < len(line); i++ {
		a, b := line[i], line[i+1]
		seg := geo.Haversine(a, b)
		for seg > 0 && next <= acc+seg {
			t := (next - acc) / seg
			out = append(out, geo.Point{Lat: a.Lat + (b.Lat-a.Lat)*t, Lng: a.Lng + (b.Lng-a.Lng)*t})
			next += stepM
		}
		acc += seg
	}
	return out
}

// --- military areas ------------------------------------------------------------------

// cmdAreas turns military polygons into a GraphHopper custom-model file that
// every profile loads, so no route ever enters a cantonment or firing range.
func cmdAreas(ctx context.Context, o *opts) error {
	in := extractPath(o)
	out := filepath.Join(modelsDir(o), "military.json")
	if !o.force && fresh(out, in) {
		o.log.Info("cached", "file", "military.json")
		return nil
	}
	_ = os.MkdirAll(tmpDir(o), 0o755)
	mil := filepath.Join(tmpDir(o), "military.osm.pbf")
	gj := filepath.Join(tmpDir(o), "military.geojsonseq")
	if err := sh(ctx, o, osmiumBin(o), "tags-filter", "--overwrite", "-o", mil, in, "wr/landuse=military", "wr/military"); err != nil {
		return err
	}
	if err := sh(ctx, o, osmiumBin(o), "export", "--overwrite", "-f", "geojsonseq", "--geometry-types=polygon", "-o", gj, mil); err != nil {
		return err
	}
	type poly struct {
		ring []geo.Point
		area float64
	}
	var polys []poly
	err := readSeq(gj, func(f *gjFeature) {
		// Military-tagged areas that are clearly civilian access (e.g. a public
		// museum) are rare; we stay conservative and block them all.
		for _, p := range f.Geometry.polygons() {
			if len(p) == 0 {
				continue
			}
			ring := geo.Simplify(p[0], 8)
			if len(ring) < 4 {
				continue
			}
			polys = append(polys, poly{ring, ringAreaM2(ring)})
		}
	})
	if err != nil {
		return err
	}
	sort.Slice(polys, func(i, j int) bool { return polys[i].area > polys[j].area })
	if len(polys) > 300 {
		polys = polys[:300]
	}
	model := map[string]any{"priority": []any{}}
	if len(polys) > 0 {
		var feats []map[string]any
		var conds []string
		for i, p := range polys {
			id := fmt.Sprintf("military_%d", i)
			coords := make([][2]float64, len(p.ring))
			for k, pt := range p.ring {
				coords[k] = [2]float64{math.Round(pt.Lng*1e6) / 1e6, math.Round(pt.Lat*1e6) / 1e6}
			}
			feats = append(feats, map[string]any{"type": "Feature", "id": id, "properties": map[string]any{},
				"geometry": map[string]any{"type": "Polygon", "coordinates": [][][2]float64{coords}}})
			conds = append(conds, "in_"+id)
		}
		model = map[string]any{
			"priority": []map[string]string{{"if": strings.Join(conds, " || "), "multiply_by": "0"}},
			"areas":    map[string]any{"type": "FeatureCollection", "features": feats},
		}
	}
	o.log.Info("military areas", "polygons", len(polys))
	return writeJSON(out, model)
}
