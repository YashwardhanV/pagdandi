package plan

import (
	"context"
	"errors"
	"fmt"
	"math"
	"math/rand"
	"sort"
	"strings"
	"time"

	"pagdandi/api/internal/features"
	"pagdandi/api/internal/geo"
	"pagdandi/api/internal/gh"
	"pagdandi/api/internal/trail"
	"pagdandi/api/internal/water"
)

// Flavours of wandering.
var Flavours = []string{"any", "water", "hills", "forest", "heritage", "wild", "fresh"}

var flavourGems = map[string][]string{
	"water":    {"lake", "waterfall", "dam", "ghat", "spring"},
	"hills":    {"peak", "viewpoint", "cliff"},
	"forest":   {"wild", "park", "camp", "picnic"},
	"heritage": {"fort", "palace", "ruins", "monument", "temple", "cave"},
	"wild":     {"waterfall", "cave", "wild", "peak", "viewpoint", "camp", "spring", "cliff"},
}

var flavourAnchors = map[string][]string{
	"water":    {"water", "river", "waterfall"},
	"hills":    {"viewpoint", "peak"},
	"forest":   {"forest", "green"},
	"heritage": {"heritage"},
	"wild":     {"waterfall", "forest", "viewpoint", "peak"},
}

// WanderRequest asks what's out there.
type WanderRequest struct {
	From     geo.Point `json:"from"`
	Vehicle  string    `json:"vehicle"`
	RadiusKm float64   `json:"radius_km"`
	Flavour  string    `json:"flavour"`
	Kind     string    `json:"kind"` // places | loops
	Seed     int64     `json:"seed"`
	Depart   time.Time `json:"depart,omitempty"`
}

// Place is somewhere to go.
type Place struct {
	ID         string    `json:"id"`
	Name       string    `json:"name"`
	NameHi     string    `json:"name_hi,omitempty"`
	Kind       string    `json:"kind"`
	Lat        float64   `json:"lat"`
	Lng        float64   `json:"lng"`
	Target     geo.Point `json:"target"`
	DistanceKm float64   `json:"distance_km"` // straight line
	Bearing    float64   `json:"bearing"`
	Score      float64   `json:"score"`
	Why        string    `json:"why"`
	Tags       []string  `json:"tags,omitempty"`
}

// WanderResult is places or loops.
type WanderResult struct {
	Vehicle  string         `json:"vehicle"`
	RadiusKm float64        `json:"radius_km"`
	Flavour  string         `json:"flavour"`
	Kind     string         `json:"kind"`
	Places   []Place        `json:"places,omitempty"`
	Loops    []*trail.Trail `json:"loops,omitempty"`
	Notes    []trail.Note   `json:"notes,omitempty"`
	TookMs   int64          `json:"took_ms"`
}

// Wander answers "surprise me within N km".
func (p *Planner) Wander(ctx context.Context, req WanderRequest) (*WanderResult, error) {
	p.init()
	start := time.Now()
	v, ok := VehicleByID(req.Vehicle)
	if !ok {
		return nil, errf("bad_request", "unknown vehicle %q", req.Vehicle)
	}
	if !req.From.Valid() {
		return nil, errf("bad_request", "from must be a valid coordinate")
	}
	if !p.Region.Contains(req.From) {
		return nil, errf("outside", "Pagdandi covers %.0f km around %s for now", p.Region.RadiusKm, p.Region.Name)
	}
	if req.Flavour == "" {
		req.Flavour = "any"
	}
	if !in(req.Flavour, Flavours...) {
		return nil, errf("bad_request", "unknown flavour %q", req.Flavour)
	}
	if req.Kind == "" {
		req.Kind = "places"
	}
	req.RadiusKm = clamp(req.RadiusKm, 1, v.MaxKm)
	if req.Depart.IsZero() {
		req.Depart = time.Now()
	}
	key := fmt.Sprintf("wander|%s|%.4f,%.4f|%.1f|%s|%s|%d", v.ID, req.From.Lat, req.From.Lng, req.RadiusKm, req.Flavour, req.Kind, req.Seed)
	if r, ok := p.cache.get(key); ok {
		return r.(*WanderResult), nil
	}
	res := &WanderResult{Vehicle: v.ID, RadiusKm: req.RadiusKm, Flavour: req.Flavour, Kind: req.Kind}
	var err error
	switch {
	case req.Kind == "places" && v.Water:
		res.Places = p.waterPlaces(req)
	case req.Kind == "places":
		res.Places = p.places(req)
	case req.Kind == "loops" && v.Water:
		res.Loops, err = p.waterLoops(req, v)
	case req.Kind == "loops":
		res.Loops, err = p.loops(ctx, req, v)
	default:
		return nil, errf("bad_request", "kind must be places or loops")
	}
	if err != nil {
		return nil, err
	}
	if req.Kind == "places" && len(res.Places) == 0 {
		res.Notes = append(res.Notes, trail.Note{Kind: "info", Text: "Nothing on the map matches here. Widen the circle or try another flavour."})
	}
	res.TookMs = time.Since(start).Milliseconds()
	p.cache.put(key, res)
	return res, nil
}

