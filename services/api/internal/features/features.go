// Package features holds the point and line layers datactl extracts from
// OpenStreetMap and the API queries at runtime:
//
//   - Gems: places worth riding to (waterfalls, viewpoints, forts, lakes …).
//   - Anchors: scenic via-points (lake shores, river banks, forest edges,
//     viewpoints) used to bend a route towards something beautiful.
//   - Waters: named water bodies (jetski areas, lakeside rides).
//   - Fresh: roads and tracks first added to OSM in the last N months.
package features

import (
	"encoding/json"
	"math"
	"os"
	"path/filepath"
	"sort"

	"pagdandi/api/internal/geo"
)

// Gem is a destination.
type Gem struct {
	ID       string     `json:"id"`
	Name     string     `json:"name"`
	NameHi   string     `json:"name_hi,omitempty"`
	Kind     string     `json:"kind"`
	Lat      float64    `json:"lat"`
	Lng      float64    `json:"lng"`
	Access   *geo.Point `json:"access,omitempty"` // where to route to, if not the label point
	Score    float64    `json:"score"`
	Featured bool       `json:"featured,omitempty"`
	Wiki     bool       `json:"wiki,omitempty"`
	AreaKm2  float64    `json:"area_km2,omitempty"`
	EleM     float64    `json:"ele_m,omitempty"`
}

// Target is the point to route to.
func (g Gem) Target() geo.Point {
	if g.Access != nil {
		return *g.Access
	}
	return geo.Point{Lat: g.Lat, Lng: g.Lng}
}

// Point is the label point.
func (g Gem) Point() geo.Point { return geo.Point{Lat: g.Lat, Lng: g.Lng} }

// Anchor is a scenic via-point.
type Anchor struct {
	Lat  float64 `json:"lat"`
	Lng  float64 `json:"lng"`
	Kind string  `json:"kind"` // water | river | forest | viewpoint | peak | waterfall | heritage | green
	Name string  `json:"name,omitempty"`
	W    float64 `json:"w"` // 0..1 how special
}

// Point returns the anchor location.
func (a Anchor) Point() geo.Point { return geo.Point{Lat: a.Lat, Lng: a.Lng} }

// Water is a named water body.
type Water struct {
	ID      string     `json:"id"`
	Name    string     `json:"name"`
	Kind    string     `json:"kind"` // lake | reservoir | river
	AreaKm2 float64    `json:"area_km2"`
	Lat     float64    `json:"lat"`
	Lng     float64    `json:"lng"`
	BBox    geo.BBox   `json:"bbox"`
	Shore   *geo.Point `json:"shore,omitempty"` // likely launch side (closest to the city)
}

// Fresh is a newly mapped way.
type Fresh struct {
	ID      int64        `json:"id"`
	Highway string       `json:"highway"`
	Month   string       `json:"month"` // YYYY-MM first mapped
	Name    string       `json:"name,omitempty"`
	LenM    float64      `json:"len_m"`
	Line    [][2]float64 `json:"line"` // [lng, lat]
}

// Mid returns the point halfway along the way.
func (f Fresh) Mid() geo.Point {
	pts := f.Points()
	if len(pts) == 0 {
		return geo.Point{}
	}
	cut := geo.SliceAlong(pts, 0, f.LenM/2)
	if len(cut) == 0 {
		return pts[len(pts)/2]
	}
	return cut[len(cut)-1]
}

// Points converts the line to geo points.
func (f Fresh) Points() []geo.Point {
	out := make([]geo.Point, len(f.Line))
	for i, c := range f.Line {
		out[i] = geo.Point{Lat: c[1], Lng: c[0]}
	}
	return out
}

// Meta describes a feature build.
type Meta struct {
	BuiltAt  string         `json:"built_at"`
	OSMDate  string         `json:"osm_date,omitempty"`
	Counts   map[string]int `json:"counts"`
	FreshFor int            `json:"fresh_months"`
}

