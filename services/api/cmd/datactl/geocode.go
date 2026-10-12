package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
	"unicode"

	"pagdandi/api/internal/geo"
	"pagdandi/api/internal/regions"
)

// Resolved is one geocoded destination, cached in data/destinations.resolved.json.
type Resolved struct {
	Lat      float64 `json:"lat,omitempty"`
	Lng      float64 `json:"lng,omitempty"`
	Verified bool    `json:"verified"`
	Source   string  `json:"source,omitempty"`
	Display  string  `json:"display,omitempty"`
	DistKm   float64 `json:"straight_km,omitempty"`
	ApproxKm float64 `json:"approx_km"`
	Reason   string  `json:"reason,omitempty"`
}

func resolvedPath(o *opts) string { return filepath.Join(o.root, "data", "destinations.resolved.json") }

func loadResolved(o *opts) (map[string]Resolved, error) {
	m := map[string]Resolved{}
	b, err := os.ReadFile(resolvedPath(o))
	if os.IsNotExist(err) {
		return m, nil
	}
	if err != nil {
		return nil, err
	}
	return m, json.Unmarshal(b, &m)
}

func saveResolved(o *opts, m map[string]Resolved) error {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	// Stable, diff-friendly output.
	var sb strings.Builder
	sb.WriteString("{\n")
	for i, k := range keys {
		kb, _ := json.Marshal(k)
		vb, _ := json.Marshal(m[k])
		sb.WriteString("  " + string(kb) + ": " + string(vb))
		if i < len(keys)-1 {
			sb.WriteString(",")
		}
		sb.WriteString("\n")
	}
	sb.WriteString("}\n")
	return os.WriteFile(resolvedPath(o), []byte(sb.String()), 0o644)
}

type geocoder struct {
	o      *opts
	http   *http.Client
	ua     string
	last   time.Time
	photon string
}

func newGeocoder(o *opts) *geocoder {
	contact := envOr("GEOCODER_CONTACT_EMAIL", "dev build; contact not configured")
	return &geocoder{
		o: o, http: &http.Client{Timeout: 15 * time.Second},
		ua:     fmt.Sprintf("Pagdandi/0.1 (%s)", contact),
		photon: strings.TrimRight(envOr("PHOTON_URL", "https://photon.komoot.io"), "/"),
	}
}

// wait keeps public geocoders at ≤1 request per second (SPEC §4.1).
func (g *geocoder) wait() {
	if d := time.Since(g.last); d < 1100*time.Millisecond {
		time.Sleep(1100*time.Millisecond - d)
	}
	g.last = time.Now()
}

type hit struct {
	name, display, source string
	country               string // ISO 3166-1 alpha-2, upper case ("" if unknown)
	p                     geo.Point
}

func (g *geocoder) get(ctx context.Context, u string, out any) error {
	for attempt := 0; attempt < 3; attempt++ {
		g.wait()
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
		if err != nil {
			return err
		}
		req.Header.Set("User-Agent", g.ua)
		resp, err := g.http.Do(req)
		if err != nil {
			time.Sleep(time.Duration(attempt+1) * 2 * time.Second)
			continue
		}
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
		resp.Body.Close()
		if resp.StatusCode == 429 || resp.StatusCode >= 500 {
			time.Sleep(time.Duration(attempt+1) * 3 * time.Second)
			continue
		}
		if resp.StatusCode != 200 {
			return fmt.Errorf("%s: %s", u, resp.Status)
		}
		return json.Unmarshal(body, out)
	}
	return fmt.Errorf("%s: gave up after retries", u)
}

func (g *geocoder) photonSearch(ctx context.Context, q string, near geo.Point) ([]hit, error) {
	v := url.Values{"q": {q}, "limit": {"8"}, "lat": {fmt.Sprintf("%.4f", near.Lat)}, "lon": {fmt.Sprintf("%.4f", near.Lng)}, "lang": {"en"}}
	var r struct {
		Features []struct {
			Geometry struct {
				Coordinates [2]float64 `json:"coordinates"`
			} `json:"geometry"`
			Properties map[string]any `json:"properties"`
		} `json:"features"`
	}
	if err := g.get(ctx, g.photon+"/api?"+v.Encode(), &r); err != nil {
		return nil, err
	}
	var out []hit
	for _, f := range r.Features {
		name, _ := f.Properties["name"].(string)
		var parts []string
		for _, k := range []string{"city", "county", "state"} {
			if s, ok := f.Properties[k].(string); ok && s != "" {
				parts = append(parts, s)
			}
		}
		cc, _ := f.Properties["countrycode"].(string)
		out = append(out, hit{name: name, display: strings.Join(append([]string{name}, parts...), ", "), source: "photon",
			country: strings.ToUpper(cc), p: geo.Point{Lat: f.Geometry.Coordinates[1], Lng: f.Geometry.Coordinates[0]}})
	}
	return out, nil
}