func (p *Planner) places(req WanderRequest) []Place {
	r := req.RadiusKm * 1000
	var out []Place
	if req.Flavour == "fresh" {
		out = p.freshPlaces(req)
	} else {
		kinds := flavourGems[req.Flavour]
		for _, g := range p.Land.Gems.Near(req.From, r) {
			d := geo.Haversine(req.From, g.Point())
			if d < 500 || (kinds != nil && !in(g.Kind, kinds...)) {
				continue
			}
			out = append(out, gemPlace(g, req.From, d))
		}
	}
	return pick(out, req.RadiusKm, req.Seed, 24)
}

func gemPlace(g features.Gem, from geo.Point, d float64) Place {
	pl := Place{ID: g.ID, Name: g.Name, NameHi: g.NameHi, Kind: g.Kind, Lat: g.Lat, Lng: g.Lng, Target: g.Target(),
		DistanceKm: trail.Round(d/1000, 1), Bearing: math.Round(geo.Bearing(from, g.Point())), Score: g.Score, Why: why(g)}
	if g.Featured {
		pl.Tags = append(pl.Tags, "local favourite")
	}
	if g.Wiki {
		pl.Tags = append(pl.Tags, "famous")
	}
	if g.Kind == "waterfall" {
		pl.Tags = append(pl.Tags, "best Jul–Oct")
	}
	return pl
}

func why(g features.Gem) string {
	switch g.Kind {
	case "waterfall":
		return "Waterfall: at its best after the rains"
	case "viewpoint":
		if g.EleM > 0 {
			return fmt.Sprintf("Viewpoint, %d m up", int(g.EleM))
		}
		return "Viewpoint: go for sunset"
	case "peak":
		if g.EleM > 0 {
			return fmt.Sprintf("Hilltop, %d m", int(g.EleM))
		}
		return "Hilltop with a view"
	case "lake":
		if g.AreaKm2 > 0 {
			return fmt.Sprintf("Lake: %.1f km² of water", g.AreaKm2)
		}
		return "Lake"
	case "dam":
		return "Dam and reservoir"
	case "fort":
		return "Fort: old walls, big views"
	case "palace":
		return "Palace"
	case "ruins":
		return "Ruins and old stones"
	case "temple":
		return "Temple"
	case "wild":
		return "Forest and wildlife"
	case "cave":
		return "Cave"
	case "ghat":
		return "Riverside ghat"
	case "park":
		return "Park"
	case "picnic":
		return "Picnic spot"
	case "camp":
		return "Campsite"
	case "spring":
		return "Hot spring"
	case "cliff":
		return "Cliff edge"
	case "monument":
		return "Monument"
	}
	return "Locals go here"
}

