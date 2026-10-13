// Package water routes jetskis across lakes, reservoirs and wide rivers.
//
// GraphHopper only knows roads, so water routing runs on the land grid: every
// cell flagged Water is navigable. A trip stays on one connected water body.
// Two styles: "open" keeps ~100 m off the banks (shallows, nets, swimmers);
// "shore" hugs a band 100–200 m from shore for the view.
package water

import (
	"container/heap"
	"fmt"
	"math"

	"pagdandi/api/internal/features"
	"pagdandi/api/internal/geo"
	"pagdandi/api/internal/raster"
	"pagdandi/api/internal/trail"
)

// Error is a user-facing failure with a stable code.
type Error struct {
	Code    string
	Message string
}

func (e *Error) Error() string { return e.Message }

// Router plans water trips.
type Router struct {
	g      *raster.Grid
	waters []features.Water
	// MaxCells caps one water body's size (cells) to keep memory bounded.
	MaxCells int
}

// New returns a router over the grid's Water cells.
func New(g *raster.Grid, waters []features.Water) *Router {
	return &Router{g: g, waters: waters, MaxCells: 6_000_000}
}

// comp is one connected water body, as a mask over its bounding box.
type comp struct {
	x0, y0, w, h int
	mask         []bool
	shore        []uint16 // distance to land in cells (BFS, 8-connected)
	cells        int
}

func (c *comp) in(x, y int) bool {
	x, y = x-c.x0, y-c.y0
	return x >= 0 && y >= 0 && x < c.w && y < c.h && c.mask[y*c.w+x]
}

func (c *comp) idx(x, y int) int { return (y-c.y0)*c.w + (x - c.x0) }

var nbr = [8][3]float64{{1, 0, 1}, {-1, 0, 1}, {0, 1, 1}, {0, -1, 1}, {1, 1, math.Sqrt2}, {1, -1, math.Sqrt2}, {-1, 1, math.Sqrt2}, {-1, -1, math.Sqrt2}}

// Snap finds the nearest water cell within maxM of p.
func (r *Router) Snap(p geo.Point, maxM float64) (x, y int, distM float64, ok bool) {
	cx, cy, inside := r.g.XY(p)
	if !inside {
		return 0, 0, 0, false
	}
	if r.g.Get(cx, cy)&raster.Water != 0 {
		return cx, cy, 0, true
	}
	maxR := int(math.Ceil(maxM / r.g.CellM()))
	best := math.Inf(1)
	for rad := 1; rad <= maxR; rad++ {
		for dy := -rad; dy <= rad; dy++ {
			for dx := -rad; dx <= rad; dx++ {
				if max(abs(dx), abs(dy)) != rad {
					continue
				}
				if r.g.Get(cx+dx, cy+dy)&raster.Water == 0 {
					continue
				}
				if d := geo.Haversine(p, r.g.Center(cx+dx, cy+dy)); d < best {
					best, x, y = d, cx+dx, cy+dy
				}
			}
		}
		// Once found, one more ring can only be marginally closer; stop.
		if !math.IsInf(best, 1) && float64(rad)*r.g.CellM() > best+r.g.CellM() {
			break
		}
	}
	if math.IsInf(best, 1) || best > maxM {
		return 0, 0, 0, false
	}
	return x, y, best, true
}

