package plan

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"sort"
	"strings"
	"sync"
	"time"

	"pagdandi/api/internal/features"
	"pagdandi/api/internal/geo"
	"pagdandi/api/internal/gh"
	"pagdandi/api/internal/land"
	"pagdandi/api/internal/region"
	"pagdandi/api/internal/trail"
	"pagdandi/api/internal/water"
)

// Error is a user-facing planning failure with a stable code.
type Error struct {
	Code    string // no_route | outside | bad_request | different_water | no_water | unavailable
	Message string
}

func (e *Error) Error() string { return e.Message }

func errf(code, format string, a ...any) *Error {
	return &Error{Code: code, Message: fmt.Sprintf(format, a...)}
}

// Planner builds trails.
type Planner struct {
	GH     *gh.Client
	Land   *land.Land
	Region *region.Region
	Water  *water.Router
	Log    *slog.Logger

	cache *lru
	sem   chan struct{}
	once  sync.Once
}

func (p *Planner) init() {
	p.once.Do(func() {
		p.cache = newLRU(256, 15*time.Minute)
		p.sem = make(chan struct{}, 6)
	})
}

// GoRequest asks for trails from A to B.
type GoRequest struct {
	From    geo.Point `json:"from"`
	To      geo.Point `json:"to"`
	Vehicle string    `json:"vehicle"`
	StrayKm *float64  `json:"stray_km,omitempty"` // detour budget over the usual; nil = auto
	Depart  time.Time `json:"depart,omitempty"`
}

// GoResult is the answer.
type GoResult struct {
	Vehicle string         `json:"vehicle"`
	StrayKm float64        `json:"stray_km"`
	Trails  []*trail.Trail `json:"trails"` // the usual first, then the rest by interest
	Notes   []trail.Note   `json:"notes,omitempty"`
	TookMs  int64          `json:"took_ms"`
}

// AutoStrayKm is the default detour budget for a usual route of d metres.
func AutoStrayKm(d float64) float64 { return clamp(math.Round(0.35*d/100)/10, 2, 25) }

// Go plans trails from A to B.
func (p *Planner) Go(ctx context.Context, req GoRequest) (*GoResult, error) {
	p.init()
	start := time.Now()
	v, ok := VehicleByID(req.Vehicle)
	if !ok {
		return nil, errf("bad_request", "unknown vehicle %q", req.Vehicle)
	}
	if !req.From.Valid() || !req.To.Valid() {
		return nil, errf("bad_request", "from and to must be valid coordinates")
	}
	if !p.Region.Contains(req.From) || !p.Region.Contains(req.To) {
		return nil, errf("outside", "Pagdandi covers %.0f km around %s for now", p.Region.RadiusKm, p.Region.Name)
	}
	if geo.Haversine(req.From, req.To) < 30 {
		return nil, errf("bad_request", "You're already there")
	}
	if req.Depart.IsZero() {
		req.Depart = time.Now()
	}
	key := fmt.Sprintf("go|%s|%.5f,%.5f|%.5f,%.5f|%v|%s", v.ID, req.From.Lat, req.From.Lng, req.To.Lat, req.To.Lng, fmtStray(req.StrayKm), req.Depart.In(p.Region.Location()).Format("2006-01-02T15"))
	if r, ok := p.cache.get(key); ok {
		return r.(*GoResult), nil
	}

	var res *GoResult
	var err error
	if v.Water {
		res, err = p.goWater(req, v)
	} else {
		res, err = p.goLand(ctx, req, v)
	}
	if err != nil {
		return nil, err
	}
	res.TookMs = time.Since(start).Milliseconds()
	p.cache.put(key, res)
	return res, nil
}

func fmtStray(s *float64) string {
	if s == nil {
		return "auto"
	}
	return fmt.Sprintf("%.1f", *s)
}