// freshPlaces turns the longest new-on-the-map ways into destinations.
func (p *Planner) freshPlaces(req WanderRequest) []Place {
	var all []features.Fresh
	for _, f := range p.Land.Fresh.Near(req.From, req.RadiusKm*1000) {
		if f.LenM >= 400 && geo.Haversine(req.From, f.Mid()) > 800 {
			all = append(all, f)
		}
	}
	sort.Slice(all, func(i, j int) bool { return all[i].LenM > all[j].LenM })
	var out []Place
	for _, f := range all {
		m := f.Mid()
		close := false
		for _, o := range out {
			if geo.Haversine(m, geo.Point{Lat: o.Lat, Lng: o.Lng}) < 1500 {
				close = true
				break
			}
		}
		if close {
			continue
		}
		name := f.Name
		if name == "" {
			name = freshLabel(f.Highway)
		}
		d := geo.Haversine(req.From, m)
		out = append(out, Place{ID: fmt.Sprintf("fresh-%d", f.ID), Name: name, Kind: "fresh", Lat: m.Lat, Lng: m.Lng, Target: m,
			DistanceKm: trail.Round(d/1000, 1), Bearing: math.Round(geo.Bearing(req.From, m)),
			Score: clamp(0.4+f.LenM/5000, 0.4, 0.9), Why: fmt.Sprintf("%.1f km of %s, on the map since %s", f.LenM/1000, f.Highway, monthLabel(f.Month)),
			Tags: []string{"new on the map"}})
		if len(out) >= 60 {
			break
		}
	}
	return out
}

func (p *Planner) waterPlaces(req WanderRequest) []Place {
	var out []Place
	for _, w := range p.Land.Waters {
		c := geo.Point{Lat: w.Lat, Lng: w.Lng}
		target := c
		if w.Shore != nil {
			target = *w.Shore
		}
		d := geo.Haversine(req.From, target)
		if d > req.RadiusKm*1000 || w.AreaKm2 < 0.3 {
			continue
		}
		kind := w.Kind
		why := fmt.Sprintf("%s: %.1f km² of water to ride", strings.Title(kind), w.AreaKm2)
		out = append(out, Place{ID: w.ID, Name: w.Name, Kind: kind, Lat: c.Lat, Lng: c.Lng, Target: target,
			DistanceKm: trail.Round(d/1000, 1), Bearing: math.Round(geo.Bearing(req.From, c)),
			Score: clamp(0.4+0.2*math.Log10(w.AreaKm2*3), 0.3, 1), Why: why})
	}
	return pick(out, req.RadiusKm, req.Seed, 24)
}

// pick ranks places (slightly favouring farther ones: this is about going
// somewhere) and, with a seed, draws a weighted random "surprise" sample.
func pick(ps []Place, radiusKm float64, seed int64, n int) []Place {
	w := func(p Place) float64 {
		f := 0.75 + 0.25*math.Min(1, p.DistanceKm/radiusKm)
		if contains(p.Tags, "local favourite") {
			f *= 1.15
		}
		return math.Max(0.01, p.Score*f)
	}
	keys := make([]float64, len(ps))
	rng := rand.New(rand.NewSource(seed))
	for i, p := range ps {
		if seed == 0 {
			keys[i] = w(p)
		} else {
			keys[i] = math.Pow(rng.Float64(), 1/w(p)) // Efraimidis–Spirakis
		}
	}
	idx := make([]int, len(ps))
	for i := range idx {
		idx[i] = i
	}
	sort.Slice(idx, func(a, b int) bool { return keys[idx[a]] > keys[idx[b]] })
	var out []Place
	for _, i := range idx {
		out = append(out, ps[i])
		if len(out) == n {
			break
		}
	}
	return out
}

type loopAnchor struct {
	p    geo.Point
	name string
	kind string
	w    float64
}

