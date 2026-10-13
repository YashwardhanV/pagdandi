// Package search answers place search: Pagdandi's own gems first, then
// Photon (OpenStreetMap geocoder), biased to the region.
package search

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"sync"
	"time"

	"pagdandi/api/internal/features"
	"pagdandi/api/internal/geo"
	"pagdandi/api/internal/region"
)

// Place is one result.
type Place struct {
	ID     string  `json:"id"`
	Name   string  `json:"name"`
	Label  string  `json:"label"` // secondary line: locality, kind
	Kind   string  `json:"kind"`
	Lat    float64 `json:"lat"`
	Lng    float64 `json:"lng"`
	Source string  `json:"source"` // gem | osm
	Inside bool    `json:"inside"` // within the served region
}

// Service searches.
type Service struct {
	Region    *region.Region
	Gems      []features.Gem
	PhotonURL string
	UserAgent string
	HTTP      *http.Client

	mu    sync.Mutex
	last  time.Time
	cache map[string]cached
}

type cached struct {
	at  time.Time
	val []Place
}

func (s *Service) public() bool { return strings.Contains(s.PhotonURL, "photon.komoot.io") }

// Search returns up to 8 places for q, near (if valid) biasing results.
func (s *Service) Search(ctx context.Context, q string, near geo.Point) ([]Place, error) {
	q = strings.TrimSpace(q)
	if len(q) < 2 {
		return nil, nil
	}
	out := s.gems(q)
	remote, err := s.photon(ctx, "/api/?"+url.Values{
		"q": {q}, "limit": {"8"}, "lang": {"en"},
		"lat":  {fmt.Sprintf("%.5f", pick(near, s.Region.CenterPoint()).Lat)},
		"lon":  {fmt.Sprintf("%.5f", pick(near, s.Region.CenterPoint()).Lng)},
		"bbox": {bboxParam(s.Region.BBox())},
	}.Encode())
	if err == nil {
		for _, r := range remote {
			if !dup(out, r) {
				out = append(out, r)
			}
		}
	}
	if len(out) > 8 {
		out = out[:8]
	}
	if len(out) == 0 && err != nil {
		return nil, err
	}
	return out, nil
}

// Reverse names the place at p.
func (s *Service) Reverse(ctx context.Context, p geo.Point) (*Place, error) {
	for _, g := range s.Gems {
		if geo.Haversine(p, g.Point()) < 120 {
			return &Place{ID: g.ID, Name: g.Name, Kind: g.Kind, Lat: g.Lat, Lng: g.Lng, Source: "gem", Inside: true, Label: g.Kind}, nil
		}
	}
	r, err := s.photon(ctx, "/reverse?"+url.Values{"lat": {fmt.Sprintf("%.6f", p.Lat)}, "lon": {fmt.Sprintf("%.6f", p.Lng)}, "lang": {"en"}, "limit": {"1"}}.Encode())
	if err != nil || len(r) == 0 {
		return &Place{Name: fmt.Sprintf("%.4f, %.4f", p.Lat, p.Lng), Lat: p.Lat, Lng: p.Lng, Source: "pin", Inside: s.Region.Contains(p)}, nil
	}
	r[0].Lat, r[0].Lng = p.Lat, p.Lng
	return &r[0], nil
}

func pick(p, def geo.Point) geo.Point {
	if p.Valid() && (p.Lat != 0 || p.Lng != 0) {
		return p
	}
	return def
}

func bboxParam(b geo.BBox) string { return fmt.Sprintf("%.4f,%.4f,%.4f,%.4f", b[0], b[1], b[2], b[3]) }

func dup(list []Place, p Place) bool {
	for _, x := range list {
		if strings.EqualFold(x.Name, p.Name) && geo.Haversine(geo.Point{Lat: x.Lat, Lng: x.Lng}, geo.Point{Lat: p.Lat, Lng: p.Lng}) < 1500 {
			return true
		}
	}
	return false
}

