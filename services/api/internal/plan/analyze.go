package plan

import (
	"fmt"
	"math"
	"sort"
	"strings"
	"time"

	"pagdandi/api/internal/features"
	"pagdandi/api/internal/geo"
	"pagdandi/api/internal/gh"
	"pagdandi/api/internal/land"
	"pagdandi/api/internal/raster"
	"pagdandi/api/internal/sun"
	"pagdandi/api/internal/trail"
)

// Details requested from GraphHopper for every route.
var details = []string{"road_class", "surface", "track_type", "osm_way_id", "urban_density"}

// busyness of a road class: 1 = everyone drives here, 0 = nobody.
var busy = map[string]float64{
	"motorway": 1, "trunk": 1, "primary": 0.9, "secondary": 0.7, "tertiary": 0.5,
	"unclassified": 0.25, "residential": 0.35, "living_street": 0.2, "service": 0.2, "road": 0.4,
	"track": 0.05, "path": 0.05, "footway": 0.1, "cycleway": 0.1, "bridleway": 0.05, "steps": 0.1, "pedestrian": 0.2,
}

// surfaceClass maps OSM surface/road class/track type to paved | rough | dirt.
func surfaceClass(roadClass, surface, track string) string {
	switch surface {
	case "asphalt", "concrete", "paved", "paving_stones", "cement", "sett", "metal", "wood":
		return "paved"
	case "compacted", "fine_gravel", "gravel", "unpaved", "cobblestone", "unhewn_cobblestone", "pebblestone":
		return "rough"
	case "dirt", "ground", "sand", "grass", "mud", "earth":
		return "dirt"
	}
	switch roadClass {
	case "track":
		if track == "grade1" {
			return "rough"
		}
		return "dirt"
	case "path", "bridleway":
		return "dirt"
	}
	return "paved"
}

// Ctx is what analysis needs besides the path.
type Ctx struct {
	Land    *land.Land
	Vehicle Vehicle
	Depart  time.Time
	Loc     *time.Location
}

type segInfo struct {
	roadClass, surface, track, urban string
	way                              int64
}

