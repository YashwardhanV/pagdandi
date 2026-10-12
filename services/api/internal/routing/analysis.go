package routing

import (
	"fmt"
	"math"
	"strings"

	"pagdandi/api/internal/geo"
)

var unpavedSurfaces = map[string]bool{
	"unpaved": true, "compacted": true, "fine_gravel": true, "gravel": true,
	"ground": true, "dirt": true, "grass": true, "sand": true, "mud": true, "wood": false,
}

// ClassifySurface maps a GraphHopper road_class and surface to paved/unpaved/track.
// A missing surface on a non-track road counts as paved (OSM's implied default).
func ClassifySurface(roadClass, surface string) string {
	if strings.EqualFold(roadClass, "track") {
		return SurfTrack
	}
	if unpavedSurfaces[strings.ToLower(surface)] {
		return SurfUnpaved
	}
	return SurfPaved
}

// detailAt expands detail spans into one value per segment (segment i joins points i and i+1).
func detailAt(details []Detail, nSeg int) []string {
	out := make([]string, nSeg)
	for _, d := range details {
		v := fmt.Sprint(d.Value)
		for i := d.From; i < d.To && i < nSeg; i++ {
			if i >= 0 {
				out[i] = v
			}
		}
	}
	return out
}

// Spans computes merged surface spans over the polyline from GraphHopper details.
func Spans(nPoints int, details map[string][]Detail) []SurfaceSpan {
	if nPoints < 2 {
		return nil
	}
	nSeg := nPoints - 1
	rc := detailAt(details["road_class"], nSeg)
	sf := detailAt(details["surface"], nSeg)
	kinds := make([]string, nSeg)
	for i := range kinds {
		kinds[i] = ClassifySurface(rc[i], sf[i])
	}
	return mergeKinds(kinds, 0)
}

// mergeKinds turns per-segment kinds into spans, offsetting point indexes by off.
func mergeKinds(kinds []string, off int) []SurfaceSpan {
	var spans []SurfaceSpan
	for i, k := range kinds {
		if n := len(spans); n > 0 && spans[n-1].Kind == k {
			spans[n-1].ToIdx = off + i + 1
			continue
		}
		spans = append(spans, SurfaceSpan{FromIdx: off + i, ToIdx: off + i + 1, Kind: k})
	}
	return spans
}

// Breakdown sums span lengths per class.
func Breakdown(line []geo.Point, spans []SurfaceSpan) SurfaceBreakdown {
	cum := geo.CumulativeDistances(line)
	var b SurfaceBreakdown
	var paved, unpaved, track float64
	for _, s := range spans {
		if s.FromIdx < 0 || s.ToIdx >= len(cum) || s.ToIdx < s.FromIdx {
			continue
		}
		d := cum[s.ToIdx] - cum[s.FromIdx]
		switch s.Kind {
		case SurfTrack:
			track += d
		case SurfUnpaved:
			unpaved += d
		default:
			paved += d
		}
	}
	b.PavedM, b.UnpavedM, b.TrackM = int(math.Round(paved)), int(math.Round(unpaved)), int(math.Round(track))
	return b
}

// UnpavedWarnings reports each unpaved/track stretch of at least minLen metres.
// Stretches separated by less than 30 m of paved road are merged; the merged
// stretch is labelled "track" if any part is a track.
func UnpavedWarnings(line []geo.Point, spans []SurfaceSpan, minLen float64) []Warning {
	cum := geo.CumulativeDistances(line)
	type stretch struct {
		from, to float64
		track    bool
	}
	var st []stretch
	for _, s := range spans {
		if s.Kind == SurfPaved || s.ToIdx >= len(cum) {
			continue
		}
		a, b := cum[s.FromIdx], cum[s.ToIdx]
		if n := len(st); n > 0 && a-st[n-1].to < 30 {
			st[n-1].to = b
			st[n-1].track = st[n-1].track || s.Kind == SurfTrack
			continue
		}
		st = append(st, stretch{a, b, s.Kind == SurfTrack})
	}
	var out []Warning
	for _, s := range st {
		if s.to-s.from < minLen {
			continue
		}
		at, ln := int(math.Round(s.from)), int(math.Round(s.to-s.from))
		w := Warning{Type: "unpaved", AtM: &at, LengthM: &ln, Surface: SurfUnpaved}
		if s.track {
			w.Surface = SurfTrack
		}
		out = append(out, w)
	}
	return out
}

// ConvertInstructions maps GraphHopper instructions, shifting point intervals by off.
func ConvertInstructions(in []Instruction, off int) []Instr {
	out := make([]Instr, 0, len(in))
	for _, i := range in {
		out = append(out, Instr{
			Text:      i.Text,
			Street:    i.StreetName,
			DistanceM: int(math.Round(i.Distance)),
			DurationS: int(math.Round(float64(i.Time) / 1000)),
			Sign:      i.Sign,
			Interval:  [2]int{i.Interval[0] + off, i.Interval[1] + off},
			Exit:      i.ExitNumber,
		})
	}
	return out
}

// GraphHopper instruction signs we care about.
const (
	SignContinue = 0
	SignFinish   = 4
	SignVia      = 5
)