func (g *geocoder) nominatimSearch(ctx context.Context, q string, near geo.Point) ([]hit, error) {
	d := 2.0
	v := url.Values{"q": {q}, "format": {"jsonv2"}, "limit": {"8"}, "addressdetails": {"1"},
		"viewbox": {fmt.Sprintf("%.3f,%.3f,%.3f,%.3f", near.Lng-d, near.Lat+d, near.Lng+d, near.Lat-d)}}
	var r []struct {
		Lat, Lon, Name, DisplayName string `json:"-"`
		RawLat                      string `json:"lat"`
		RawLon                      string `json:"lon"`
		RawName                     string `json:"name"`
		RawDisplay                  string `json:"display_name"`
		Address                     struct {
			CountryCode string `json:"country_code"`
		} `json:"address"`
	}
	if err := g.get(ctx, "https://nominatim.openstreetmap.org/search?"+v.Encode(), &r); err != nil {
		return nil, err
	}
	var out []hit
	for _, x := range r {
		var lat, lng float64
		fmt.Sscanf(x.RawLat, "%f", &lat)
		fmt.Sscanf(x.RawLon, "%f", &lng)
		out = append(out, hit{name: x.RawName, display: x.RawDisplay, source: "nominatim", country: strings.ToUpper(x.Address.CountryCode), p: geo.Point{Lat: lat, Lng: lng}})
	}
	return out, nil
}

func tokens(s string) map[string]bool {
	m := map[string]bool{}
	for _, f := range strings.FieldsFunc(strings.ToLower(s), func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsDigit(r) }) {
		if len(f) > 2 {
			m[f] = true
		}
	}
	return m
}

func nameScore(want, got string) float64 {
	a, b := tokens(want), tokens(got)
	if len(a) == 0 {
		return 0
	}
	n := 0
	for t := range a {
		if b[t] {
			n++
		}
	}
	return float64(n) / float64(len(a))
}

// resolve geocodes one destination and applies SPEC §4.1's checks: the straight-line
// distance from the centre must be within ±40 % of approx_km (with a 2 km floor for
// very close places), and the point must be inside the region circle unless it is a
// corridor target. Coordinates are never invented: failures stay unverified.
func (g *geocoder) resolve(ctx context.Context, reg regions.Region, name string, approxKm float64, requireCoverage bool) Resolved {
	center := reg.CenterPoint()
	tol := math.Max(0.4*approxKm, 2)
	check := func(h hit) (bool, float64, string) {
		d := geo.Haversine(center, h.p) / 1000
		if h.country != "" && h.country != reg.Country {
			return false, d, "result is in " + h.country + ", not " + reg.Country
		}
		if approxKm > 0 && math.Abs(d-approxKm) > tol {
			return false, d, fmt.Sprintf("%.1f km from centre, expected %.0f±%.0f", d, approxKm, tol)
		}
		if requireCoverage && d > reg.RadiusKm {
			return false, d, "outside coverage"
		}
		return true, d, ""
	}
	queries := []string{name, name + ", " + reg.Name}
	if i := strings.Index(name, "("); i > 0 {
		queries = append(queries, strings.TrimSpace(name[:i]))
	}
	if i := strings.Index(name, ","); i > 0 {
		queries = append(queries, strings.TrimSpace(name[:i]))
	}
	lastReason := "no results"
	for _, search := range []func(context.Context, string, geo.Point) ([]hit, error){g.photonSearch, g.nominatimSearch} {
		for _, q := range queries {
			hits, err := search(ctx, q, center)
			if err != nil {
				lastReason = err.Error()
				continue
			}
			var best *hit
			bestScore, bestD := -1.0, 0.0
			for i := range hits {
				ok, d, why := check(hits[i])
				if !ok {
					lastReason = why
					continue
				}
				score := nameScore(name, hits[i].name+" "+hits[i].display) - 0.002*math.Abs(d-approxKm)
				if score > bestScore {
					best, bestScore, bestD = &hits[i], score, d
				}
			}
			if best != nil && bestScore >= 0.3 {
				return Resolved{Lat: math.Round(best.p.Lat*1e6) / 1e6, Lng: math.Round(best.p.Lng*1e6) / 1e6, Verified: true,
					Source: best.source, Display: best.display, DistKm: math.Round(bestD*10) / 10, ApproxKm: approxKm}
			}
			if best != nil {
				lastReason = fmt.Sprintf("weak name match %q", best.name)
			}
		}
	}
	return Resolved{Verified: false, ApproxKm: approxKm, Reason: lastReason}
}