// gems matches local gems by word prefix.
func (s *Service) gems(q string) []Place {
	ql := strings.ToLower(q)
	type hit struct {
		g     features.Gem
		score float64
	}
	var hits []hit
	for _, g := range s.Gems {
		n := strings.ToLower(g.Name)
		sc := 0.0
		switch {
		case strings.HasPrefix(n, ql):
			sc = 2
		case strings.Contains(" "+n, " "+ql):
			sc = 1.5
		case strings.Contains(n, ql):
			sc = 1
		case g.NameHi != "" && strings.Contains(g.NameHi, q):
			sc = 1.5
		}
		if sc > 0 {
			hits = append(hits, hit{g, sc + g.Score})
		}
	}
	sort.Slice(hits, func(i, j int) bool { return hits[i].score > hits[j].score })
	var out []Place
	for _, h := range hits {
		t := h.g.Target()
		out = append(out, Place{ID: h.g.ID, Name: h.g.Name, Label: strings.Title(h.g.Kind), Kind: h.g.Kind, Lat: t.Lat, Lng: t.Lng, Source: "gem", Inside: true})
		if len(out) == 4 {
			break
		}
	}
	return out
}

type photonResp struct {
	Features []struct {
		Geometry struct {
			Coordinates [2]float64 `json:"coordinates"`
		} `json:"geometry"`
		Properties struct {
			OSMID    int64  `json:"osm_id"`
			OSMType  string `json:"osm_type"`
			OSMKey   string `json:"osm_key"`
			OSMValue string `json:"osm_value"`
			Name     string `json:"name"`
			Street   string `json:"street"`
			District string `json:"district"`
			City     string `json:"city"`
			County   string `json:"county"`
			State    string `json:"state"`
		} `json:"properties"`
	} `json:"features"`
}

func (s *Service) photon(ctx context.Context, path string) ([]Place, error) {
	s.mu.Lock()
	if s.cache == nil {
		s.cache = map[string]cached{}
	}
	if c, ok := s.cache[path]; ok && time.Since(c.at) < 24*time.Hour {
		s.mu.Unlock()
		return c.val, nil
	}
	// The public instance asks for ≤ 1 request/second.
	if s.public() {
		if wait := time.Second - time.Since(s.last); wait > 0 {
			s.mu.Unlock()
			select {
			case <-time.After(wait):
			case <-ctx.Done():
				return nil, ctx.Err()
			}
			s.mu.Lock()
		}
		s.last = time.Now()
	}
	s.mu.Unlock()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(s.PhotonURL, "/")+path, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", s.UserAgent)
	resp, err := s.HTTP.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("photon: %s", resp.Status)
	}
	var pr photonResp
	if err := json.NewDecoder(resp.Body).Decode(&pr); err != nil {
		return nil, err
	}
	var out []Place
	for _, f := range pr.Features {
		pp := f.Properties
		name := pp.Name
		if name == "" {
			name = pp.Street
		}
		if name == "" {
			continue
		}
		var parts []string
		for _, x := range []string{pp.District, pp.City, pp.County} {
			if x != "" && x != name && !containsStr(parts, x) {
				parts = append(parts, x)
			}
		}
		pt := geo.Point{Lat: f.Geometry.Coordinates[1], Lng: f.Geometry.Coordinates[0]}
		out = append(out, Place{ID: fmt.Sprintf("%s%d", strings.ToLower(pp.OSMType), pp.OSMID), Name: name, Label: strings.Join(parts, ", "),
			Kind: pp.OSMValue, Lat: pt.Lat, Lng: pt.Lng, Source: "osm", Inside: s.Region.Contains(pt)})
	}
	s.mu.Lock()
	s.cache[path] = cached{time.Now(), out}
	s.mu.Unlock()
	return out, nil
}

func containsStr(s []string, v string) bool {
	for _, x := range s {
		if x == v {
			return true
		}
	}
	return false
}
