package routing

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"pagdandi/api/internal/cache"
	"pagdandi/api/internal/geo"
	"pagdandi/api/internal/regions"
)

const regionsYAML = `
defaults:
  radius_km: 150
  corridor_buffer_km: 20
  timezone: Asia/Kolkata
  country: IN
  langs: [en, hi]
  seasons:
    - {name: monsoon, from: "06-01", to: "09-30", unpaved_penalty: 1.6, confidence_half_life_days: 7}
regions:
  - id: indore
    name: Indore
    center: [22.7196, 75.8577]
  - id: stockholm
    name: Stockholm
    country: SE
    timezone: Europe/Stockholm
    center: [59.3293, 18.0686]
    seasons: []
`

// Lal Bagh area → Bilawali lake, Indore.
var (
	origin = geo.Point{Lat: 22.6960, Lng: 75.8470}
	dest   = geo.Point{Lat: 22.6795, Lng: 75.8335}
)

type fakeLookup struct{}

func (fakeLookup) RegionAt(_ context.Context, p geo.Point) (string, bool, error) {
	if geo.Haversine(p, geo.Point{Lat: 22.7196, Lng: 75.8577}) < 150000 {
		return "indore", true, nil
	}
	if geo.Haversine(p, geo.Point{Lat: 59.3293, Lng: 18.0686}) < 150000 {
		return "stockholm", true, nil
	}
	return "", false, nil
}

type fakeStore struct {
	cands    []Candidate
	barriers []Barrier
}

func (f *fakeStore) Candidates(context.Context, CandidateQuery) ([]Candidate, error) {
	return f.cands, nil
}
func (f *fakeStore) BarriersNear(_ context.Context, line []geo.Point, d float64) ([]Barrier, error) {
	var out []Barrier
	for _, b := range f.barriers {
		if geo.Project(line, b.Point).DistM <= d {
			out = append(out, b)
		}
	}
	return out, nil
}

// fakeGH answers like GraphHopper: the standard profile goes around via a
// detour point; shortcut profiles go nearly straight on a track.
func fakeGH(t *testing.T, calls *int32) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(calls, 1)
		var req RouteRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Errorf("bad body: %v", err)
		}
		a := geo.Point{Lat: req.Points[0][1], Lng: req.Points[0][0]}
		b := geo.Point{Lat: req.Points[1][1], Lng: req.Points[1][0]}
		var line []geo.Point
		var speed float64 // m/s
		var rc, sf []Detail
		isFullTrip := geo.Haversine(a, origin) < 1 && geo.Haversine(b, dest) < 1
		switch {
		case strings.HasSuffix(req.Profile, "standard"):
			via := geo.Point{Lat: 22.7050, Lng: 75.8200} // around via Khandwa Naka
			line = []geo.Point{a, via, b}
			speed = 7
			rc = []Detail{{0, 2, "primary"}}
			sf = []Detail{{0, 2, "asphalt"}}
		case isFullTrip:
			mid := geo.Point{Lat: (a.Lat + b.Lat) / 2, Lng: (a.Lng + b.Lng) / 2}
			line = []geo.Point{a, mid, b}
			speed = 5.5
			rc = []Detail{{0, 1, "residential"}, {1, 2, "track"}}
			sf = []Detail{{0, 1, "asphalt"}, {1, 2, "dirt"}}
		default: // legs to/from community shortcuts
			line = []geo.Point{a, b}
			speed = 6
			rc = []Detail{{0, 1, "residential"}}
			sf = []Detail{{0, 1, "asphalt"}}
		}
		dist := geo.Length(line)
		path := map[string]any{
			"distance": dist, "time": int64(dist / speed * 1000), "weight": dist / speed,
			"points": geo.Encode(line),
			"instructions": []map[string]any{
				{"text": "Continue", "distance": dist, "time": int64(dist / speed * 1000), "sign": 0, "interval": []int{0, len(line) - 1}},
				{"text": "Arrive at destination", "distance": 0, "time": 0, "sign": 4, "interval": []int{len(line) - 1, len(line) - 1}},
			},
			"details": map[string]any{"road_class": rc, "surface": sf},
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"paths": []any{path}})
	}))
}

func newRouter(t *testing.T, gh *httptest.Server, store Store, at time.Time) *Router {
	set, err := regions.Parse([]byte(regionsYAML))
	if err != nil {
		t.Fatal(err)
	}
	return &Router{
		Regions: set, Lookup: fakeLookup{}, Store: store, Cache: cache.NewMemory(),
		Graphs: map[string]*GH{"IN": NewGH(gh.URL), "SE": NewGH(gh.URL)},
		Now:    func() time.Time { return at },
	}
}