// component flood-fills the water body containing (sx, sy).
func (r *Router) component(sx, sy int) (*comp, error) {
	type pt struct{ x, y int }
	seen := map[int]struct{}{}
	q := []pt{{sx, sy}}
	seen[sy*r.g.W+sx] = struct{}{}
	x0, y0, x1, y1 := sx, sy, sx, sy
	for i := 0; i < len(q); i++ {
		if len(seen) > r.MaxCells {
			return nil, &Error{"too_big", "This water body is too large to plan on"}
		}
		c := q[i]
		for _, n := range nbr[:4] {
			x, y := c.x+int(n[0]), c.y+int(n[1])
			k := y*r.g.W + x
			if _, ok := seen[k]; ok || r.g.Get(x, y)&raster.Water == 0 {
				continue
			}
			seen[k] = struct{}{}
			q = append(q, pt{x, y})
			x0, y0, x1, y1 = min(x0, x), min(y0, y), max(x1, x), max(y1, y)
		}
	}
	cp := &comp{x0: x0, y0: y0, w: x1 - x0 + 1, h: y1 - y0 + 1, cells: len(q)}
	cp.mask = make([]bool, cp.w*cp.h)
	for _, c := range q {
		cp.mask[cp.idx(c.x, c.y)] = true
	}
	// Distance to shore: multi-source BFS from water cells touching land.
	cp.shore = make([]uint16, len(cp.mask))
	for i := range cp.shore {
		cp.shore[i] = math.MaxUint16
	}
	var frontier []pt
	for _, c := range q {
		for _, n := range nbr {
			if !cp.in(c.x+int(n[0]), c.y+int(n[1])) {
				cp.shore[cp.idx(c.x, c.y)] = 1
				frontier = append(frontier, c)
				break
			}
		}
	}
	for i := 0; i < len(frontier); i++ {
		c := frontier[i]
		d := cp.shore[cp.idx(c.x, c.y)]
		for _, n := range nbr {
			x, y := c.x+int(n[0]), c.y+int(n[1])
			if !cp.in(x, y) {
				continue
			}
			if k := cp.idx(x, y); cp.shore[k] > d+1 {
				cp.shore[k] = d + 1
				frontier = append(frontier, pt{x, y})
			}
		}
	}
	return cp, nil
}

// cost of entering a cell in a given style.
func (c *comp) cost(x, y int, style string) float64 {
	d := float64(c.shore[c.idx(x, y)])
	switch style {
	case trail.Open:
		if d <= 2 {
			return 2.5
		}
	case trail.Shore:
		return 0.4 * math.Min(4, math.Abs(d-3))
	}
	return 0
}

type node struct {
	k    int
	f, g float64
	i    int
}
type pq []*node

func (p pq) Len() int            { return len(p) }
func (p pq) Less(i, j int) bool  { return p[i].f < p[j].f }
func (p pq) Swap(i, j int)       { p[i], p[j] = p[j], p[i]; p[i].i = i; p[j].i = j }
func (p *pq) Push(x any)         { n := x.(*node); n.i = len(*p); *p = append(*p, n) }
func (p *pq) Pop() any           { o := *p; n := o[len(o)-1]; *p = o[:len(o)-1]; return n }
func abs(v int) int              { return max(v, -v) }
func hyp(dx, dy float64) float64 { return math.Sqrt(dx*dx + dy*dy) }

// astar returns the cell path from a to b inside the component.
func (c *comp) astar(ax, ay, bx, by int, style string) [][2]int {
	n := len(c.mask)
	g := make([]float64, n)
	from := make([]int32, n)
	closed := make([]bool, n)
	for i := range g {
		g[i] = math.Inf(1)
		from[i] = -1
	}
	start, goal := c.idx(ax, ay), c.idx(bx, by)
	g[start] = 0
	open := &pq{}
	heap.Push(open, &node{k: start, f: hyp(float64(bx-ax), float64(by-ay))})
	for open.Len() > 0 {
		cur := heap.Pop(open).(*node)
		if closed[cur.k] {
			continue
		}
		if cur.k == goal {
			break
		}
		closed[cur.k] = true
		cx, cy := cur.k%c.w+c.x0, cur.k/c.w+c.y0
		for _, d := range nbr {
			x, y := cx+int(d[0]), cy+int(d[1])
			if !c.in(x, y) {
				continue
			}
			k := c.idx(x, y)
			if closed[k] {
				continue
			}
			ng := g[cur.k] + d[2]*(1+c.cost(x, y, style))
			if ng < g[k] {
				g[k] = ng
				from[k] = int32(cur.k)
				heap.Push(open, &node{k: k, g: ng, f: ng + hyp(float64(bx-x), float64(by-y))})
			}
		}
	}
	if math.IsInf(g[goal], 1) {
		return nil
	}
	var path [][2]int
	for k := goal; k != -1; k = int(from[k]) {
		path = append(path, [2]int{k%c.w + c.x0, k/c.w + c.y0})
		if k == start {
			break
		}
	}
	for i, j := 0, len(path)-1; i < j; i, j = i+1, j-1 {
		path[i], path[j] = path[j], path[i]
	}
	return path
}

