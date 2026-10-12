// Package integration exercises the real stack: PostGIS, Redis and a
// GraphHopper serving the synthetic-town fixture (SPEC §14).
//
//	TEST_DATABASE_URL        postgres://…/pagdandi_test (required, else skipped)
//	TEST_REDIS_URL           optional; in-memory cache otherwise
//	PAGDANDI_FIXTURE_GH_URL  GraphHopper on the fixture graph (`make test-gh` starts it)
package integration

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"testing"
	"time"

	"pagdandi/api/internal/geo"
	"pagdandi/api/internal/routing"
	"pagdandi/api/internal/store"
)

// Fixture geometry (tests/fixtures/synthetic-town.osm).
var (
	fxA       = geo.Point{Lat: 23.0000, Lng: 77.0000}
	fxC       = geo.Point{Lat: 23.0060, Lng: 77.0060}
	fxGate    = geo.Point{Lat: 23.0030, Lng: 77.0030}
	fxPrivate = geo.Point{Lat: 23.0035, Lng: 77.0022}
	fxMotor   = geo.Point{Lat: 23.0022, Lng: 77.0038}
	fxMil     = geo.Point{Lat: 23.0048, Lng: 77.0012}
)

func testDB(t *testing.T) *store.DB {
	t.Helper()
	url := os.Getenv("TEST_DATABASE_URL")
	if url == "" {
		t.Skip("TEST_DATABASE_URL not set")
	}
	ctx := context.Background()
	db, err := store.Open(ctx, url)
	if err != nil {
		t.Skipf("test database unavailable: %v", err)
	}
	if err := db.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(db.Close)
	return db
}

func fixtureGH(t *testing.T) *routing.GH {
	t.Helper()
	url := os.Getenv("PAGDANDI_FIXTURE_GH_URL")
	if url == "" {
		t.Skip("PAGDANDI_FIXTURE_GH_URL not set (run `make test-gh`)")
	}
	gh := routing.NewGH(url)
	if err := gh.Health(context.Background()); err != nil {
		t.Skipf("fixture GraphHopper not healthy: %v", err)
	}
	return gh
}

func nearLine(t *testing.T, line []geo.Point, p geo.Point) float64 {
	t.Helper()
	return geo.Project(line, p).DistM
}

// TestFixtureProfiles is the deterministic profile test of SPEC §14: standard
// avoids the track, shortcut takes it, every profile avoids private, motorway
// and military roads.
func TestFixtureProfiles(t *testing.T) {
	gh := fixtureGH(t)
	ctx := context.Background()
	route := func(profile string) []geo.Point {
		res, err := gh.Route(ctx, routing.RouteRequest{
			Points:  [][2]float64{{fxA.Lng, fxA.Lat}, {fxC.Lng, fxC.Lat}},
			Profile: profile, Details: []string{"road_class", "surface"},
		})
		if err != nil {
			t.Fatalf("%s: %v", profile, err)
		}
		line, err := res.Paths[0].Line()
		if err != nil {
			t.Fatal(err)
		}
		return line
	}
	forbidden := map[string]geo.Point{"private lane": fxPrivate, "expressway": fxMotor, "cantonment road": fxMil}
	for _, p := range []string{"tw_standard", "tw_shortcut", "tw_shortcut_night", "tw_adventure"} {
		line := route(p)
		for name, pt := range forbidden {
			if d := nearLine(t, line, pt); d < 60 {
				t.Errorf("%s routes over the %s (%.0f m from its midpoint)", p, name, d)
			}
		}
		onTrack := nearLine(t, line, fxGate) < 15
		switch p {
		case "tw_standard", "tw_shortcut_night":
			if onTrack {
				t.Errorf("%s must avoid the kaccha track", p)
			}
			if l := geo.Length(line); l < 6000 {
				t.Errorf("%s should go round the ring (got %.0f m)", p, l)
			}
		case "tw_shortcut", "tw_adventure":
			if !onTrack {
				t.Errorf("%s must take the track", p)
			}
		}
	}
}

type fixtureLookup struct{}

func (fixtureLookup) RegionAt(context.Context, geo.Point) (string, bool, error) { return "indore", true, nil }

// TestFixtureRouterWarnsAboutGate runs the full orchestrator against the
// fixture graph and the barrier index loaded by `datactl fixture`.
func TestFixtureRouterWarnsAboutGate(t *testing.T) {
	gh := fixtureGH(t)
	db := testDB(t)
	set := loadRegions(t)
	noon := time.Date(2026, 10, 12, 6, 30, 0, 0, time.UTC) // 12:00 IST, dry season
	r := &routing.Router{Regions: set, Lookup: fixtureLookup{}, Graphs: map[string]*routing.GH{"IN": gh}, Store: db,
		Now: func() time.Time { return noon }}
	resp, err := r.Route(context.Background(), routing.Request{From: fxA, To: fxC})
	if err != nil {
		t.Fatal(err)
	}
	if len(resp.Options) < 2 {
		t.Fatalf("expected usual way and a shortcut, got %d options", len(resp.Options))
	}
	std, sc := resp.Options[0], resp.Options[1]
	if std.Kind != "standard" || std.SurfaceBreakdown.TrackM != 0 {
		t.Fatalf("standard option wrong: %+v", std.SurfaceBreakdown)
	}
	if sc.SurfaceBreakdown.TrackM == 0 {
		t.Fatalf("shortcut should be on the track: %+v", sc.SurfaceBreakdown)
	}
	gate := false
	for _, w := range sc.Warnings {
		gate = gate || w.Type == "gate"
	}
	if !gate {
		t.Fatalf("gate on the track must produce a warning, got %+v", sc.Warnings)
	}
	if resp.Recommended != sc.ID {
		t.Fatalf("a 6 km saving in daylight should be recommended, got %s", resp.Recommended)
	}
	b, _ := json.Marshal(resp)
	if len(b) == 0 {
		t.Fatal("response must marshal")
	}
}

func TestReadyz(t *testing.T) {
	h := newAPI(t)
	res := h.do(t, http.MethodGet, "/healthz", nil, nil)
	if res.Code != 200 {
		t.Fatalf("healthz = %d", res.Code)
	}
}