var ist, _ = time.LoadLocation("Asia/Kolkata")
var octoberNoon = time.Date(2026, 10, 12, 12, 0, 0, 0, ist)

func TestRouteStandardAndShortcut(t *testing.T) {
	var calls int32
	gh := fakeGH(t, &calls)
	defer gh.Close()
	r := newRouter(t, gh, &fakeStore{}, octoberNoon)
	resp, err := r.Route(context.Background(), Request{From: origin, To: dest})
	if err != nil {
		t.Fatal(err)
	}
	if resp.Region != "indore" || resp.Context.Vehicle != "scooter" || !resp.Context.Daylight || resp.Context.Season != nil {
		t.Fatalf("context = %+v", resp.Context)
	}
	if len(resp.Options) != 2 || resp.Options[0].ID != "std" || resp.Options[1].ID != "sc1" {
		t.Fatalf("options = %+v", resp.Options)
	}
	sc := resp.Options[1]
	if sc.SurfaceBreakdown.TrackM == 0 || sc.Savings == nil || sc.Savings.DurationS <= 0 {
		t.Fatalf("shortcut analysis wrong: %+v", sc)
	}
	if resp.Recommended != "sc1" {
		t.Fatalf("recommended = %s", resp.Recommended)
	}
	if resp.Disclaimer == "" || resp.Attribution != Attribution {
		t.Fatal("disclaimer and attribution required")
	}
	foundUnpaved := false
	for _, w := range sc.Warnings {
		if w.Type == "unpaved" && w.Surface == SurfTrack {
			foundUnpaved = true
		}
	}
	if !foundUnpaved {
		t.Fatalf("expected track warning, got %+v", sc.Warnings)
	}
	// Second identical request is served from cache.
	before := atomic.LoadInt32(&calls)
	resp2, err := r.Route(context.Background(), Request{From: origin, To: dest})
	if err != nil || !resp2.Cached || atomic.LoadInt32(&calls) != before {
		t.Fatalf("expected cache hit (err=%v cached=%v)", err, resp2 != nil && resp2.Cached)
	}
	if resp2.RequestID == resp.RequestID {
		t.Fatal("cached response must get a fresh request id")
	}
	if _, ok := r.Stored(context.Background(), resp.RequestID); !ok {
		t.Fatal("route must be retrievable by id for GPX")
	}
}

func TestRouteNightUsesNightProfileAndDoesNotRecommendTrack(t *testing.T) {
	var calls int32
	gh := fakeGH(t, &calls)
	defer gh.Close()
	night := time.Date(2026, 10, 12, 22, 30, 0, 0, ist)
	r := newRouter(t, gh, &fakeStore{}, night)
	resp, err := r.Route(context.Background(), Request{From: origin, To: dest})
	if err != nil {
		t.Fatal(err)
	}
	if resp.Context.Daylight || resp.Context.Profiles[1] != "tw_shortcut_night" {
		t.Fatalf("expected night profile, got %+v", resp.Context)
	}
	if resp.Recommended != "std" {
		t.Fatal("unlit night track must not be recommended")
	}
	hasNight := false
	for _, w := range resp.Options[1].Warnings {
		hasNight = hasNight || w.Type == "night"
	}
	if !hasNight {
		t.Fatal("night warning expected")
	}
}

func TestRouteMonsoonAddsSeasonModel(t *testing.T) {
	var seen []string
	gh := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req RouteRequest
		_ = json.NewDecoder(r.Body).Decode(&req)
		if req.CustomModel != nil && len(req.CustomModel.Priority) == 1 {
			seen = append(seen, req.CustomModel.Priority[0].MultiplyBy)
		}
		line := []geo.Point{origin, dest}
		_ = json.NewEncoder(w).Encode(map[string]any{"paths": []any{map[string]any{
			"distance": 2000, "time": 300000, "points": geo.Encode(line),
			"details": map[string]any{"road_class": [][]any{{0, 1, "primary"}}, "surface": [][]any{{0, 1, "asphalt"}}},
		}}})
	}))
	defer gh.Close()
	july := time.Date(2026, 7, 20, 12, 0, 0, 0, ist)
	r := newRouter(t, gh, nil, july)
	resp, err := r.Route(context.Background(), Request{From: origin, To: dest, Mode: "standard"})
	if err != nil {
		t.Fatal(err)
	}
	if resp.Context.Season == nil || *resp.Context.Season != "monsoon" {
		t.Fatal("monsoon expected")
	}
	if len(seen) != 1 || seen[0] != "0.6250" {
		t.Fatalf("season multiply_by = %v (want 1/1.6)", seen)
	}
	if len(resp.Options) != 1 {
		t.Fatal("standard mode returns only the usual route")
	}
}