// clear reports whether the straight line between two cells stays on water
// (and, for open water, off the shore band).
func (c *comp) clear(a, b [2]int, style string) bool {
	dx, dy := b[0]-a[0], b[1]-a[1]
	steps := max(abs(dx), abs(dy)) * 2
	if steps == 0 {
		return true
	}
	for s := 0; s <= steps; s++ {
		x := a[0] + int(math.Round(float64(dx)*float64(s)/float64(steps)))
		y := a[1] + int(math.Round(float64(dy)*float64(s)/float64(steps)))
		if !c.in(x, y) {
			return false
		}
		if style == trail.Open && c.shore[c.idx(x, y)] <= 1 && s > 0 && s < steps {
			return false
		}
	}
	return true
}

// smooth string-pulls a cell path into few straight legs.
func (c *comp) smooth(path [][2]int, style string) [][2]int {
	if len(path) <= 2 {
		return path
	}
	out := [][2]int{path[0]}
	i := 0
	for i < len(path)-1 {
		j := len(path) - 1
		// Shoreline rides keep more vertices so they follow the bank.
		limit := len(path)
		if style == trail.Shore {
			limit = i + 10
		}
		for j > i+1 && (j >= limit || !c.clear(path[i], path[j], style)) {
			j--
		}
		out = append(out, path[j])
		i = j
	}
	return out
}

func (r *Router) line(cells [][2]int) []geo.Point {
	out := make([]geo.Point, len(cells))
	for i, c := range cells {
		out[i] = r.g.Center(c[0], c[1])
	}
	return out
}

func (r *Router) build(cp *comp, legs [][2][2]int, style string, cruiseKmh float64) *trail.Trail {
	var cellsPath [][2]int
	for _, l := range legs {
		p := cp.astar(l[0][0], l[0][1], l[1][0], l[1][1], style)
		if p == nil {
			return nil
		}
		p = cp.smooth(p, style)
		if len(cellsPath) > 0 {
			p = p[1:]
		}
		cellsPath = append(cellsPath, p...)
	}
	line := r.line(cellsPath)
	if len(line) < 2 {
		return nil
	}
	t := &trail.Trail{Kind: style}
	t.SetLine(line)
	d := geo.Length(line)
	speed := cruiseKmh
	if style == trail.Shore {
		speed *= 0.8
	}
	t.DistanceM = math.Round(d)
	t.DurationS = math.Round(d / (speed / 3.6))
	t.Surfaces = []trail.Span{{Kind: "water", From: 0, To: t.DistanceM}}
	t.Mix = map[string]float64{"water": 1}
	t.Quiet = 1
	t.ScenicS = 0.65
	if style == trail.Shore {
		t.ScenicS = 0.85
	}
	return t
}

// Trails plans open-water and shoreline trips from a to b.
func (r *Router) Trails(a, b geo.Point, cruiseKmh float64) ([]*trail.Trail, []trail.Note, error) {
	ax, ay, ad, ok := r.Snap(a, 2500)
	if !ok {
		return nil, nil, &Error{"no_water", "No lake, reservoir or river within 2.5 km of your start"}
	}
	bx, by, bd, ok := r.Snap(b, 2500)
	if !ok {
		return nil, nil, &Error{"no_water", "No water within 2.5 km of your destination"}
	}
	cp, err := r.component(ax, ay)
	if err != nil {
		return nil, nil, err
	}
	if !cp.in(bx, by) {
		return nil, nil, &Error{"different_water", "Those points are on different water bodies. A jetski can't cross land."}
	}
	var out []*trail.Trail
	for _, style := range []string{trail.Open, trail.Shore} {
		if t := r.build(cp, [][2][2]int{{{ax, ay}, {bx, by}}}, style, cruiseKmh); t != nil {
			out = append(out, t)
		}
	}
	if len(out) == 0 {
		return nil, nil, &Error{"no_route", "No way across the water between these points"}
	}
	if len(out) == 2 && sameLine(out[0], out[1]) {
		out = out[1:]
	}
	notes := waterNotes()
	if ad > 80 {
		notes = append(notes, trail.Note{Kind: "info", Text: fmt.Sprintf("Launch from the shore about %s from your start", human(ad))})
	}
	if bd > 80 {
		notes = append(notes, trail.Note{Kind: "info", Text: fmt.Sprintf("Land on the shore about %s from your destination", human(bd))})
	}
	for _, t := range out {
		t.Warnings = append(t.Warnings, notes...)
	}
	return out, nil, nil
}