// loops plans up to three round trips reaching about RadiusKm out.
func (p *Planner) loops(ctx context.Context, req WanderRequest, v Vehicle) ([]*trail.Trail, error) {
	r := req.RadiusKm * 1000
	var anchors []loopAnchor
	akinds := flavourAnchors[req.Flavour]
	gkinds := flavourGems[req.Flavour]
	for _, a := range p.Land.Anchors.Near(req.From, r) {
		if akinds == nil || in(a.Kind, akinds...) {
			anchors = append(anchors, loopAnchor{a.Point(), a.Name, a.Kind, a.W})
		}
	}
	for _, g := range p.Land.Gems.Near(req.From, r) {
		if gkinds == nil || in(g.Kind, gkinds...) {
			anchors = append(anchors, loopAnchor{g.Target(), g.Name, g.Kind, g.Score + 0.15})
		}
	}
	if req.Flavour == "fresh" {
		for _, f := range p.Land.Fresh.Near(req.From, r) {
			if f.LenM >= 200 {
				anchors = append(anchors, loopAnchor{f.Mid(), f.Name, "fresh", math.Min(1, f.LenM/1500)})
			}
		}
	}
	kind := trail.Loop
	if req.Flavour == "wild" {
		kind = trail.Kaccha
	}
	pairs := loopPairs(req.From, anchors, r, req.Seed)
	var jobs []job
	for _, pr := range pairs {
		a, b := pr[0], pr[1]
		jobs = append(jobs, job{kind, []geo.Point{req.From, a.p, b.p, req.From}, []trail.Via{
			{Name: a.name, Kind: a.kind, Lat: a.p.Lat, Lng: a.p.Lng}, {Name: b.name, Kind: b.kind, Lat: b.p.Lat, Lng: b.p.Lng}}})
	}
	got := p.runJobs(ctx, v, jobs, req.Depart)
	var out []*trail.Trail
	for _, t := range got {
		if t != nil && t.DistanceM <= 3.6*r+2000 {
			out = append(out, t)
		}
	}
	// Top up with GraphHopper's own round trips when the map is thin here.
	for seed := int64(1); len(out) < 3 && seed <= 3; seed++ {
		t, err := p.roundTrip(ctx, v, req.From, 2.2*r, seed+req.Seed, req.Depart)
		if err == nil && t != nil {
			out = append(out, t)
		}
	}
	out = p.dedupeLoops(out)
	if len(out) == 0 {
		return nil, errf("no_route", "Couldn't find a loop from here. Try a bigger circle.")
	}
	for i, t := range out {
		t.Kind = trail.Loop
		t.Named()
		t.Name, t.Tagline = loopName(t, i)
		t.ID = trailID(t)
	}
	return out, nil
}

// loopPairs chooses anchor pairs that make a triangle of about the right size.
func loopPairs(s geo.Point, as []loopAnchor, r float64, seed int64) [][2]loopAnchor {
	type ab struct {
		a   loopAnchor
		d   float64
		brg float64
	}
	var ring []ab
	for _, a := range as {
		d := geo.Haversine(s, a.p)
		if d >= 0.3*r && d <= 0.95*r {
			ring = append(ring, ab{a, d, geo.Bearing(s, a.p)})
		}
	}
	rng := rand.New(rand.NewSource(seed))
	sort.Slice(ring, func(i, j int) bool { return ring[i].a.w > ring[j].a.w })
	if len(ring) > 60 {
		ring = ring[:60]
	}
	targetPerim := 2.2 * r / 1.3 // road km run ~30% over straight lines
	type cand struct {
		pair [2]loopAnchor
		s    float64
		brg  float64
	}
	var cs []cand
	for i := range ring {
		for j := i + 1; j < len(ring); j++ {
			sep := math.Abs(math.Mod(ring[i].brg-ring[j].brg+540, 360) - 180)
			if sep < 30 || sep > 110 {
				continue
			}
			perim := ring[i].d + ring[j].d + geo.Haversine(ring[i].a.p, ring[j].a.p)
			fit := 1 - math.Min(1, math.Abs(perim-targetPerim)/targetPerim)
			score := (ring[i].a.w + ring[j].a.w) * (0.4 + 0.6*fit)
			if seed != 0 {
				score *= 0.75 + 0.5*rng.Float64()
			}
			// Order the pair clockwise so the loop doesn't cross itself.
			a, b := ring[i], ring[j]
			if math.Mod(b.brg-a.brg+360, 360) > 180 {
				a, b = b, a
			}
			mid := math.Mod(a.brg+math.Mod(b.brg-a.brg+360, 360)/2, 360)
			cs = append(cs, cand{[2]loopAnchor{a.a, b.a}, score, mid})
		}
	}
	sort.Slice(cs, func(i, j int) bool { return cs[i].s > cs[j].s })
	var out [][2]loopAnchor
	var used []float64
	for _, c := range cs {
		clash := false
		for _, u := range used {
			if math.Abs(math.Mod(c.brg-u+540, 360)-180) < 60 {
				clash = true
				break
			}
		}
		if clash {
			continue
		}
		used = append(used, c.brg)
		out = append(out, c.pair)
		if len(out) == 4 {
			break
		}
	}
	return out
}