// Analyze scores a GraphHopper path. The planner sets Kind, Tagline and deltas.
func Analyze(p *gh.Path, c Ctx) *trail.Trail {
	coords := p.Points.Coordinates
	pts := make([]geo.Point, len(coords))
	var ele []float64
	hasEle := len(coords) > 0 && len(coords[0]) >= 3
	for i, co := range coords {
		pts[i] = geo.Point{Lng: co[0], Lat: co[1]}
		if hasEle {
			ele = append(ele, co[2])
		}
	}
	t := &trail.Trail{DistanceM: math.Round(p.Distance), DurationS: math.Round(float64(p.Time) / 1000),
		ClimbM: math.Round(p.Ascend), DescentM: math.Round(p.Descend)}
	t.SetLine(pts)
	cum := geo.CumulativeDistances(pts)
	total := cum[len(cum)-1]
	if total <= 0 {
		return t
	}

	// Per-segment attributes from path details.
	segs := make([]segInfo, max(0, len(pts)-1))
	apply := func(name string, set func(*segInfo, gh.Detail)) {
		for _, d := range p.Details[name] {
			for k := d.From; k < d.To && k < len(segs); k++ {
				set(&segs[k], d)
			}
		}
	}
	apply("road_class", func(s *segInfo, d gh.Detail) { s.roadClass = d.String() })
	apply("surface", func(s *segInfo, d gh.Detail) { s.surface = d.String() })
	apply("track_type", func(s *segInfo, d gh.Detail) { s.track = d.String() })
	apply("osm_way_id", func(s *segInfo, d gh.Detail) { s.way = d.Int() })
	apply("urban_density", func(s *segInfo, d gh.Detail) { s.urban = d.String() })
	urban := make([]bool, len(segs))
	for i, sg := range segs {
		urban[i] = sg.urban == "city" || sg.urban == "residential"
	}

	// Surfaces, quietness, fresh roads, rough tracks.
	mix := map[string]float64{}
	var busySum, roughM float64
	var freshSince string
	for i, s := range segs {
		l := cum[i+1] - cum[i]
		cls := surfaceClass(s.roadClass, s.surface, s.track)
		mix[cls] += l
		t.Surfaces = appendSpan(t.Surfaces, cls, cum[i], cum[i+1], "")
		w, ok := busy[s.roadClass]
		if !ok {
			w = 0.4
		}
		busySum += w * l
		if s.roadClass == "track" && (s.track == "grade4" || s.track == "grade5") {
			roughM += l
		}
		if in(s.roadClass, "track", "path", "bridleway", "unclassified") && !urban[i] {
			t.BackroadsM += l
		}
		if f, ok := c.Land.FreshBy[s.way]; ok && s.way != 0 {
			t.FreshM += l
			t.FreshSpans = appendSpan(t.FreshSpans, "fresh", cum[i], cum[i+1], "On the map since "+monthLabel(f.Month))
			if freshSince == "" || f.Month < freshSince {
				freshSince = f.Month
			}
		}
	}
	t.FreshM = math.Round(t.FreshM)
	t.BackroadsM = math.Round(t.BackroadsM)
	t.FreshSince = freshSince
	t.Mix = map[string]float64{}
	for k, v := range mix {
		t.Mix[k] = trail.Round(v/total, 3)
	}
	t.Surfaces = roundSpans(mergeShort(t.Surfaces, 60))
	t.FreshSpans = roundSpans(t.FreshSpans)

	// Scenery from the land grid, sampled every 50 m.
	scen := scenery(c.Land, pts, cum, ele, urban)
	t.Highlights = scen.highlights
	built := scen.builtShare
	t.Quiet = trail.Round(clamp((1-busySum/total)*(1-0.35*built), 0, 1), 2)
	hills := 0.0
	if km := total / 1000; km > 0.5 {
		hills = clamp((t.ClimbM+t.DescentM)/2/km/22, 0, 1)
	}
	t.ScenicS = trail.Round(clamp(0.85*scen.mean+0.25*hills+0.1*scen.views, 0, 1), 2)
	classes := make([]string, len(segs))
	for i, sg := range segs {
		classes[i] = sg.roadClass
	}
	t.Twisty = trail.Round(twisty(pts, cum, classes, urban, c.Land.Grid), 2)
	if t.Twisty >= 0.45 {
		t.Highlights = append(t.Highlights, trail.Span{Kind: "twisty", From: 0, To: math.Round(total), Label: "Twisty, fun to ride"})
	}

	// Elevation profile (~80 samples).
	if hasEle {
		n := 80
		for k := 0; k <= n; k++ {
			d := total * float64(k) / float64(n)
			t.Elevation = append(t.Elevation, [2]float64{math.Round(d), math.Round(interp(cum, ele, d))})
		}
	}

	// Steps.
	for _, in := range p.Instructions {
		idx := in.Interval[0]
		if idx >= len(pts) {
			idx = len(pts) - 1
		}
		t.Steps = append(t.Steps, trail.Step{Text: in.Text, Street: in.StreetName, Sign: in.Sign,
			DistanceM: math.Round(in.Distance), DurationS: math.Round(float64(in.Time) / 1000),
			At: [2]float64{trail.Round(pts[idx].Lng, 5), trail.Round(pts[idx].Lat, 5)}, Index: idx, AlongM: math.Round(cum[idx])})
	}

	t.Warnings = warnings(t, c, roughM, maxGrade(cum, ele))
	return t
}

type sceneryResult struct {
	mean       float64 // 0..1 average scenery value
	views      float64 // 0..1 share of viewpoint/waterfall/heritage passes
	builtShare float64
	highlights []trail.Span
}