// Loops plans round trips on the water body nearest to start, reaching about reachKm out.
func (r *Router) Loops(start geo.Point, reachKm, cruiseKmh float64) ([]*trail.Trail, error) {
	sx, sy, sd, ok := r.Snap(start, 3000)
	if !ok {
		return nil, &Error{"no_water", "No lake, reservoir or river within 3 km. Pick a water body in Wander → Places."}
	}
	cp, err := r.component(sx, sy)
	if err != nil {
		return nil, err
	}
	if cp.cells < 40 {
		return nil, &Error{"no_water", "This water is too small for a jetski loop"}
	}
	// Geodesic (on-water) distance from the start to every cell.
	dist := make([]int32, len(cp.mask))
	for i := range dist {
		dist[i] = -1
	}
	q := []cell{{sx, sy}}
	dist[cp.idx(sx, sy)] = 0
	for i := 0; i < len(q); i++ {
		c := q[i]
		for _, n := range nbr[:4] {
			x, y := c.x+int(n[0]), c.y+int(n[1])
			if cp.in(x, y) && dist[cp.idx(x, y)] < 0 {
				dist[cp.idx(x, y)] = dist[cp.idx(c.x, c.y)] + 1
				q = append(q, cell{x, y})
			}
		}
	}
	target := int32(math.Max(4, reachKm*1000/r.g.CellM()))
	far := dist[cp.idx(q[len(q)-1].x, q[len(q)-1].y)]
	// The whole lake is within reach: ride around it.
	if far <= target {
		if t := r.circuit(cp, sx, sy, q, cruiseKmh); t != nil {
			t.Warnings = waterNotes()
			if sd > 80 {
				t.Warnings = append(t.Warnings, trail.Note{Kind: "info", Text: "Launch from the shore about " + human(sd) + " from your start"})
			}
			out := []*trail.Trail{t}
			if back := r.build(cp, [][2][2]int{{{sx, sy}, {q[len(q)-1].x, q[len(q)-1].y}}, {{q[len(q)-1].x, q[len(q)-1].y}, {sx, sy}}}, trail.Open, cruiseKmh); back != nil {
				back.Kind = trail.Loop
				back.Warnings = t.Warnings
				back.Tagline = fmt.Sprintf("%.1f km straight out across the open water and back", back.DistanceM/1000)
				out = append(out, back)
			}
			return out, nil
		}
	}
	if target > far {
		target = far
	}
	// Turning points: cells near the target reach, spread by bearing.
	type cand struct {
		x, y int
		brg  float64
	}
	var cands []cand
	for _, c := range q {
		d := dist[cp.idx(c.x, c.y)]
		if d >= target*7/10 && d <= target && cp.shore[cp.idx(c.x, c.y)] >= 2 {
			cands = append(cands, cand{c.x, c.y, geo.Bearing(r.g.Center(sx, sy), r.g.Center(c.x, c.y))})
		}
	}
	if len(cands) == 0 {
		for _, c := range q[len(q)*3/4:] {
			cands = append(cands, cand{c.x, c.y, geo.Bearing(r.g.Center(sx, sy), r.g.Center(c.x, c.y))})
		}
	}
	var out []*trail.Trail
	used := []float64{}
	for _, c := range cands {
		if len(out) == 2 {
			break
		}
		clash := false
		for _, b := range used {
			if math.Abs(math.Mod(c.brg-b+540, 360)-180) < 50 {
				clash = true
			}
		}
		if clash {
			continue
		}
		used = append(used, c.brg)
		// Out along the shore, back across open water: a loop, not a retrace.
		out1 := r.build(cp, [][2][2]int{{{sx, sy}, {c.x, c.y}}}, trail.Shore, cruiseKmh)
		back := r.build(cp, [][2][2]int{{{c.x, c.y}, {sx, sy}}}, trail.Open, cruiseKmh)
		if out1 == nil || back == nil {
			continue
		}
		line := append(out1.Line(), back.Line()[1:]...)
		t := &trail.Trail{Kind: trail.Loop}
		t.SetLine(line)
		t.DistanceM = out1.DistanceM + back.DistanceM
		t.DurationS = out1.DurationS + back.DurationS
		t.Surfaces = []trail.Span{{Kind: "water", From: 0, To: t.DistanceM}}
		t.Mix = map[string]float64{"water": 1}
		t.Quiet, t.ScenicS = 1, 0.8
		t.Warnings = waterNotes()
		if sd > 80 {
			t.Warnings = append(t.Warnings, trail.Note{Kind: "info", Text: "Launch from the shore about " + human(sd) + " from your start"})
		}
		out = append(out, t)
	}
	if len(out) == 0 {
		return nil, &Error{"no_route", "Couldn't plan a loop on this water"}
	}
	return out, nil
}