// route calls GraphHopper with a concurrency limit and analyzes the path.
func (p *Planner) route(ctx context.Context, v Vehicle, kind string, pts []geo.Point, depart time.Time) (*trail.Trail, error) {
	select {
	case p.sem <- struct{}{}:
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	defer func() { <-p.sem }()
	r := gh.Request{Profile: v.Profile, Locale: "en", Instructions: true, Elevation: true, Details: details,
		CustomModel: Model(kind, v), PassThrough: len(pts) > 2}
	for _, q := range pts {
		r.Points = append(r.Points, [2]float64{q.Lng, q.Lat})
	}
	path, err := p.GH.Route(ctx, r)
	if err != nil {
		return nil, err
	}
	t := Analyze(path, Ctx{Land: p.Land, Vehicle: v, Depart: depart, Loc: p.Region.Location()})
	t.Kind = kind
	return t, nil
}

type job struct {
	kind string
	pts  []geo.Point
	via  []trail.Via
}

func (p *Planner) runJobs(ctx context.Context, v Vehicle, jobs []job, depart time.Time) []*trail.Trail {
	out := make([]*trail.Trail, len(jobs))
	var wg sync.WaitGroup
	for i, j := range jobs {
		wg.Add(1)
		go func(i int, j job) {
			defer wg.Done()
			t, err := p.route(ctx, v, j.kind, j.pts, depart)
			if err != nil {
				if !gh.IsNoRoute(err) {
					p.Log.Warn("route failed", "kind", j.kind, "err", err)
				}
				return
			}
			t.Via = j.via
			out[i] = t
		}(i, j)
	}
	wg.Wait()
	return out
}

func (p *Planner) goLand(ctx context.Context, req GoRequest, v Vehicle) (*GoResult, error) {
	ab := []geo.Point{req.From, req.To}
	base := p.runJobs(ctx, v, []job{{trail.Usual, ab, nil}, {trail.Shortest, ab, nil}, {trail.Kaccha, ab, nil}, {trail.Quiet, ab, nil}}, req.Depart)
	usual := base[0]
	if usual == nil {
		// GraphHopper may be down or the points unroutable; tell which.
		if err := p.GH.Health(ctx); err != nil {
			return nil, errf("unavailable", "Routing is starting up or offline. Try again in a minute.")
		}
		return nil, errf("no_route", "No way through on a %s between these points.", v.Label)
	}
	d0 := usual.DistanceM
	stray := AutoStrayKm(d0)
	if req.StrayKm != nil {
		stray = clamp(*req.StrayKm, 0, 100)
	}
	budget := stray * 1000

	// Scenic and fresh trails bend through via-points inside an ellipse
	// around A and B that the detour budget allows.
	var extra []job
	for _, a := range p.scenicAnchors(req.From, req.To, d0, budget, 5) {
		extra = append(extra, job{trail.Scenic, []geo.Point{req.From, a.Point(), req.To}, []trail.Via{{Name: a.Name, Kind: a.Kind, Lat: a.Lat, Lng: a.Lng}}})
	}
	for _, f := range p.freshAnchors(req.From, req.To, d0, budget, 4) {
		m := f.Mid()
		name := f.Name
		if name == "" {
			name = freshLabel(f.Highway)
		}
		extra = append(extra, job{trail.Fresh, []geo.Point{req.From, m, req.To}, []trail.Via{{Name: name, Kind: "fresh", Lat: m.Lat, Lng: m.Lng}}})
	}
	more := p.runJobs(ctx, v, extra, req.Depart)

	cands := []*trail.Trail{usual}
	for _, t := range base[1:] {
		if t != nil {
			cands = append(cands, t)
		}
	}
	if s := bestBy(more, trail.Scenic, d0, budget, func(t *trail.Trail) float64 { return t.ScenicS }); s != nil && s.ScenicS >= usual.ScenicS+0.04 {
		cands = append(cands, s)
	}
	if f := bestBy(more, trail.Fresh, d0, budget, func(t *trail.Trail) float64 { return t.FreshM / 1000 }); f != nil && f.FreshM >= 250 && f.FreshM >= usual.FreshM+200 {
		cands = append(cands, f)
	}
	trails := p.finish(usual, cands, budget, v)
	res := &GoResult{Vehicle: v.ID, StrayKm: stray, Trails: trails}
	if len(trails) == 1 {
		res.Notes = append(res.Notes, trail.Note{Kind: "info", Text: "No other way is different enough within your detour budget. Stray further to see more."})
	}
	return res, nil
}

// bestBy picks the candidate of kind with the highest score minus a detour penalty.
func bestBy(ts []*trail.Trail, kind string, d0, budget float64, score func(*trail.Trail) float64) *trail.Trail {
	var best *trail.Trail
	bestV := math.Inf(-1)
	for _, t := range ts {
		if t == nil || t.Kind != kind || t.DistanceM > d0+budget+1 {
			continue
		}
		pen := 0.0
		if budget > 0 {
			pen = 0.25 * math.Max(0, t.DistanceM-d0) / budget
		}
		if s := score(t) - pen; s > bestV {
			best, bestV = t, s
		}
	}
	return best
}

// finish filters by budget and distinctiveness, dedupes, names and orders.
func (p *Planner) finish(usual *trail.Trail, cands []*trail.Trail, budget float64, v Vehicle) []*trail.Trail {
	d0 := usual.DistanceM
	unpaved := func(t *trail.Trail) float64 { return (t.Mix["rough"] + t.Mix["dirt"]) * t.DistanceM }
	keep := []*trail.Trail{}
	sigs := []map[int]struct{}{}
	order := map[string]int{trail.Usual: 0, trail.Shortest: 1, trail.Scenic: 2, trail.Kaccha: 3, trail.Quiet: 4, trail.Fresh: 5}
	sort.SliceStable(cands, func(i, j int) bool { return order[cands[i].Kind] < order[cands[j].Kind] })
	for _, t := range cands {
		if t.Kind != trail.Usual && t.Kind != trail.Shortest && t.DistanceM > d0+budget+1 {
			continue
		}
		switch t.Kind {
		case trail.Shortest:
			if t.DistanceM > d0-math.Max(300, 0.03*d0) {
				continue
			}
		case trail.Kaccha:
			moreDirt := unpaved(t) >= unpaved(usual)+500 || t.Mix["dirt"]+t.Mix["rough"] >= usual.Mix["dirt"]+usual.Mix["rough"]+0.15
			moreBack := t.BackroadsM >= usual.BackroadsM+math.Max(1500, 0.15*t.DistanceM)
			if !moreDirt && !moreBack {
				continue
			}
		case trail.Quiet:
			if t.Quiet < usual.Quiet+0.08 {
				continue
			}
		}
		sig := cells(p.Land.Grid, t.Line())
		dup := false
		for k, s := range sigs {
			if overlap(sig, s) >= 0.85 {
				if !contains(keep[k].Also, t.Kind) && keep[k].Kind != t.Kind {
					keep[k].Also = append(keep[k].Also, t.Kind)
				}
				dup = true
				break
			}
		}
		if dup {
			continue
		}
		keep = append(keep, t)
		sigs = append(sigs, sig)
	}
	for _, t := range keep {
		t.DeltaM = math.Round(t.DistanceM - usual.DistanceM)
		t.DeltaS = math.Round(t.DurationS - usual.DurationS)
		t.Named()
		t.Tagline = tagline(t, usual, v)
		t.ID = trailID(t)
	}
	return keep
}

func contains(s []string, v string) bool {
	for _, x := range s {
		if x == v {
			return true
		}
	}
	return false
}

func trailID(t *trail.Trail) string {
	h := sha1.New()
	fmt.Fprintf(h, "%s|%v|%v", t.Kind, t.DistanceM, t.Geometry[len(t.Geometry)/2])
	return t.Kind + "-" + hex.EncodeToString(h.Sum(nil))[:8]
}

func tagline(t, usual *trail.Trail, v Vehicle) string {
	unpavedKm := (t.Mix["rough"] + t.Mix["dirt"]) * t.DistanceM / 1000
	firstHighlight := func(kinds ...string) string {
		for _, h := range t.Highlights {
			if in(h.Kind, kinds...) && h.Label != "" {
				return h.Label
			}
		}
		return ""
	}
	switch t.Kind {
	case trail.Usual:
		if t.Mix["paved"] > 0.97 {
			return "Main roads, no surprises"
		}
		return "The way most people go"
	case trail.Shortest:
		s := fmt.Sprintf("Saves %.1f km", -t.DeltaM/1000)
		if unpavedKm >= 0.5 {
			s += fmt.Sprintf(", %.1f km of it kaccha", unpavedKm)
		}
		return s
	case trail.Kaccha:
		if unpavedKm >= 1 {
			return fmt.Sprintf("%.1f km of dirt, gravel and track", unpavedKm)
		}
		return fmt.Sprintf("%.1f km of village roads and tracks", t.BackroadsM/1000)
	case trail.Quiet:
		if h := firstHighlight("water", "forest", "wild"); h != "" {
			return "Skips the main roads. " + h
		}
		return fmt.Sprintf("Skips the main roads: %d%% quiet", int(math.Round(t.Quiet*100)))
	case trail.Scenic:
		if h := firstHighlight("water", "view", "forest", "wild", "hills"); h != "" {
			return h
		}
		return "Greener and prettier than the usual"
	case trail.Fresh:
		return fmt.Sprintf("%.1f km of road new on the map since %s", t.FreshM/1000, monthLabel(t.FreshSince))
	}
	return ""
}

// scenicAnchors picks up to k diverse scenic via-points the budget can reach.
func (p *Planner) scenicAnchors(a, b geo.Point, d0, budget float64, k int) []features.Anchor {
	if budget <= 0 {
		return nil
	}
	ab := geo.Haversine(a, b)
	major := math.Max(ab*1.02, (d0+budget*0.8)/1.3) // straight-line reach of the budget
	if major <= ab {
		return nil
	}
	c := geo.Point{Lat: (a.Lat + b.Lat) / 2, Lng: (a.Lng + b.Lng) / 2}
	boost := map[string]float64{"waterfall": 1.25, "viewpoint": 1.15, "peak": 1.0, "water": 1.0, "forest": 0.85, "heritage": 0.9, "river": 0.85, "green": 0.6}
	type sc struct {
		a features.Anchor
		s float64
	}
	var all []sc
	for _, an := range p.Land.Anchors.Near(c, major/2+500) {
		pt := an.Point()
		sum := geo.Haversine(pt, a) + geo.Haversine(pt, b)
		if sum > major || geo.Haversine(pt, a) < 400 || geo.Haversine(pt, b) < 400 {
			continue
		}
		frac := (sum - ab) / math.Max(1, major-ab)
		all = append(all, sc{an, an.W * boost[an.Kind] * (1 - 0.6*frac)})
	}
	sort.Slice(all, func(i, j int) bool { return all[i].s > all[j].s })
	minSep := math.Max(1000, 0.12*ab)
	var out []features.Anchor
	for _, x := range all {
		ok := true
		for _, o := range out {
			if geo.Haversine(o.Point(), x.a.Point()) < minSep {
				ok = false
				break
			}
		}
		if ok {
			out = append(out, x.a)
			if len(out) == k {
				break
			}
		}
	}
	return out
}

// freshAnchors picks up to k new-on-the-map ways the budget can reach.
func (p *Planner) freshAnchors(a, b geo.Point, d0, budget float64, k int) []features.Fresh {
	if budget <= 0 {
		return nil
	}
	ab := geo.Haversine(a, b)
	major := math.Max(ab*1.02, (d0+budget*0.8)/1.3)
	c := geo.Point{Lat: (a.Lat + b.Lat) / 2, Lng: (a.Lng + b.Lng) / 2}
	newest := time.Now().UTC().Format("2006-01")
	type sc struct {
		f features.Fresh
		s float64
	}
	var all []sc
	for _, f := range p.Land.Fresh.Near(c, major/2+500) {
		if f.LenM < 150 {
			continue
		}
		m := f.Mid()
		sum := geo.Haversine(m, a) + geo.Haversine(m, b)
		if sum > major || geo.Haversine(m, a) < 300 || geo.Haversine(m, b) < 300 {
			continue
		}
		s := math.Min(1, f.LenM/1500)
		if in(f.Highway, "track", "unclassified", "path") {
			s *= 1.2
		}
		if f.Month >= newest[:4] {
			s *= 1.1
		}
		frac := (sum - ab) / math.Max(1, major-ab)
		all = append(all, sc{f, s * (1 - 0.5*frac)})
	}
	sort.Slice(all, func(i, j int) bool { return all[i].s > all[j].s })
	var out []features.Fresh
	for _, x := range all {
		ok := true
		for _, o := range out {
			if geo.Haversine(o.Mid(), x.f.Mid()) < 800 {
				ok = false
				break
			}
		}
		if ok {
			out = append(out, x.f)
			if len(out) == k {
				break
			}
		}
	}
	return out
}

// freshLabel names an unnamed new way by its kind.
func freshLabel(highway string) string {
	switch highway {
	case "track", "path", "bridleway":
		return "A new track"
	case "residential", "living_street":
		return "A new street"
	case "service":
		return "A new lane"
	case "unclassified":
		return "A new village road"
	}
	return "A new road"
}

// goWater plans jetski trails on one water body.
func (p *Planner) goWater(req GoRequest, v Vehicle) (*GoResult, error) {
	if p.Water == nil {
		return nil, errf("unavailable", "Water routing is not loaded")
	}
	ts, notes, err := p.Water.Trails(req.From, req.To, v.CruiseKm)
	if err != nil {
		var we *water.Error
		if errors.As(err, &we) {
			return nil, &Error{Code: we.Code, Message: we.Message}
		}
		return nil, err
	}
	for _, t := range ts {
		p.decorateWater(t)
	}
	return &GoResult{Vehicle: v.ID, Trails: ts, Notes: notes}, nil
}

// decorateWater adds names, highlights and ids to a water trail.
func (p *Planner) decorateWater(t *trail.Trail) {
	t.Named()
	line := t.Line()
	if len(line) > 1 {
		mid := line[len(line)/2]
		name := ""
		if w, ok := p.Water.WaterAt(mid); ok && !strings.HasPrefix(w.Name, "Unnamed") {
			name = w.Name
		} else {
			name = p.Land.WaterNear(mid, 3000)
		}
		if name != "" {
			t.Highlights = append(t.Highlights, trail.Span{Kind: "water", From: 0, To: t.DistanceM, Label: "On " + name})
			if t.Tagline == "" {
				t.Tagline = "On " + name
			}
		}
	}
	t.ID = trailID(t)
}
