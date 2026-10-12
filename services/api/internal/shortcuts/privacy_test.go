package shortcuts

import (
	"testing"
	"time"

	"pagdandi/api/internal/geo"
)

func straight(n int, stepDeg float64) []TimedPoint {
	t0 := time.Date(2026, 10, 1, 8, 0, 0, 0, time.UTC)
	out := make([]TimedPoint, n)
	for i := range out {
		out[i] = TimedPoint{Point: geo.Point{Lat: 22.7, Lng: 75.8 + float64(i)*stepDeg}, T: t0.Add(time.Duration(i) * 5 * time.Second)}
	}
	return out
}

func TestTrimEnds(t *testing.T) {
	pts := straight(101, 0.0001) // ~10.3 m apart, ~1 km total
	got := TrimEnds(pts, 200)
	if len(got) == 0 {
		t.Fatal("expected trimmed trace")
	}
	if d := geo.Haversine(pts[0].Point, got[0].Point); d < 200 {
		t.Fatalf("start kept within %v m", d)
	}
	if d := geo.Haversine(pts[len(pts)-1].Point, got[len(got)-1].Point); d < 200 {
		t.Fatalf("end kept within %v m", d)
	}
	if TrimEnds(straight(30, 0.0001), 200) != nil {
		t.Fatal("a 300 m trace has nothing left after trimming 200 m each end")
	}
}

func TestCutAround(t *testing.T) {
	pts := straight(101, 0.0001)
	home := pts[50].Point
	pieces := CutAround(pts, []geo.Point{home}, 300, 50)
	if len(pieces) != 2 {
		t.Fatalf("want 2 pieces either side of home, got %d", len(pieces))
	}
	for _, pc := range pieces {
		for _, p := range pc {
			if geo.Haversine(p.Point, home) <= 300 {
				t.Fatal("point inside the private radius survived")
			}
		}
	}
}
