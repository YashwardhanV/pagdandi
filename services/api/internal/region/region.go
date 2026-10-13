// Package region loads data/region.yaml: the one area Pagdandi serves.
package region

import (
	"fmt"
	"os"
	"strings"
	"time"

	"gopkg.in/yaml.v3"

	"pagdandi/api/internal/geo"
)

// Source is one OSM extract to download.
type Source struct {
	Name   string `yaml:"name" json:"name"`
	URL    string `yaml:"url" json:"url"`
	Mirror string `yaml:"mirror" json:"mirror,omitempty"`
}

// Featured is a place locals ride to; matching OSM features get a boost.
type Featured struct {
	Name    string   `yaml:"name" json:"name"`
	Kind    string   `yaml:"kind" json:"kind"`
	Aliases []string `yaml:"aliases" json:"aliases,omitempty"`
}

// Region is the served area.
type Region struct {
	ID          string     `yaml:"id" json:"id"`
	Name        string     `yaml:"name" json:"name"`
	NameHi      string     `yaml:"name_hi" json:"name_hi"`
	Center      [2]float64 `yaml:"center" json:"-"` // [lat, lng]
	RadiusKm    float64    `yaml:"radius_km" json:"radius_km"`
	Timezone    string     `yaml:"timezone" json:"timezone"`
	OSMSources  []Source   `yaml:"osm_sources" json:"-"`
	FreshMonths int        `yaml:"fresh_months" json:"fresh_months"`
	Featured    []Featured `yaml:"featured" json:"-"`

	loc *time.Location
}

// Load reads and validates a region file.
func Load(path string) (*Region, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var r Region
	if err := yaml.Unmarshal(b, &r); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	if r.ID == "" || r.RadiusKm <= 0 || !r.CenterPoint().Valid() {
		return nil, fmt.Errorf("%s: id, center and radius_km are required", path)
	}
	if r.FreshMonths <= 0 {
		r.FreshMonths = 18
	}
	if r.Timezone == "" {
		r.Timezone = "UTC"
	}
	loc, err := time.LoadLocation(r.Timezone)
	if err != nil {
		return nil, fmt.Errorf("%s: timezone: %w", path, err)
	}
	r.loc = loc
	return &r, nil
}

// CenterPoint returns the centre as a geo.Point.
func (r *Region) CenterPoint() geo.Point { return geo.Point{Lat: r.Center[0], Lng: r.Center[1]} }

// Location is the region's time zone.
func (r *Region) Location() *time.Location {
	if r.loc == nil {
		return time.UTC
	}
	return r.loc
}

// Contains reports whether p is inside the served circle.
func (r *Region) Contains(p geo.Point) bool {
	return geo.Haversine(r.CenterPoint(), p) <= r.RadiusKm*1000
}

// Ring is the coverage circle as a closed [lng, lat] ring (64 sides).
func (r *Region) Ring() [][2]float64 { return geo.Circle(r.CenterPoint(), r.RadiusKm*1000, 64) }

// BBox is the bounding box of the coverage circle.
func (r *Region) BBox() geo.BBox {
	c := r.CenterPoint()
	m := r.RadiusKm * 1000
	n, s := geo.Destination(c, 0, m), geo.Destination(c, 180, m)
	e, w := geo.Destination(c, 90, m), geo.Destination(c, 270, m)
	return geo.BBox{w.Lng, s.Lat, e.Lng, n.Lat}
}

// MatchFeatured returns the featured entry whose name or alias appears in name.
func (r *Region) MatchFeatured(name string) (Featured, bool) {
	n := strings.ToLower(name)
	if n == "" {
		return Featured{}, false
	}
	for _, f := range r.Featured {
		for _, a := range append([]string{f.Name}, f.Aliases...) {
			if strings.Contains(n, strings.ToLower(a)) {
				return f, true
			}
		}
	}
	return Featured{}, false
}