// scenery samples the land grid along the route and finds named stretches.
func scenery(l *land.Land, pts []geo.Point, cum []float64, ele []float64, urban []bool) sceneryResult {
	total := cum[len(cum)-1]
	step := 50.0
	n := int(total/step) + 1
	var sum, built float64
	cats := make([]string, n)
	for k := 0; k < n; k++ {
		d := math.Min(total, float64(k)*step)
		p := pointAt(pts, cum, d)
		f := l.Grid.At(p)
		v, cat := 0.12, ""
		switch {
		case f&raster.NearWater != 0:
			v, cat = 1.0, "water"
		case f&raster.Protected != 0:
			v, cat = 0.9, "wild"
		case f&raster.Forest != 0:
			v, cat = 0.85, "forest"
		case f&raster.Green != 0:
			v = 0.5
		case f&raster.Farmland != 0:
			v = 0.3
		}
		seg := sort.SearchFloat64s(cum, d) - 1
		inTown := f&raster.BuiltUp != 0 || (seg >= 0 && seg < len(urban) && urban[seg])
		if inTown {
			v *= 0.35
			built++
			// In town, a river line is usually a drain; only lakes count.
			if cat == "forest" || cat == "wild" || (cat == "water" && !waterWithin(l.Grid, p, 2)) {
				cat = ""
			}
		}
		sum += v
		cats[k] = cat
	}
	res := sceneryResult{mean: sum / float64(n), builtShare: built / float64(n)}

	// Runs of the same category, tolerating 2-sample gaps, at least 400 m.
	type run struct {
		cat      string
		from, to int
	}
	var runs []run
	for k := 0; k < n; k++ {
		if cats[k] == "" {
			continue
		}
		if len(runs) > 0 && runs[len(runs)-1].cat == cats[k] && k-runs[len(runs)-1].to <= 8 {
			runs[len(runs)-1].to = k
			continue
		}
		runs = append(runs, run{cats[k], k, k})
	}
	var hs []trail.Span
	for _, r := range runs {
		from, to := float64(r.from)*step, math.Min(total, float64(r.to)*step)
		if to-from < 400 {
			continue
		}
		mid := pointAt(pts, cum, (from+to)/2)
		label := ""
		switch r.cat {
		case "water":
			if name := l.WaterNear(mid, 900); name != "" {
				label = "Along " + name
			} else {
				label = "Along the water"
			}
		case "forest":
			if a, ok := l.NamedNear(mid, 1500, "forest"); ok {
				label = "Through " + a.Name
			} else {
				label = "Through the forest"
			}
		case "wild":
			if a, ok := l.NamedNear(mid, 2500, "forest"); ok {
				label = "Through " + a.Name
			} else {
				label = "Through protected wild land"
			}
		}
		hs = append(hs, trail.Span{Kind: r.cat, From: math.Round(from), To: math.Round(to), Label: label})
	}

	// Named viewpoints, waterfalls, peaks and heritage within 300 m of the route.
	seen := map[string]bool{}
	views := 0
	if len(pts) > 1 {
		bb := bboxOf(pts)
		pad := 0.004
		cands := l.Anchors.InBBox(geo.BBox{bb[0] - pad, bb[1] - pad, bb[2] + pad, bb[3] + pad})
		for _, a := range cands {
			if a.Name == "" || seen[a.Name] || !in(a.Kind, "viewpoint", "waterfall", "peak", "heritage") {
				continue
			}
			pr := geo.Project(pts, a.Point())
			if pr.DistM > 300 {
				continue
			}
			seen[a.Name] = true
			views++
			label := "Past " + a.Name
			if in(a.Name, "Viewpoint", "Waterfall", "Cave", "Peak") {
				label = "Past a " + strings.ToLower(a.Name)
			}
			hs = append(hs, trail.Span{Kind: "view", From: math.Round(pr.AlongM), To: math.Round(pr.AlongM), Label: label})
		}
	}
	res.views = clamp(float64(views)/3, 0, 1)

	// Biggest climbs.
	if len(ele) == len(pts) && total > 1000 {
		if from, to, gain := biggestClimb(cum, ele); gain >= 60 {
			hs = append(hs, trail.Span{Kind: "hills", From: math.Round(from), To: math.Round(to), Label: fmt.Sprintf("Climbs %d m", int(math.Round(gain/10)*10))})
		}
	}

	// Keep the 5 most notable (longest; views count as 1 km), in route order.
	sort.SliceStable(hs, func(i, j int) bool { return spanWeight(hs[i]) > spanWeight(hs[j]) })
	if len(hs) > 5 {
		hs = hs[:5]
	}
	sort.SliceStable(hs, func(i, j int) bool { return hs[i].From < hs[j].From })
	res.highlights = dedupeHighlights(hs)
	return res
}

// dedupeHighlights drops repeats of the same label (a lake passed twice).
func dedupeHighlights(hs []trail.Span) []trail.Span {
	var out []trail.Span
	seen := map[string]bool{}
	for _, h := range hs {
		if seen[h.Label] {
			continue
		}
		seen[h.Label] = true
		out = append(out, h)
	}
	return out
}

// waterWithin reports whether a lake/reservoir cell (water, not river) lies within r cells of p.
func waterWithin(g *raster.Grid, p geo.Point, r int) bool {
	x, y, ok := g.XY(p)
	if !ok {
		return false
	}
	for dy := -r; dy <= r; dy++ {
		for dx := -r; dx <= r; dx++ {
			if c := g.Get(x+dx, y+dy); c&raster.Water != 0 && c&raster.River == 0 {
				return true
			}
		}
	}
	return false
}

func spanWeight(s trail.Span) float64 {
	switch s.Kind {
	case "view":
		return 1200
	case "hills":
		return 900
	}
	return s.To - s.From
}

