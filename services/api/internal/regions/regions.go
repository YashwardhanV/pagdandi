// Package regions loads data/regions.yaml and answers region-level questions:
// which season is active, which graph serves a region, coverage geometry.
package regions

import (
	"fmt"
	"os"
	"sort"
	"strings"
	"time"

	"gopkg.in/yaml.v3"

	"pagdandi/api/internal/geo"
)

// Season is a recurring date window that makes routing more conservative.
type Season struct {
	Name                   string  `yaml:"name" json:"name"`
	From                   string  `yaml:"from" json:"from"` // MM-DD inclusive
	To                     string  `yaml:"to" json:"to"`     // MM-DD inclusive
	UnpavedPenalty         float64 `yaml:"unpaved_penalty" json:"unpaved_penalty"`
	ConfidenceHalfLifeDays float64 `yaml:"confidence_half_life_days" json:"confidence_half_life_days"`
	LandslideRisk          bool    `yaml:"landslide_risk" json:"landslide_risk,omitempty"`
	Note                   string  `yaml:"note" json:"note,omitempty"`
}

// Destination is a curated place near a region's centre.
type Destination struct {
	Name     string  `yaml:"name" json:"name"`
	ApproxKm float64 `yaml:"approx_km" json:"approx_km"`
	Type     string  `yaml:"type" json:"type"`
	Note     string  `yaml:"note" json:"note,omitempty"`
}

// Corridor extends coverage toward a famous destination outside the radius.
type Corridor struct {
	To       string  `yaml:"to" json:"to"`
	ApproxKm float64 `yaml:"approx_km" json:"approx_km"`
}

// Region is one city with its coverage settings.
type Region struct {
	ID               string        `yaml:"id" json:"id"`
	Name             string        `yaml:"name" json:"name"`
	Country          string        `yaml:"country" json:"country"`
	Timezone         string        `yaml:"timezone" json:"timezone"`
	Center           [2]float64    `yaml:"center" json:"center"` // [lat, lng]
	RadiusKm         float64       `yaml:"radius_km" json:"radius_km"`
	CorridorBufferKm float64       `yaml:"corridor_buffer_km" json:"corridor_buffer_km"`
	Langs            []string      `yaml:"langs" json:"langs"`
	DefaultVehicle   string        `yaml:"default_vehicle" json:"default_vehicle"`
	Seasons          []Season      `yaml:"seasons" json:"seasons"`
	Destinations     []Destination `yaml:"destinations" json:"-"`
	Corridors        []Corridor    `yaml:"corridors" json:"corridors,omitempty"`
	Notes            string        `yaml:"notes" json:"notes,omitempty"`
}

// CenterPoint returns the region centre.
func (r Region) CenterPoint() geo.Point { return geo.Point{Lat: r.Center[0], Lng: r.Center[1]} }

type defaults struct {
	RadiusKm         float64  `yaml:"radius_km"`
	CorridorBufferKm float64  `yaml:"corridor_buffer_km"`
	Timezone         string   `yaml:"timezone"`
	Country          string   `yaml:"country"`
	Langs            []string `yaml:"langs"`
	Seasons          []Season `yaml:"seasons"`
}

type file struct {
	Defaults defaults `yaml:"defaults"`
	Regions  []Region `yaml:"regions"`
}

// Set is the parsed regions file with defaults applied.
type Set struct {
	Regions []Region
	byID    map[string]int
}

// Load reads and validates a regions.yaml file.
func Load(path string) (*Set, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return Parse(b)
}

// Parse parses regions YAML and applies defaults.
func Parse(b []byte) (*Set, error) {
	var f file
	if err := yaml.Unmarshal(b, &f); err != nil {
		return nil, fmt.Errorf("regions: %w", err)
	}
	s := &Set{byID: map[string]int{}}
	for i, r := range f.Regions {
		if r.ID == "" || r.Name == "" {
			return nil, fmt.Errorf("regions: entry %d missing id or name", i)
		}
		if _, dup := s.byID[r.ID]; dup {
			return nil, fmt.Errorf("regions: duplicate id %q", r.ID)
		}
		if r.RadiusKm == 0 {
			r.RadiusKm = f.Defaults.RadiusKm
		}
		if r.CorridorBufferKm == 0 {
			r.CorridorBufferKm = f.Defaults.CorridorBufferKm
		}
		if r.Timezone == "" {
			r.Timezone = f.Defaults.Timezone
		}
		if r.Country == "" {
			r.Country = f.Defaults.Country
		}
		r.Country = strings.ToUpper(r.Country)
		if len(r.Langs) == 0 {
			r.Langs = f.Defaults.Langs
		}
		if r.Seasons == nil {
			r.Seasons = f.Defaults.Seasons
		}
		if r.DefaultVehicle == "" {
			if r.Country == "SE" {
				r.DefaultVehicle = "bicycle"
			} else {
				r.DefaultVehicle = "scooter"
			}
		}
		if _, err := time.LoadLocation(r.Timezone); err != nil {
			return nil, fmt.Errorf("regions: %s: bad timezone %q: %w", r.ID, r.Timezone, err)
		}
		if !r.CenterPoint().Valid() {
			return nil, fmt.Errorf("regions: %s: invalid center", r.ID)
		}
		for _, se := range r.Seasons {
			if _, _, err := parseMMDD(se.From); err != nil {
				return nil, fmt.Errorf("regions: %s season %s: %w", r.ID, se.Name, err)
			}
			if _, _, err := parseMMDD(se.To); err != nil {
				return nil, fmt.Errorf("regions: %s season %s: %w", r.ID, se.Name, err)
			}
			if se.UnpavedPenalty < 1 {
				return nil, fmt.Errorf("regions: %s season %s: unpaved_penalty must be >= 1 (seasons only make routes more conservative)", r.ID, se.Name)
			}
		}
		s.byID[r.ID] = len(s.Regions)
		s.Regions = append(s.Regions, r)
	}
	return s, nil
}