// Files are the file names inside the features directory.
const (
	GemsFile    = "gems.json"
	AnchorsFile = "anchors.json"
	WatersFile  = "waters.json"
	FreshFile   = "fresh.json"
	RasterFile  = "land.grid.gz"
	MetaFile    = "meta.json"
)

// WriteJSON writes v to dir/name atomically.
func WriteJSON(dir, name string, v any) error {
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p+".tmp", b, 0o644); err != nil {
		return err
	}
	return os.Rename(p+".tmp", p)
}

// ReadJSON reads dir/name into v.
func ReadJSON(dir, name string, v any) error {
	b, err := os.ReadFile(filepath.Join(dir, name))
	if err != nil {
		return err
	}
	return json.Unmarshal(b, v)
}

// Bucket is a coarse spatial hash over items with a position.
type Bucket[T any] struct {
	cell  float64
	cells map[[2]int][]int
	items []T
	pos   func(T) geo.Point
}

// NewBucket indexes items in cells of cellDeg degrees.
func NewBucket[T any](items []T, cellDeg float64, pos func(T) geo.Point) *Bucket[T] {
	b := &Bucket[T]{cell: cellDeg, cells: map[[2]int][]int{}, items: items, pos: pos}
	for i, it := range items {
		k := b.key(pos(it))
		b.cells[k] = append(b.cells[k], i)
	}
	return b
}

func (b *Bucket[T]) key(p geo.Point) [2]int {
	return [2]int{int(math.Floor(p.Lng / b.cell)), int(math.Floor(p.Lat / b.cell))}
}

// Items returns every indexed item.
func (b *Bucket[T]) Items() []T { return b.items }

// Near returns items within radiusM of p, closest first.
func (b *Bucket[T]) Near(p geo.Point, radiusM float64) []T {
	type hit struct {
		i int
		d float64
	}
	var hits []hit
	b.visit(p, radiusM, func(i int) {
		if d := geo.Haversine(p, b.pos(b.items[i])); d <= radiusM {
			hits = append(hits, hit{i, d})
		}
	})
	sort.Slice(hits, func(a, c int) bool { return hits[a].d < hits[c].d })
	out := make([]T, len(hits))
	for k, h := range hits {
		out[k] = b.items[h.i]
	}
	return out
}

// InBBox returns items inside bb.
func (b *Bucket[T]) InBBox(bb geo.BBox) []T {
	var out []T
	x0, y0 := int(math.Floor(bb[0]/b.cell)), int(math.Floor(bb[1]/b.cell))
	x1, y1 := int(math.Floor(bb[2]/b.cell)), int(math.Floor(bb[3]/b.cell))
	if (x1-x0+1)*(y1-y0+1) > 40000 {
		for _, it := range b.items {
			if bb.Contains(b.pos(it)) {
				out = append(out, it)
			}
		}
		return out
	}
	for y := y0; y <= y1; y++ {
		for x := x0; x <= x1; x++ {
			for _, i := range b.cells[[2]int{x, y}] {
				if bb.Contains(b.pos(b.items[i])) {
					out = append(out, b.items[i])
				}
			}
		}
	}
	return out
}

func (b *Bucket[T]) visit(p geo.Point, radiusM float64, fn func(i int)) {
	dLat := radiusM / 111320.0
	dLng := radiusM / (111320.0 * math.Max(0.1, math.Cos(p.Lat*math.Pi/180)))
	bb := geo.BBox{p.Lng - dLng, p.Lat - dLat, p.Lng + dLng, p.Lat + dLat}
	x0, y0 := int(math.Floor(bb[0]/b.cell)), int(math.Floor(bb[1]/b.cell))
	x1, y1 := int(math.Floor(bb[2]/b.cell)), int(math.Floor(bb[3]/b.cell))
	for y := y0; y <= y1; y++ {
		for x := x0; x <= x1; x++ {
			for _, i := range b.cells[[2]int{x, y}] {
				fn(i)
			}
		}
	}
}