// circuit rides around the water body: three shoreline legs through points
// spaced a third of the way round the lake, in the 100–200 m shore band.
func (r *Router) circuit(cp *comp, sx, sy int, cells []cell, cruiseKmh float64) *trail.Trail {
	var cx, cy float64
	for _, c := range cells {
		cx += float64(c.x)
		cy += float64(c.y)
	}
	cx /= float64(len(cells))
	cy /= float64(len(cells))
	maxShore := uint16(0)
	for _, c := range cells {
		maxShore = max(maxShore, cp.shore[cp.idx(c.x, c.y)])
	}
	lo, hi := uint16(2), uint16(4)
	if maxShore < 4 {
		lo, hi = 1, max(1, maxShore/2+1)
	}
	a0 := math.Atan2(float64(sy)-cy, float64(sx)-cx)
	pickAt := func(angle float64) ([2]int, bool) {
		best, bestD := [2]int{}, math.Inf(1)
		for _, c := range cells {
			s := cp.shore[cp.idx(c.x, c.y)]
			if s < lo || s > hi {
				continue
			}
			a := math.Atan2(float64(c.y)-cy, float64(c.x)-cx)
			d := math.Abs(math.Mod(a-angle+3*math.Pi, 2*math.Pi) - math.Pi)
			// Prefer the outermost band cell along that bearing (the real shore, not a bay inside).
			d -= 0.0005 * math.Hypot(float64(c.x)-cx, float64(c.y)-cy)
			if d < bestD {
				best, bestD = [2]int{c.x, c.y}, d
			}
		}
		return best, !math.IsInf(bestD, 1)
	}
	p1, ok1 := pickAt(a0 + 2*math.Pi/3)
	p2, ok2 := pickAt(a0 + 4*math.Pi/3)
	if !ok1 || !ok2 {
		return nil
	}
	s := [2]int{sx, sy}
	t := r.build(cp, [][2][2]int{{s, p1}, {p1, p2}, {p2, s}}, trail.Shore, cruiseKmh)
	if t == nil {
		return nil
	}
	t.Kind = trail.Loop
	t.ScenicS = 0.9
	t.Tagline = fmt.Sprintf("%.1f km round the lake, about 150 m off the shore", t.DistanceM/1000)
	return t
}

// cell is a grid coordinate.
type cell struct{ x, y int }

// WaterAt returns the water body record whose bbox contains p, if any.
func (r *Router) WaterAt(p geo.Point) (features.Water, bool) {
	for _, w := range r.waters {
		if w.BBox.Contains(p) {
			return w, true
		}
	}
	return features.Water{}, false
}

func waterNotes() []trail.Note {
	return []trail.Note{
		{Kind: "legal", Text: "Most lakes and reservoirs need permission from the local authority for jetskis"},
		{Kind: "water", Text: "Wear a life jacket. Watch for fishing nets, swimmers and boats near the shore"},
	}
}

func sameLine(a, b *trail.Trail) bool {
	return math.Abs(a.DistanceM-b.DistanceM) < 0.03*a.DistanceM && len(a.Geometry) == len(b.Geometry)
}

func human(m float64) string {
	if m >= 1000 {
		return fmt.Sprintf("%.1f km", m/1000)
	}
	return fmt.Sprintf("%d m", int(math.Round(m/10)*10))
}