// biggestClimb finds the largest continuous elevation gain (smoothed).
func biggestClimb(cum, ele []float64) (from, to, gain float64) {
	sm := smooth(ele, 5)
	lowIdx := 0
	bestFrom, bestTo, best := 0, 0, 0.0
	for i := 1; i < len(sm); i++ {
		if sm[i] < sm[lowIdx] {
			lowIdx = i
		}
		if g := sm[i] - sm[lowIdx]; g > best {
			best, bestFrom, bestTo = g, lowIdx, i
		}
	}
	return cum[bestFrom], cum[bestTo], best
}

func smooth(v []float64, w int) []float64 {
	out := make([]float64, len(v))
	for i := range v {
		lo, hi := max(0, i-w), min(len(v)-1, i+w)
		s := 0.0
		for k := lo; k <= hi; k++ {
			s += v[k]
		}
		out[i] = s / float64(hi-lo+1)
	}
	return out
}

// maxGrade is the steepest climb over any 200 m window, in percent.
func maxGrade(cum, ele []float64) float64 {
	if len(ele) != len(cum) || len(cum) < 2 {
		return 0
	}
	total := cum[len(cum)-1]
	best := 0.0
	for d := 0.0; d+200 <= total; d += 50 {
		g := (interp(cum, ele, d+200) - interp(cum, ele, d)) / 200 * 100
		best = math.Max(best, g)
	}
	return best
}

// twisty is bend per km on open roads, scaled to 0..1. Junction turns (> 45°)
// and town streets don't count: a twisty road bends, it doesn't turn corners.
func twisty(pts []geo.Point, cum []float64, classes []string, urban []bool, g *raster.Grid) float64 {
	total := cum[len(cum)-1]
	if total < 1500 {
		return 0
	}
	step := 30.0
	var turn, rural float64
	prev := -1.0
	for d := 0.0; d+step <= total; d += step {
		a, b := pointAt(pts, cum, d), pointAt(pts, cum, d+step)
		h := geo.Bearing(a, b)
		seg := sort.SearchFloat64s(cum, d+step/2) - 1
		cls := ""
		if seg >= 0 && seg < len(classes) {
			cls = classes[seg]
		}
		town := seg >= 0 && seg < len(urban) && urban[seg]
		if town || g.At(a)&raster.BuiltUp != 0 || in(cls, "residential", "service", "living_street", "footway", "pedestrian", "steps") {
			prev = -1
			continue
		}
		rural += step
		if prev >= 0 {
			dh := math.Abs(math.Mod(h-prev+540, 360) - 180)
			if dh > 6 && dh <= 45 {
				turn += dh
			}
		}
		prev = h
	}
	if rural < 1000 {
		return 0
	}
	perKm := turn / (rural / 1000)
	return clamp((perKm-40)/360, 0, 1)
}

func warnings(t *trail.Trail, c Ctx, roughM, grade float64) []trail.Note {
	var out []trail.Note
	unpavedKm := (t.Mix["rough"] + t.Mix["dirt"]) * t.DistanceM / 1000
	dirtKm := t.Mix["dirt"] * t.DistanceM / 1000
	loc := c.Loc
	if loc == nil {
		loc = time.UTC
	}
	depart := c.Depart.In(loc)
	if unpavedKm >= 0.8 {
		out = append(out, trail.Note{Kind: "unpaved", Text: fmt.Sprintf("%.1f km unpaved: expect dust, ruts and slow going", unpavedKm)})
	}
	if m := depart.Month(); m >= time.June && m <= time.September && dirtKm >= 0.5 {
		out = append(out, trail.Note{Kind: "monsoon", Text: "Monsoon: dirt stretches may be muddy or flooded"})
	}
	if roughM >= 200 {
		txt := fmt.Sprintf("%.1f km of rough track: needs ground clearance", roughM/1000)
		if c.Vehicle.ID == "scooty" {
			txt = fmt.Sprintf("%.1f km of rough track: hard going on a scooty", roughM/1000)
		}
		out = append(out, trail.Note{Kind: "rough", Text: txt})
	}
	if grade >= 9 {
		out = append(out, trail.Note{Kind: "steep", Text: fmt.Sprintf("Steep: up to %d%% climb", int(math.Round(grade)))})
	}
	if len(t.Geometry) > 0 {
		end := depart.Add(time.Duration(t.DurationS) * time.Second)
		if rise, set, ok := sun.SunTimes(depart, t.Geometry[0][1], t.Geometry[0][0]); ok {
			rise, set = rise.In(loc), set.In(loc)
			dark := depart.After(set) || depart.Before(rise)
			switch {
			case (dark || end.After(set)) && unpavedKm >= 0.5:
				out = append(out, trail.Note{Kind: "dark", Text: "Sunset at " + set.Format("15:04") + ": the unpaved parts have no lights"})
			case dark:
				out = append(out, trail.Note{Kind: "dark", Text: "Riding after dark: sunset was " + set.Format("15:04")})
			case end.After(set):
				out = append(out, trail.Note{Kind: "dark", Text: "You'll arrive after sunset (" + set.Format("15:04") + ")"})
			}
		}
	}
	return out
}

