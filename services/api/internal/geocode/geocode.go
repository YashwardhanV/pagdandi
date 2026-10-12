// Package geocode answers place search: the local curated-destination index
// first, then Photon (self-hosted, or the public instance at ≤1 req/s).
package geocode

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"pagdandi/api/internal/cache"
	"pagdandi/api/internal/geo"
	"pagdandi/api/internal/store"
)

// Place is one search result.
type Place struct {
	ID         string  `json:"id"`
	Name       string  `json:"name"`
	Label      string  `json:"label"`
	Lat        float64 `json:"lat"`
	Lng        float64 `json:"lng"`
	Source     string  `json:"source"` // destination | photon
	Category   string  `json:"category,omitempty"`
	RegionID   string  `json:"region_id,omitempty"`
	InCoverage bool    `json:"in_coverage"`
}

// Lookup tells whether a point is inside coverage.
type Lookup interface {
	RegionAt(ctx context.Context, p geo.Point) (string, bool, error)
}

// Service is the geocoder.
type Service struct {
	DB        *store.DB
	Lookup    Lookup
	Cache     cache.Cache
	PhotonURL string
	UserAgent string
	HTTP      *http.Client
}

// Public reports whether we talk to the shared public Photon (strict 1 req/s).
func (s *Service) Public() bool { return strings.Contains(s.PhotonURL, "photon.komoot.io") }

func (s *Service) throttle(ctx context.Context) error {
	if !s.Public() || s.Cache == nil {
		return nil
	}
	for i := 0; i < 3; i++ {
		ok, wait, err := s.Cache.Allow(ctx, "photon:global", 1, time.Second)
		if err != nil || ok {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(wait):
		}
	}
	return fmt.Errorf("geocoder busy")
}

type photonResp struct {
	Features []struct {
		Geometry struct {
			Coordinates [2]float64 `json:"coordinates"`
		} `json:"geometry"`
		Properties struct {
			OSMID    int64  `json:"osm_id"`
			OSMType  string `json:"osm_type"`
			Name     string `json:"name"`
			Street   string `json:"street"`
			House    string `json:"housenumber"`
			District string `json:"district"`
			City     string `json:"city"`
			County   string `json:"county"`
			State    string `json:"state"`
			Country  string `json:"country"`
			OSMKey   string `json:"osm_key"`
			OSMValue string `json:"osm_value"`
		} `json:"properties"`
	} `json:"features"`
}

func (s *Service) photon(ctx context.Context, path string, q url.Values) ([]Place, error) {
	key := "geo:" + path + ":" + hash(q.Encode())
	if s.Cache != nil {
		if b, ok, _ := s.Cache.Get(ctx, key); ok {
			var out []Place
			if json.Unmarshal(b, &out) == nil {
				return out, nil
			}
		}
	}
	if err := s.throttle(ctx); err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(s.PhotonURL, "/")+path+"?"+q.Encode(), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", s.UserAgent)
	client := s.HTTP
	if client == nil {
		client = &http.Client{Timeout: 5 * time.Second}
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("photon: %s", resp.Status)
	}
	var pr photonResp
	if err := json.NewDecoder(io.LimitReader(resp.Body, 4<<20)).Decode(&pr); err != nil {
		return nil, err
	}
	var out []Place
	for _, f := range pr.Features {
		p := f.Properties
		name := p.Name
		if name == "" {
			name = strings.TrimSpace(p.Street + " " + p.House)
		}
		if name == "" {
			continue
		}
		var parts []string
		for _, x := range []string{p.District, p.City, p.County, p.State} {
			if x != "" && x != name && !contains(parts, x) {
				parts = append(parts, x)
			}
		}
		out = append(out, Place{
			ID: fmt.Sprintf("osm:%s%d", strings.ToLower(p.OSMType), p.OSMID), Name: name, Label: strings.Join(parts, ", "),
			Lat: f.Geometry.Coordinates[1], Lng: f.Geometry.Coordinates[0], Source: "photon", Category: p.OSMValue,
		})
	}
	if s.Cache != nil {
		if b, err := json.Marshal(out); err == nil {
			_ = s.Cache.Set(ctx, key, b, 24*time.Hour)
		}
	}
	return out, nil
}

