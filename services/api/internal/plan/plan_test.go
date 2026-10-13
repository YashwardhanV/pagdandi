package plan

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"pagdandi/api/internal/features"
	"pagdandi/api/internal/geo"
	"pagdandi/api/internal/gh"
	"pagdandi/api/internal/land"
	"pagdandi/api/internal/raster"
	"pagdandi/api/internal/region"
	"pagdandi/api/internal/trail"
)

// A synthetic town: A at the west, B 6 km east. GraphHopper is faked: each
// trail kind (recognised by its distance_influence) gets a different road.
var (
	A = geo.Point{Lat: 22.70, Lng: 75.80}
	B = geo.Point{Lat: 22.70, Lng: 75.858}
)

func line(pts ...geo.Point) [][]float64 {
	out := [][]float64{}
	for i := 0; i+1 < len(pts); i++ {
		for k := 0; k < 20; k++ {
			f := float64(k) / 20
			out = append(out, []float64{pts[i].Lng + (pts[i+1].Lng-pts[i].Lng)*f, pts[i].Lat + (pts[i+1].Lat-pts[i].Lat)*f, 550 + 10*float64(k%5)})
		}
	}
	last := pts[len(pts)-1]
	return append(out, []float64{last.Lng, last.Lat, 550})
}

func fakeGH(t *testing.T) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/health" {
			_, _ = io.WriteString(w, "OK")
			return
		}
		var req gh.Request
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatal(err)
		}
		di := 0.0
		if req.CustomModel != nil && req.CustomModel.DistanceInfluence != nil {
			di = *req.CustomModel.DistanceInfluence
		}
		var coords [][]float64
		roadClass, surface := "primary", "asphalt"
		switch {
		case len(req.Points) == 3: // via-point detours (scenic / fresh): go round the north side
			v := geo.Point{Lat: req.Points[1][1], Lng: req.Points[1][0]}
			coords = line(A, v, B)
			roadClass = "tertiary"
		case di == 600: // shortest: straight through
			coords = line(A, B)
			roadClass = "unclassified"
		case di == 70: // kaccha: a dirt track to the south
			coords = line(A, geo.Point{Lat: 22.69, Lng: 75.83}, B)
			roadClass, surface = "track", "dirt"
		case di == 60: // quiet: village lanes, south
			coords = line(A, geo.Point{Lat: 22.685, Lng: 75.829}, B)
			roadClass = "residential"
		default: // usual: the highway bulges north
			coords = line(A, geo.Point{Lat: 22.715, Lng: 75.829}, B)
		}
		pts := make([]geo.Point, len(coords))
		for i, c := range coords {
			pts[i] = geo.Point{Lng: c[0], Lat: c[1]}
		}
		d := geo.Length(pts)
		n := len(coords) - 1
		resp := map[string]any{"paths": []any{map[string]any{
			"distance": d, "time": int64(d / 12 * 1000), "ascend": 40, "descend": 30,
			"points": map[string]any{"coordinates": coords},
			"instructions": []any{
				map[string]any{"text": "Continue", "distance": d, "time": 1000, "sign": 0, "interval": []int{0, n}},
				map[string]any{"text": "Arrive", "distance": 0, "time": 0, "sign": 4, "interval": []int{n, n}},
			},
			"details": map[string]any{
				"road_class":    []any{[]any{0, n, roadClass}},
				"surface":       []any{[]any{0, n, surface}},
				"track_type":    []any{[]any{0, n, "missing"}},
				"osm_way_id":    []any{[]any{0, n, 42}},
				"urban_density": []any{[]any{0, n, "rural"}},
			},
		}}}
		_ = json.NewEncoder(w).Encode(resp)
	}))
}

func testPlanner(t *testing.T, ghURL string) *Planner {
	t.Helper()
	dir := t.TempDir()
	g := raster.New(geo.BBox{75.7, 22.6, 75.95, 22.8}, 50)
	// A lake north of the corridor, so a northern via-point is scenic.
	g.FillPolygon([][]geo.Point{{{Lat: 22.72, Lng: 75.82}, {Lat: 22.72, Lng: 75.84}, {Lat: 22.735, Lng: 75.84}, {Lat: 22.735, Lng: 75.82}, {Lat: 22.72, Lng: 75.82}}}, raster.Water)
	g.Dilate(raster.Water, raster.NearWater, 4)
	if err := g.Write(filepath.Join(dir, features.RasterFile)); err != nil {
		t.Fatal(err)
	}
	anchors := []features.Anchor{{Lat: 22.718, Lng: 75.83, Kind: "water", Name: "Test Lake", W: 0.9}}
	fresh := []features.Fresh{{ID: 42, Highway: "track", Month: "2026-05", LenM: 900, Line: [][2]float64{{75.83, 22.68}, {75.838, 22.68}}}}
	_ = features.WriteJSON(dir, features.AnchorsFile, anchors)
	_ = features.WriteJSON(dir, features.FreshFile, fresh)
	_ = features.WriteJSON(dir, features.GemsFile, []features.Gem{})
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	l, err := land.Load(dir, log)
	if err != nil {
		t.Fatal(err)
	}
	reg := &region.Region{ID: "test", Name: "Test", Center: [2]float64{22.7, 75.83}, RadiusKm: 50, Timezone: "Asia/Kolkata"}
	return &Planner{GH: gh.New(ghURL), Land: l, Region: reg, Log: log}
}