// Get returns the region with the given id.
func (s *Set) Get(id string) (Region, bool) {
	i, ok := s.byID[id]
	if !ok {
		return Region{}, false
	}
	return s.Regions[i], true
}

// Countries returns the distinct country codes, sorted.
func (s *Set) Countries() []string {
	seen := map[string]bool{}
	var out []string
	for _, r := range s.Regions {
		if !seen[r.Country] {
			seen[r.Country] = true
			out = append(out, r.Country)
		}
	}
	sort.Strings(out)
	return out
}

// Nearest returns the region whose centre is closest to p, and the distance to it in metres.
func (s *Set) Nearest(p geo.Point) (Region, float64) { return s.NearestWhere(p, nil) }

// NearestWhere is Nearest restricted to regions accepted by keep (nil keeps all).
func (s *Set) NearestWhere(p geo.Point, keep func(id string) bool) (Region, float64) {
	var best Region
	bestD := -1.0
	for _, r := range s.Regions {
		if keep != nil && !keep(r.ID) {
			continue
		}
		d := geo.Haversine(p, r.CenterPoint())
		if bestD < 0 || d < bestD {
			best, bestD = r, d
		}
	}
	return best, bestD
}

func parseMMDD(s string) (int, int, error) {
	var m, d int
	if _, err := fmt.Sscanf(s, "%d-%d", &m, &d); err != nil || m < 1 || m > 12 || d < 1 || d > 31 {
		return 0, 0, fmt.Errorf("bad MM-DD %q", s)
	}
	return m, d, nil
}

// ActiveSeason returns the season active at t in the region's time zone, or nil.
// When windows overlap, the most conservative one (highest penalty) wins.
func (r Region) ActiveSeason(t time.Time) *Season {
	loc, err := time.LoadLocation(r.Timezone)
	if err != nil {
		loc = time.UTC
	}
	lt := t.In(loc)
	today := int(lt.Month())*100 + lt.Day()
	var best *Season
	for i := range r.Seasons {
		se := r.Seasons[i]
		fm, fd, _ := parseMMDD(se.From)
		tm, td, _ := parseMMDD(se.To)
		from, to := fm*100+fd, tm*100+td
		in := false
		if from <= to {
			in = today >= from && today <= to
		} else { // wraps over the new year
			in = today >= from || today <= to
		}
		if in && (best == nil || se.UnpavedPenalty > best.UnpavedPenalty) {
			best = &se
		}
	}
	return best
}

// HalfLifeDays returns the confidence half-life at t: the active season's value or the default 30 days.
func (r Region) HalfLifeDays(t time.Time) float64 {
	if s := r.ActiveSeason(t); s != nil && s.ConfidenceHalfLifeDays > 0 {
		return s.ConfidenceHalfLifeDays
	}
	return 30
}

// Vehicles returns the vehicles routable in the region.
func (r Region) Vehicles() []string {
	if r.Country == "SE" {
		return []string{"bicycle", "moped"}
	}
	return []string{"scooter", "motorcycle"}
}

// CoverageRings returns the rings (circle plus corridors) making up this region's
// coverage, as GeoJSON-ready [lng,lat] rings. Corridor endpoints come from the
// resolver, which returns the resolved location of a corridor destination.
func (r Region) CoverageRings(resolve func(name string) (geo.Point, bool)) [][][2]float64 {
	rings := [][][2]float64{geo.Circle(r.CenterPoint(), r.RadiusKm*1000, 64)}
	for _, c := range r.Corridors {
		if resolve == nil {
			continue
		}
		if p, ok := resolve(c.To); ok {
			rings = append(rings, geo.Corridor(r.CenterPoint(), p, r.CorridorBufferKm*1000))
		}
	}
	return rings
}