func (p *Planner) roundTrip(ctx context.Context, v Vehicle, from geo.Point, lengthM float64, seed int64, depart time.Time) (*trail.Trail, error) {
	select {
	case p.sem <- struct{}{}:
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	defer func() { <-p.sem }()
	r := gh.Request{Profile: v.Profile, Locale: "en", Instructions: true, Elevation: true, Details: details,
		CustomModel: Model(trail.Loop, v), Algorithm: "round_trip", RoundTripDist: lengthM, RoundTripSeed: seed,
		Points: [][2]float64{{from.Lng, from.Lat}}}
	path, err := p.GH.Route(ctx, r)
	if err != nil {
		return nil, err
	}
	t := Analyze(path, Ctx{Land: p.Land, Vehicle: v, Depart: depart, Loc: p.Region.Location()})
	t.Kind = trail.Loop
	return t, nil
}

func (p *Planner) dedupeLoops(ts []*trail.Trail) []*trail.Trail {
	sort.SliceStable(ts, func(i, j int) bool { return ts[i].ScenicS > ts[j].ScenicS })
	var out []*trail.Trail
	var sigs []map[int]struct{}
	for _, t := range ts {
		sig := cells(p.Land.Grid, t.Line())
		dup := false
		for _, s := range sigs {
			if overlap(sig, s) > 0.7 {
				dup = true
				break
			}
		}
		if !dup {
			out = append(out, t)
			sigs = append(sigs, sig)
		}
		if len(out) == 3 {
			break
		}
	}
	return out
}

func loopName(t *trail.Trail, i int) (string, string) {
	var names []string
	for _, v := range t.Via {
		n := shortName(v.Name)
		if n != "" && !contains(names, n) {
			names = append(names, n)
		}
	}
	name := []string{"Morning Loop", "Long Way Round", "Wanderer's Loop"}[i%3]
	switch len(names) {
	case 1:
		name = "Loop via " + names[0]
	case 2:
		name = names[0] + " & " + names[1]
	}
	tag := fmt.Sprintf("%.0f km round trip", t.DistanceM/1000)
	for _, h := range t.Highlights {
		if h.Label != "" && h.Kind != "twisty" {
			tag += ". " + h.Label
			break
		}
	}
	return name, tag
}

// shortName trims "Ralamandal Wildlife Sanctuary" to "Ralamandal" for loop titles.
func shortName(n string) string {
	for _, suf := range []string{" Wildlife Sanctuary", " Sanctuary", " Reserve Forest", " National Park", " Palace", " Waterfall", " Falls", " Lake", " Dam", " River"} {
		if strings.HasSuffix(n, suf) && len(n) > len(suf)+2 {
			return strings.TrimSuffix(n, suf)
		}
	}
	return n
}

func (p *Planner) waterLoops(req WanderRequest, v Vehicle) ([]*trail.Trail, error) {
	if p.Water == nil {
		return nil, errf("unavailable", "Water routing is not loaded")
	}
	ts, err := p.Water.Loops(req.From, req.RadiusKm, v.CruiseKm)
	if err != nil {
		var we *water.Error
		if errors.As(err, &we) {
			return nil, &Error{Code: we.Code, Message: we.Message}
		}
		return nil, err
	}
	for i, t := range ts {
		p.decorateWater(t)
		name := "Lake Loop"
		if w, ok := p.Water.WaterAt(t.Line()[0]); ok && !strings.HasPrefix(w.Name, "Unnamed") {
			name = w.Name + " Loop"
		}
		if i > 0 {
			name += " II"
		}
		t.Name = name
		if t.Tagline == "" || strings.HasPrefix(t.Tagline, "On ") {
			t.Tagline = fmt.Sprintf("%.1f km on the water: out along the shore, back across", t.DistanceM/1000)
		}
	}
	return ts, nil
}