func TestGoGivesDistinctHonestTrails(t *testing.T) {
	srv := fakeGH(t)
	defer srv.Close()
	p := testPlanner(t, srv.URL)
	stray := 4.0
	res, err := p.Go(context.Background(), GoRequest{From: A, To: B, Vehicle: "bike", StrayKm: &stray, Depart: time.Date(2026, 1, 10, 9, 0, 0, 0, time.UTC)})
	if err != nil {
		t.Fatal(err)
	}
	kinds := map[string]*trail.Trail{}
	for _, tr := range res.Trails {
		kinds[tr.Kind] = tr
		if tr.Name == "" || tr.Tagline == "" || tr.ID == "" {
			t.Errorf("%s: missing name/tagline/id", tr.Kind)
		}
	}
	if res.Trails[0].Kind != trail.Usual {
		t.Fatalf("the usual must come first, got %s", res.Trails[0].Kind)
	}
	if s := kinds[trail.Shortest]; s == nil || s.DeltaM >= 0 {
		t.Errorf("want a shorter Shortcut, got %+v", s)
	}
	k := kinds[trail.Kaccha]
	if k == nil || k.Mix["dirt"] < 0.9 {
		t.Fatalf("want a dirt Kaccha trail, got %+v", k)
	}
	if len(k.Warnings) == 0 || k.Warnings[0].Kind != "unpaved" {
		t.Errorf("kaccha should warn about unpaved road: %+v", k.Warnings)
	}
	if q := kinds[trail.Quiet]; q == nil || q.Quiet <= res.Trails[0].Quiet {
		t.Errorf("quiet lanes must be quieter than the usual")
	}
	if f := kinds[trail.Fresh]; f == nil && kinds[trail.Kaccha].FreshM == 0 {
		t.Errorf("fresh way 42 should show up somewhere")
	}
	for _, tr := range res.Trails {
		if tr.Kind != trail.Usual && tr.DistanceM > res.Trails[0].DistanceM+stray*1000+1 {
			t.Errorf("%s breaks the detour budget", tr.Kind)
		}
	}
}

func TestZeroStrayOnlyShorterOrUsual(t *testing.T) {
	srv := fakeGH(t)
	defer srv.Close()
	p := testPlanner(t, srv.URL)
	zero := 0.0
	res, err := p.Go(context.Background(), GoRequest{From: A, To: B, Vehicle: "car", StrayKm: &zero})
	if err != nil {
		t.Fatal(err)
	}
	for _, tr := range res.Trails {
		if tr.DistanceM > res.Trails[0].DistanceM+1 {
			t.Errorf("%s is longer than the usual with no budget", tr.Kind)
		}
	}
}

func TestRejectsOutsideAndUnknownVehicle(t *testing.T) {
	p := testPlanner(t, "http://127.0.0.1:1")
	far := geo.Point{Lat: 28.6, Lng: 77.2}
	_, err := p.Go(context.Background(), GoRequest{From: A, To: far, Vehicle: "bike"})
	if pe, ok := err.(*Error); !ok || pe.Code != "outside" {
		t.Errorf("want outside, got %v", err)
	}
	_, err = p.Go(context.Background(), GoRequest{From: A, To: B, Vehicle: "hovercraft"})
	if pe, ok := err.(*Error); !ok || pe.Code != "bad_request" {
		t.Errorf("want bad_request, got %v", err)
	}
}

func TestModelsOnlyLowerPriorities(t *testing.T) {
	// LM stays valid only if request models never raise priority above 1.
	for _, v := range Vehicles {
		if v.Water {
			continue
		}
		for _, k := range []string{trail.Usual, trail.Shortest, trail.Kaccha, trail.Quiet, trail.Scenic, trail.Fresh, trail.Loop} {
			for _, s := range Model(k, v).Priority {
				var f float64
				if err := json.Unmarshal([]byte(s.MultiplyBy), &f); err != nil || f > 1 || f < 0 {
					t.Errorf("%s/%s: priority %q must be in [0,1]", v.ID, k, s.MultiplyBy)
				}
			}
		}
	}
}

func TestPickIsDeterministicPerSeed(t *testing.T) {
	var ps []Place
	for i := 0; i < 40; i++ {
		ps = append(ps, Place{ID: string(rune('a' + i%26)), Score: float64(i%10) / 10, DistanceKm: float64(i)})
	}
	a := pick(ps, 40, 7, 10)
	b := pick(ps, 40, 7, 10)
	c := pick(ps, 40, 8, 10)
	if len(a) != 10 || a[0].ID != b[0].ID || a[9].ID != b[9].ID || a[0].DistanceKm != b[0].DistanceKm {
		t.Fatal("same seed must give the same surprise")
	}
	same := true
	for i := range a {
		if a[i].DistanceKm != c[i].DistanceKm {
			same = false
		}
	}
	if same {
		t.Error("a new seed should shuffle")
	}
}

func TestSurfaceClass(t *testing.T) {
	cases := map[[3]string]string{
		{"primary", "asphalt", ""}:        "paved",
		{"track", "missing", "grade1"}:    "rough",
		{"track", "missing", "grade3"}:    "dirt",
		{"residential", "missing", ""}:    "paved",
		{"unclassified", "gravel", ""}:    "rough",
		{"tertiary", "ground", "missing"}: "dirt",
	}
	for in, want := range cases {
		if got := surfaceClass(in[0], in[1], in[2]); got != want {
			t.Errorf("%v: got %s want %s", in, got, want)
		}
	}
}
