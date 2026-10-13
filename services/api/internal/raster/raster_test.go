package raster

import (
	"path/filepath"
	"testing"

	"pagdandi/api/internal/geo"
)

func square(lng, lat, d float64) []geo.Point {
	return []geo.Point{{Lat: lat, Lng: lng}, {Lat: lat, Lng: lng + d}, {Lat: lat + d, Lng: lng + d}, {Lat: lat + d, Lng: lng}, {Lat: lat, Lng: lng}}
}

func TestFillDilateRoundTrip(t *testing.T) {
	g := New(geo.BBox{75, 22, 75.1, 22.1}, 50)
	// A 0.02° lake with a 0.01° island hole.
	outer := square(75.02, 22.02, 0.02)
	hole := square(75.025, 22.025, 0.01)
	n := g.FillPolygon([][]geo.Point{outer, hole}, Water)
	if n == 0 {
		t.Fatal("nothing filled")
	}
	if g.At(geo.Point{Lat: 22.022, Lng: 75.022})&Water == 0 {
		t.Error("inside the lake should be water")
	}
	if g.At(geo.Point{Lat: 22.03, Lng: 75.03})&Water != 0 {
		t.Error("the island hole should stay dry")
	}
	g.Dilate(Water, NearWater, 4)
	shore := geo.Point{Lat: 22.03, Lng: 75.0415} // ~150 m east of the lake edge
	if g.At(shore)&NearWater == 0 {
		t.Error("150 m from water should be near water")
	}
	if g.At(geo.Point{Lat: 22.03, Lng: 75.06})&NearWater != 0 {
		t.Error("2 km away should not be near water")
	}
	g.DrawLine([]geo.Point{{Lat: 22.08, Lng: 75.0}, {Lat: 22.08, Lng: 75.1}}, River, 0)
	if g.At(geo.Point{Lat: 22.08, Lng: 75.05})&River == 0 {
		t.Error("river line missing")
	}

	p := filepath.Join(t.TempDir(), "g.gz")
	if err := g.Write(p); err != nil {
		t.Fatal(err)
	}
	h, err := Read(p)
	if err != nil {
		t.Fatal(err)
	}
	if h.W != g.W || h.H != g.H || h.Count(Water) != g.Count(Water) || h.Count(NearWater) != g.Count(NearWater) {
		t.Errorf("round trip changed the grid")
	}
}