func contains(ss []string, s string) bool {
	for _, x := range ss {
		if x == s {
			return true
		}
	}
	return false
}

func hash(s string) string {
	h := sha1.Sum([]byte(s))
	return hex.EncodeToString(h[:8])
}

// Search runs autocomplete: local destinations first, then Photon. Results
// outside coverage are kept but flagged and sorted last.
func (s *Service) Search(ctx context.Context, q string, near *geo.Point, lang string, limit int) ([]Place, error) {
	q = strings.TrimSpace(q)
	if len([]rune(q)) < 2 {
		return []Place{}, nil
	}
	out := []Place{}
	if s.DB != nil {
		ds, err := s.DB.SearchDestinations(ctx, q, near, 5)
		if err != nil {
			return nil, err
		}
		for _, d := range ds {
			if d.Lat == nil || d.Lng == nil {
				continue
			}
			out = append(out, Place{ID: "dest:" + d.ID, Name: d.Name, Label: d.Category, Lat: *d.Lat, Lng: *d.Lng,
				Source: "destination", Category: d.Category, RegionID: d.RegionID, InCoverage: true})
		}
	}
	if s.PhotonURL != "" && len(out) < limit {
		v := url.Values{"q": {q}, "limit": {strconv.Itoa(limit)}}
		if near != nil {
			v.Set("lat", strconv.FormatFloat(near.Lat, 'f', 5, 64))
			v.Set("lon", strconv.FormatFloat(near.Lng, 'f', 5, 64))
		}
		if lang == "en" { // Photon has no hi/sv; its default uses local names.
			v.Set("lang", "en")
		}
		ps, err := s.photon(ctx, "/api", v)
		if err == nil {
			for _, p := range ps {
				if s.dup(out, p) {
					continue
				}
				if s.Lookup != nil {
					if id, ok, err := s.Lookup.RegionAt(ctx, geo.Point{Lat: p.Lat, Lng: p.Lng}); err == nil && ok {
						p.InCoverage, p.RegionID = true, id
					}
				}
				out = append(out, p)
			}
		} else if len(out) == 0 {
			return nil, err
		}
	}
	// Stable partition: in-coverage first.
	in, outCov := []Place{}, []Place{}
	for _, p := range out {
		if p.InCoverage {
			in = append(in, p)
		} else {
			outCov = append(outCov, p)
		}
	}
	res := append(in, outCov...)
	if len(res) > limit {
		res = res[:limit]
	}
	return res, nil
}

func (s *Service) dup(list []Place, p Place) bool {
	for _, x := range list {
		if strings.EqualFold(x.Name, p.Name) && geo.Haversine(geo.Point{Lat: x.Lat, Lng: x.Lng}, geo.Point{Lat: p.Lat, Lng: p.Lng}) < 300 {
			return true
		}
	}
	return false
}

// Reverse returns the nearest named place.
func (s *Service) Reverse(ctx context.Context, p geo.Point, lang string) (*Place, error) {
	v := url.Values{"lat": {strconv.FormatFloat(p.Lat, 'f', 5, 64)}, "lon": {strconv.FormatFloat(p.Lng, 'f', 5, 64)}, "limit": {"1"}}
	if lang == "en" {
		v.Set("lang", "en")
	}
	ps, err := s.photon(ctx, "/reverse", v)
	if err != nil {
		return nil, err
	}
	if len(ps) == 0 {
		return &Place{ID: "pt", Name: fmt.Sprintf("%.5f, %.5f", p.Lat, p.Lng), Lat: p.Lat, Lng: p.Lng, Source: "coordinates"}, nil
	}
	res := ps[0]
	if s.Lookup != nil {
		if id, ok, err := s.Lookup.RegionAt(ctx, p); err == nil && ok {
			res.InCoverage, res.RegionID = true, id
		}
	}
	return &res, nil
}

// Forward geocodes a single name (used by datactl at seed time).
func (s *Service) Forward(ctx context.Context, q string, near geo.Point) ([]Place, error) {
	v := url.Values{"q": {q}, "limit": {"5"}, "lat": {strconv.FormatFloat(near.Lat, 'f', 4, 64)}, "lon": {strconv.FormatFloat(near.Lng, 'f', 4, 64)}}
	return s.photon(ctx, "/api", v)
}
