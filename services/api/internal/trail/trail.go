// Package trail is the route Pagdandi shows: geometry plus everything a rider
// wants to know about it (surface, how quiet, how scenic, what's new, warnings).
package trail

import (
	"math"

	"pagdandi/api/internal/geo"
)

// Kinds of trail. Each has a name, a colour in the UI and a reason to exist.
const (
	Usual    = "usual"    // main roads, the reference
	Shortest = "shortest" // fewest kilometres, whatever the surface
	Kaccha   = "kaccha"   // seeks dirt, tracks and village roads
	Quiet    = "quiet"    // avoids busy roads: the lesser-taken way
	Scenic   = "scenic"   // bends towards water, forest, viewpoints
	Fresh    = "fresh"    // goes through roads new on the map
	Loop     = "loop"     // Wander: out and back a different way
	Open     = "open"     // jetski across open water
	Shore    = "shore"    // jetski along the shoreline
)

// Names are the display names (English, Hindi).
var Names = map[string][2]string{
	Usual:    {"The Usual", "हमेशा वाला"},
	Shortest: {"Shortcut", "छोटा रास्ता"},
	Kaccha:   {"Kaccha", "कच्चा रास्ता"},
	Quiet:    {"Quiet Lanes", "सुनसान गलियाँ"},
	Scenic:   {"Scenic", "नज़ारा"},
	Fresh:    {"New Ground", "नई ज़मीन"},
	Loop:     {"Loop", "चक्कर"},
	Open:     {"Open Water", "खुला पानी"},
	Shore:    {"Shoreline", "किनारा"},
}

// Span is a stretch of the route between two distances (metres from start).
type Span struct {
	Kind  string  `json:"kind"`
	From  float64 `json:"from_m"`
	To    float64 `json:"to_m"`
	Label string  `json:"label,omitempty"`
}

// Note is a warning or a tip.
type Note struct {
	Kind string `json:"kind"` // unpaved | monsoon | dark | steep | rough | water | legal | info
	Text string `json:"text"`
}

// Step is one manoeuvre.
type Step struct {
	Text      string     `json:"text"`
	Street    string     `json:"street,omitempty"`
	Sign      int        `json:"sign"`
	DistanceM float64    `json:"distance_m"`
	DurationS float64    `json:"duration_s"`
	At        [2]float64 `json:"at"` // [lng, lat]
	Index     int        `json:"index"`
	AlongM    float64    `json:"along_m"`
}

// Via is a point the trail deliberately passes.
type Via struct {
	Name string  `json:"name"`
	Kind string  `json:"kind"`
	Lat  float64 `json:"lat"`
	Lng  float64 `json:"lng"`
}

// Trail is one route option.
type Trail struct {
	ID         string             `json:"id"`
	Kind       string             `json:"kind"`
	Also       []string           `json:"also,omitempty"` // other kinds this route also satisfies
	Name       string             `json:"name"`
	NameHi     string             `json:"name_hi"`
	Tagline    string             `json:"tagline"`
	DistanceM  float64            `json:"distance_m"`
	DurationS  float64            `json:"duration_s"`
	DeltaM     float64            `json:"delta_m"` // vs the usual (negative = shorter)
	DeltaS     float64            `json:"delta_s"`
	ClimbM     float64            `json:"climb_m"`
	DescentM   float64            `json:"descent_m"`
	Geometry   [][2]float64       `json:"geometry"`    // [lng, lat]
	Elevation  [][2]float64       `json:"elevation"`   // [along_m, ele_m], ~80 samples
	Surfaces   []Span             `json:"surfaces"`    // paved | rough | dirt | water
	Mix        map[string]float64 `json:"mix"`         // share of each surface 0..1
	Quiet      float64            `json:"quiet"`       // 0..1, 1 = nobody else on it
	ScenicS    float64            `json:"scenic"`      // 0..1
	Twisty     float64            `json:"twisty"`      // 0..1
	BackroadsM float64            `json:"backroads_m"` // village roads, tracks and paths outside towns
	FreshM     float64            `json:"fresh_m"`
	FreshSince string             `json:"fresh_since,omitempty"` // earliest YYYY-MM of fresh parts
	FreshSpans []Span             `json:"fresh_spans,omitempty"`
	Highlights []Span             `json:"highlights,omitempty"` // water | forest | wild | view | hills | twisty
	Warnings   []Note             `json:"warnings,omitempty"`
	Steps      []Step             `json:"steps,omitempty"`
	Via        []Via              `json:"via,omitempty"`
	BBox       geo.BBox           `json:"bbox"`

	line []geo.Point // full-precision geometry for scoring/dedupe
}

// Line returns the geometry as points.
func (t *Trail) Line() []geo.Point {
	if t.line == nil {
		t.line = make([]geo.Point, len(t.Geometry))
		for i, c := range t.Geometry {
			t.line[i] = geo.Point{Lat: c[1], Lng: c[0]}
		}
	}
	return t.line
}

// SetLine stores geometry (rounded to ~1 m) and the bbox.
func (t *Trail) SetLine(pts []geo.Point) {
	t.line = pts
	t.Geometry = make([][2]float64, len(pts))
	bb := geo.BBox{math.Inf(1), math.Inf(1), math.Inf(-1), math.Inf(-1)}
	for i, p := range pts {
		t.Geometry[i] = [2]float64{round(p.Lng, 5), round(p.Lat, 5)}
		bb[0], bb[1] = math.Min(bb[0], p.Lng), math.Min(bb[1], p.Lat)
		bb[2], bb[3] = math.Max(bb[2], p.Lng), math.Max(bb[3], p.Lat)
	}
	t.BBox = geo.BBox{round(bb[0], 5), round(bb[1], 5), round(bb[2], 5), round(bb[3], 5)}
}

// Named fills Name/NameHi from Kind.
func (t *Trail) Named() {
	if n, ok := Names[t.Kind]; ok {
		if t.Name == "" {
			t.Name = n[0]
		}
		if t.NameHi == "" {
			t.NameHi = n[1]
		}
	}
}

func round(v float64, d int) float64 {
	p := math.Pow(10, float64(d))
	return math.Round(v*p) / p
}

// Round is exported for planners.
func Round(v float64, d int) float64 { return round(v, d) }