func TestRouteCommunityOverlay(t *testing.T) {
	var calls int32
	gh := fakeGH(t, &calls)
	defer gh.Close()
	// A rider shortcut lying right on the straight line between O and D.
	a := geo.Point{Lat: origin.Lat - 0.002, Lng: origin.Lng - 0.0016}
	b := geo.Point{Lat: dest.Lat + 0.002, Lng: dest.Lng + 0.0016}
	line := []geo.Point{a, b}
	now := octoberNoon
	far := geo.Point{Lat: 22.80, Lng: 75.95} // way outside the ellipse
	store := &fakeStore{
		cands: []Candidate{
			{ID: "near", Title: "Farm track", Line: line, LengthM: geo.Length(line), Surface: "unpaved", Flags: []string{"gate"}, Confidence: 0.82, LastConfirmedAt: &now, Confirmations30d: 4},
			{ID: "far", Line: []geo.Point{far, {Lat: far.Lat + 0.01, Lng: far.Lng}}, LengthM: 1100, Surface: "paved", Confidence: 0.9},
		},
		barriers: []Barrier{{ID: 1, Point: geo.Point{Lat: 22.7050, Lng: 75.8200}, Kind: "gate"}}, // on the standard route's via point
	}
	r := newRouter(t, gh, store, now)
	var evaluated int
	r.OnCommunityEval = func(found, ev int) { evaluated = ev }
	resp, err := r.Route(context.Background(), Request{From: origin, To: dest})
	if err != nil {
		t.Fatal(err)
	}
	// Forward direction only: the reverse direction fails the lower bound
	// (it heads back toward the origin) and the far shortcut fails the ellipse.
	if evaluated != 1 {
		t.Fatalf("evaluated = %d", evaluated)
	}
	var community *Option
	for i := range resp.Options {
		if len(resp.Options[i].Legs) == 3 {
			community = &resp.Options[i]
		}
	}
	if community == nil {
		t.Fatalf("expected a community option, got %+v", resp.Options)
	}
	if community.Legs[1].Source != "community" || community.Legs[1].ShortcutID != "near" {
		t.Fatalf("legs = %+v", community.Legs)
	}
	pts, _ := geo.Decode(community.Polyline)
	last := len(pts) - 1
	if community.Legs[0].FromIdx != 0 || community.Legs[2].ToIdx != last ||
		community.Legs[0].ToIdx != community.Legs[1].FromIdx || community.Legs[1].ToIdx != community.Legs[2].FromIdx {
		t.Fatalf("legs must tile the polyline: %+v (last=%d)", community.Legs, last)
	}
	if community.Confidence == nil || *community.Confidence != 0.82 || *community.Confirmations30d != 4 {
		t.Fatal("community metadata missing")
	}
	hasGate := false
	for _, w := range community.Warnings {
		hasGate = hasGate || w.Type == "gate"
	}
	if !hasGate {
		t.Fatal("gate flag should become a warning")
	}
	stdGate := false
	for _, w := range resp.Options[0].Warnings {
		stdGate = stdGate || (w.Type == "gate" && w.AtM != nil && *w.AtM > 0)
	}
	if !stdGate {
		t.Fatalf("OSM gate on the standard route should warn: %+v", resp.Options[0].Warnings)
	}
	if fin := community.Instructions[len(community.Instructions)-1]; fin.Sign != SignFinish {
		t.Fatal("instructions must end with arrival")
	}
	finishes := 0
	for _, in := range community.Instructions {
		if in.Sign == SignFinish {
			finishes++
		}
	}
	if finishes != 1 {
		t.Fatalf("intermediate arrivals must be removed, got %d", finishes)
	}
}

func TestOutOfCoverage(t *testing.T) {
	var calls int32
	gh := fakeGH(t, &calls)
	defer gh.Close()
	r := newRouter(t, gh, nil, octoberNoon)
	_, err := r.Route(context.Background(), Request{From: origin, To: geo.Point{Lat: 19.0, Lng: 73.0}})
	var oc *OutOfCoverageError
	if !errors.As(err, &oc) || oc.Which != "to" || oc.NearestRegion != "indore" {
		t.Fatalf("expected out of coverage, got %v", err)
	}
	_, err = r.Route(context.Background(), Request{From: origin, To: geo.Point{Lat: 59.33, Lng: 18.07}})
	if !errors.Is(err, ErrCrossRegion) {
		t.Fatalf("expected cross-region error, got %v", err)
	}
	_, err = r.Route(context.Background(), Request{From: origin, To: dest, Vehicle: "bicycle"})
	var ve *ValidationError
	if !errors.As(err, &ve) {
		t.Fatalf("bicycle is not an India vehicle, got %v", err)
	}
}

