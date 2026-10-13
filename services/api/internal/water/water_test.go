package water

import (
	"errors"
	"testing"

	"pagdandi/api/internal/geo"
	"pagdandi/api/internal/raster"
	"pagdandi/api/internal/trail"
)

func lakes(t *testing.T) *Router {
	t.Helper()
	g := raster.New(geo.BBox{75, 22, 75.2, 22.1}, 50)
	ring := func(lng0, lat0, lng1, lat1 float64) [][]geo.Point {
		return [][]geo.Point{{{Lat: lat0, Lng: lng0}, {Lat: lat0, Lng: lng1}, {Lat: lat1, Lng: lng1}, {Lat: lat1, Lng: lng0}, {Lat: lat0, Lng: lng0}}}
	}
	// An L-shaped lake (so the straight line crosses land) and a separate pond.
	g.FillPolygon(ring(75.01, 22.01, 75.08, 22.03), raster.Water)
	g.FillPolygon(ring(75.06, 22.01, 75.08, 22.08), raster.Water)
	g.FillPolygon(ring(75.15, 22.05, 75.17, 22.07), raster.Water)
	return New(g, nil)
}

func TestTrailsStayOnWater(t *testing.T) {
	r := lakes(t)
	a := geo.Point{Lat: 22.02, Lng: 75.015}
	b := geo.Point{Lat: 22.075, Lng: 75.07}
	ts, _, err := r.Trails(a, b, 35)
	if err != nil {
		t.Fatal(err)
	}
	if len(ts) == 0 {
		t.Fatal("no trails")
	}
	for _, tr := range ts {
		line := tr.Line()
		for i := 0; i+1 < len(line); i++ {
			// Sample each leg; every sample must be water.
			for k := 0; k <= 10; k++ {
				f := float64(k) / 10
				p := geo.Point{Lat: line[i].Lat + (line[i+1].Lat-line[i].Lat)*f, Lng: line[i].Lng + (line[i+1].Lng-line[i].Lng)*f}
				if r.g.At(p)&raster.Water == 0 {
					t.Fatalf("%s leg %d leaves the water at %v", tr.Kind, i, p)
				}
			}
		}
		if tr.DistanceM < geo.Haversine(a, b) {
			t.Errorf("%s shorter than the straight line", tr.Kind)
		}
		if tr.DurationS <= 0 || tr.Mix["water"] != 1 {
			t.Errorf("%s: bad duration or mix", tr.Kind)
		}
	}
}

func TestDifferentWaterAndNoWater(t *testing.T) {
	r := lakes(t)
	_, _, err := r.Trails(geo.Point{Lat: 22.02, Lng: 75.015}, geo.Point{Lat: 22.06, Lng: 75.16}, 35)
	var we *Error
	if !errors.As(err, &we) || we.Code != "different_water" {
		t.Fatalf("want different_water, got %v", err)
	}
	_, _, err = r.Trails(geo.Point{Lat: 22.095, Lng: 75.12}, geo.Point{Lat: 22.02, Lng: 75.015}, 35)
	if !errors.As(err, &we) || we.Code != "no_water" {
		t.Fatalf("want no_water, got %v", err)
	}
}

func TestLoopsReturnHome(t *testing.T) {
	r := lakes(t)
	start := geo.Point{Lat: 22.02, Lng: 75.015}
	ls, err := r.Loops(start, 3, 35)
	if err != nil {
		t.Fatal(err)
	}
	for _, l := range ls {
		line := l.Line()
		if l.Kind != trail.Loop || geo.Haversine(line[0], line[len(line)-1]) > 60 {
			t.Errorf("loop doesn't come back: %v → %v", line[0], line[len(line)-1])
		}
		if l.DistanceM < 2000 {
			t.Errorf("loop too short: %.0f m", l.DistanceM)
		}
	}
}
