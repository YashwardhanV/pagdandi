package mining

import (
	"testing"

	"pagdandi/api/internal/geo"
)

// A road running east, and a rider cutting a 300 m diagonal across a field.
var (
	road  = []geo.Point{{Lat: 22.70, Lng: 75.80}, {Lat: 22.70, Lng: 75.81}}
	field = func(offLat float64) []geo.Point {
		return []geo.Point{
			{Lat: 22.7000, Lng: 75.8000}, {Lat: 22.7000, Lng: 75.8010}, // on the road
			{Lat: 22.7006 + offLat, Lng: 75.8020}, {Lat: 22.7012 + offLat, Lng: 75.8030}, {Lat: 22.7018 + offLat, Lng: 75.8040}, // off it
			{Lat: 22.7024 + offLat, Lng: 75.8050},
			{Lat: 22.7000, Lng: 75.8060}, // back on the road
		}
	}
)

func TestOffNetwork(t *testing.T) {
	segs := OffNetwork(field(0), road)
	if len(segs) != 1 {
		t.Fatalf("want one off-network stretch, got %d", len(segs))
	}
	if l := geo.Length(segs[0]); l < MinStretchM {
		t.Fatalf("stretch too short: %v", l)
	}
	// A trace that hugs the road has no off-network part.
	if got := OffNetwork(road, road); len(got) != 0 {
		t.Fatalf("on-road trace produced %d stretches", len(got))
	}
}

func TestCellsAreContiguous(t *testing.T) {
	cs := Cells([]geo.Point{{Lat: 22.70, Lng: 75.80}, {Lat: 22.70, Lng: 75.803}})
	if len(cs) < 5 {
		t.Fatalf("expected a sequence of res-11 cells over ~300 m, got %d", len(cs))
	}
	for i := 1; i < len(cs); i++ {
		if cs[i] == cs[i-1] {
			t.Fatal("consecutive duplicates must be collapsed")
		}
	}
}

func TestLinkKAnonymity(t *testing.T) {
	mk := func(user string, off float64) Segment {
		s := OffNetwork(field(off), road)[0]
		return Segment{UserID: user, RegionID: "indore", Cells: Cells(s), Line: s}
	}
	segs := []Segment{
		mk("u1", 0), mk("u2", 0.00005), mk("u3", -0.00005), // three riders, a few metres apart
		mk("u1", 0.00002), // the same rider again does not count twice
	}
	// A different path far away, ridden by two people.
	other := []geo.Point{{Lat: 22.75, Lng: 75.90}, {Lat: 22.752, Lng: 75.902}}
	segs = append(segs,
		Segment{UserID: "u4", RegionID: "indore", Cells: Cells(other), Line: other},
		Segment{UserID: "u5", RegionID: "indore", Cells: Cells(other), Line: other})

	seqs := Link(segs)
	if len(seqs) != 2 {
		t.Fatalf("want 2 sequences, got %d", len(seqs))
	}
	if seqs[0].DistinctUsers != 3 || len(seqs[0].Members) != 4 {
		t.Fatalf("field path: users=%d members=%d", seqs[0].DistinctUsers, len(seqs[0].Members))
	}
	if seqs[1].DistinctUsers != 2 {
		t.Fatalf("second path should have 2 users (below k=3), got %d", seqs[1].DistinctUsers)
	}
}

func TestGPX(t *testing.T) {
	b := string(gpx(road, nil))
	if len(b) < 50 || b[:5] != "<?xml" {
		t.Fatalf("bad gpx: %s", b)
	}
}