// --- small helpers ---------------------------------------------------------------------

func appendSpan(spans []trail.Span, kind string, from, to float64, label string) []trail.Span {
	if n := len(spans); n > 0 && spans[n-1].Kind == kind && math.Abs(spans[n-1].To-from) < 1 {
		spans[n-1].To = to
		return spans
	}
	return append(spans, trail.Span{Kind: kind, From: from, To: to, Label: label})
}

// mergeShort folds spans shorter than minM into their previous neighbour.
func mergeShort(spans []trail.Span, minM float64) []trail.Span {
	var out []trail.Span
	for _, s := range spans {
		if len(out) > 0 && (s.To-s.From < minM || out[len(out)-1].Kind == s.Kind) {
			out[len(out)-1].To = s.To
			continue
		}
		out = append(out, s)
	}
	return out
}

func roundSpans(spans []trail.Span) []trail.Span {
	for i := range spans {
		spans[i].From, spans[i].To = math.Round(spans[i].From), math.Round(spans[i].To)
	}
	return spans
}

func pointAt(pts []geo.Point, cum []float64, d float64) geo.Point {
	i := sort.SearchFloat64s(cum, d)
	if i <= 0 {
		return pts[0]
	}
	if i >= len(pts) {
		return pts[len(pts)-1]
	}
	seg := cum[i] - cum[i-1]
	if seg <= 0 {
		return pts[i]
	}
	t := (d - cum[i-1]) / seg
	a, b := pts[i-1], pts[i]
	return geo.Point{Lat: a.Lat + (b.Lat-a.Lat)*t, Lng: a.Lng + (b.Lng-a.Lng)*t}
}

func interp(cum, v []float64, d float64) float64 {
	i := sort.SearchFloat64s(cum, d)
	if i <= 0 {
		return v[0]
	}
	if i >= len(v) {
		return v[len(v)-1]
	}
	seg := cum[i] - cum[i-1]
	if seg <= 0 {
		return v[i]
	}
	return v[i-1] + (v[i]-v[i-1])*(d-cum[i-1])/seg
}

func bboxOf(pts []geo.Point) geo.BBox {
	b := geo.BBox{math.Inf(1), math.Inf(1), math.Inf(-1), math.Inf(-1)}
	for _, p := range pts {
		b[0], b[1] = math.Min(b[0], p.Lng), math.Min(b[1], p.Lat)
		b[2], b[3] = math.Max(b[2], p.Lng), math.Max(b[3], p.Lat)
	}
	return b
}

func clamp(v, lo, hi float64) float64 { return math.Max(lo, math.Min(hi, v)) }

func in(v string, set ...string) bool {
	for _, s := range set {
		if v == s {
			return true
		}
	}
	return false
}

var months = []string{"Jan", "Feb", "Mar", "Apr", "May", "Jun", "Jul", "Aug", "Sep", "Oct", "Nov", "Dec"}

// monthLabel turns 2026-03 into "Mar 2026".
func monthLabel(ym string) string {
	if len(ym) != 7 {
		return ym
	}
	var m int
	fmt.Sscanf(ym[5:], "%d", &m)
	if m < 1 || m > 12 {
		return ym
	}
	return months[m-1] + " " + ym[:4]
}

// cells returns the set of land-grid cells a line passes (for overlap tests).
func cells(g *raster.Grid, line []geo.Point) map[int]struct{} {
	out := map[int]struct{}{}
	cum := geo.CumulativeDistances(line)
	total := cum[len(cum)-1]
	for d := 0.0; d <= total; d += 25 {
		if x, y, ok := g.XY(pointAt(line, cum, d)); ok {
			out[y*g.W+x] = struct{}{}
		}
	}
	return out
}

// overlap is |a∩b| / min(|a|,|b|).
func overlap(a, b map[int]struct{}) float64 {
	if len(a) == 0 || len(b) == 0 {
		return 0
	}
	if len(a) > len(b) {
		a, b = b, a
	}
	n := 0
	for k := range a {
		if _, ok := b[k]; ok {
			n++
		}
	}
	return float64(n) / float64(len(a))
}

var _ features.Gem
