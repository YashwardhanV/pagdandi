package shortcuts

import (
	"time"

	"pagdandi/api/internal/geo"
)

// TimedPoint is a trace fix.
type TimedPoint struct {
	geo.Point
	T time.Time
}

// TrimEnds drops the first and last trimM metres of a trace (SPEC §17).
func TrimEnds(pts []TimedPoint, trimM float64) []TimedPoint {
	if len(pts) < 2 {
		return nil
	}
	cum := make([]float64, len(pts))
	for i := 1; i < len(pts); i++ {
		cum[i] = cum[i-1] + geo.Haversine(pts[i-1].Point, pts[i].Point)
	}
	total := cum[len(cum)-1]
	if total <= 2*trimM {
		return nil
	}
	var out []TimedPoint
	for i, p := range pts {
		if cum[i] >= trimM && cum[i] <= total-trimM {
			out = append(out, p)
		}
	}
	if len(out) < 2 {
		return nil
	}
	return out
}

// CutAround removes every fix within radiusM of any private place and returns
// the remaining pieces that still have at least two points and minLen metres.
func CutAround(pts []TimedPoint, places []geo.Point, radiusM, minLen float64) [][]TimedPoint {
	var pieces [][]TimedPoint
	var cur []TimedPoint
	flush := func() {
		if len(cur) >= 2 {
			line := make([]geo.Point, len(cur))
			for i, p := range cur {
				line[i] = p.Point
			}
			if geo.Length(line) >= minLen {
				pieces = append(pieces, cur)
			}
		}
		cur = nil
	}
	for _, p := range pts {
		private := false
		for _, pl := range places {
			if geo.Haversine(p.Point, pl) <= radiusM {
				private = true
				break
			}
		}
		if private {
			flush()
			continue
		}
		cur = append(cur, p)
	}
	flush()
	return pieces
}