func TestRecommendationRules(t *testing.T) {
	c := func(v float64) *float64 { return &v }
	cases := []struct {
		name string
		in   RecommendInput
		want bool
	}{
		{"saves 3 min osm-only", RecommendInput{StdDistanceM: 7100, StdDurationS: 1020, ScDistanceM: 6900, ScDurationS: 840, Daylight: true}, true},
		{"saves 25% distance, 1 min", RecommendInput{StdDistanceM: 10000, StdDurationS: 900, ScDistanceM: 7500, ScDurationS: 840, Daylight: true}, true},
		{"saves too little", RecommendInput{StdDistanceM: 7100, StdDurationS: 1020, ScDistanceM: 6900, ScDurationS: 960, Daylight: true}, false},
		{"low confidence", RecommendInput{StdDistanceM: 7100, StdDurationS: 1020, ScDistanceM: 3400, ScDurationS: 600, Confidence: c(0.55), Daylight: true}, false},
		{"confidence ok", RecommendInput{StdDistanceM: 7100, StdDurationS: 1020, ScDistanceM: 3400, ScDurationS: 600, Confidence: c(0.6), Daylight: true}, true},
		{"night track", RecommendInput{StdDistanceM: 7100, StdDurationS: 1020, ScDistanceM: 3400, ScDurationS: 600, TrackM: 400}, false},
		{"night paved", RecommendInput{StdDistanceM: 7100, StdDurationS: 1020, ScDistanceM: 3400, ScDurationS: 600}, true},
		{"30% shorter but slower still qualifies (SPEC §6.3)", RecommendInput{StdDistanceM: 10000, StdDurationS: 900, ScDistanceM: 7000, ScDurationS: 1000, Daylight: true}, true},
	}
	for _, tc := range cases {
		if got := ShouldRecommendShortcut(tc.in); got != tc.want {
			t.Errorf("%s: got %v", tc.name, got)
		}
	}
}

func TestPlanProfiles(t *testing.T) {
	if p := PlanProfiles("IN", "scooter", "auto", true); p.Standard != "tw_standard" || p.Shortcut != "tw_shortcut" {
		t.Fatalf("%+v", p)
	}
	if p := PlanProfiles("IN", "motorcycle", "adventure", true); p.Shortcut != "tw_adventure" || p.MinCommunityConfidence != 0 {
		t.Fatalf("%+v", p)
	}
	if p := PlanProfiles("IN", "scooter", "standard", true); p.Shortcut != "" {
		t.Fatalf("%+v", p)
	}
	if p := PlanProfiles("SE", "moped", "shortcut", false); p.Standard != "moped_standard" || p.Shortcut != "moped_shortcut" {
		t.Fatalf("%+v", p)
	}
	if p := PlanProfiles("SE", "bicycle", "auto", true); p.Standard != "bike_standard" || p.Shortcut != "bike_shortcut" {
		t.Fatalf("%+v", p)
	}
	if SeasonModel(nil) != nil {
		t.Fatal("no season, no model")
	}
}

func TestSpansAndBreakdown(t *testing.T) {
	line := []geo.Point{{Lat: 0, Lng: 0}, {Lat: 0, Lng: 0.001}, {Lat: 0, Lng: 0.002}, {Lat: 0, Lng: 0.003}, {Lat: 0, Lng: 0.004}}
	d := map[string][]Detail{
		"road_class": {{0, 2, "residential"}, {2, 3, "track"}, {3, 4, "residential"}},
		"surface":    {{0, 1, "asphalt"}, {1, 2, "dirt"}, {2, 4, "missing"}},
	}
	spans := Spans(len(line), d)
	want := []string{SurfPaved, SurfUnpaved, SurfTrack, SurfPaved}
	if len(spans) != 4 {
		t.Fatalf("spans = %+v", spans)
	}
	for i, s := range spans {
		if s.Kind != want[i] {
			t.Fatalf("span %d = %s want %s", i, s.Kind, want[i])
		}
	}
	b := Breakdown(line, spans)
	if b.PavedM < 200 || b.UnpavedM < 100 || b.TrackM < 100 {
		t.Fatalf("breakdown = %+v", b)
	}
	ws := UnpavedWarnings(line, spans, 100)
	if len(ws) != 1 || ws[0].Surface != SurfTrack || *ws[0].LengthM < 200 {
		t.Fatalf("adjacent unpaved+track should merge into one track warning: %+v", ws)
	}
}
