package routing

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/oklog/ulid/v2"

	"pagdandi/api/internal/cache"
	"pagdandi/api/internal/geo"
	"pagdandi/api/internal/regions"
)

// RegionLookup finds the coverage region containing a point.
type RegionLookup interface {
	RegionAt(ctx context.Context, p geo.Point) (id string, ok bool, err error)
}

// Candidate is a community shortcut eligible for the overlay.
type Candidate struct {
	ID               string
	Title            string
	Line             []geo.Point
	LengthM          float64
	Oneway           bool
	Surface          string
	Flags            []string
	Confidence       float64
	LastConfirmedAt  *time.Time
	Confirmations30d int
}

// CandidateQuery selects overlay candidates (SPEC §6.3 step 4).
type CandidateQuery struct {
	RegionID      string
	Vehicle       string
	From, To      geo.Point
	MajorAxisM    float64
	MinConfidence float64
}

// Barrier is an OSM barrier node near a route.
type Barrier struct {
	ID     int64
	Point  geo.Point
	Kind   string
	Access string
}

// Store is the data the router needs from Postgres.
type Store interface {
	Candidates(ctx context.Context, q CandidateQuery) ([]Candidate, error)
	BarriersNear(ctx context.Context, line []geo.Point, distM float64) ([]Barrier, error)
}

// Router orchestrates route requests.
type Router struct {
	Regions *regions.Set
	Lookup  RegionLookup
	Graphs  map[string]*GH // by country code
	Store   Store
	Cache   cache.Cache
	Now     func() time.Time
	Log     *slog.Logger
	// CacheTTL defaults to 10 minutes.
	CacheTTL time.Duration
	// OnCommunityEval, if set, receives how many candidates were evaluated (for metrics/tests).
	OnCommunityEval func(found, evaluated int)
	// Seeded, if set, limits "nearest region" suggestions to regions that have data.
	Seeded func(id string) bool
}

var details = []string{"surface", "road_class", "track_type", "road_access", "max_speed"}

func (r *Router) now() time.Time {
	if r.Now != nil {
		return r.Now()
	}
	return time.Now()
}

func newID() string { return ulid.MustNew(ulid.Timestamp(time.Now()), rand.Reader).String() }

func ghLocale(lang string) string {
	switch lang {
	case "sv":
		return "sv_SE"
	case "hi":
		return "hi"
	default:
		return "en"
	}
}

// Validate normalises and checks a request against its region.
func (r *Router) validate(req *Request, reg regions.Region) error {
	if req.Mode == "" {
		req.Mode = "auto"
	}
	okMode := false
	for _, m := range Modes {
		if m == req.Mode {
			okMode = true
		}
	}
	if !okMode {
		return &ValidationError{"mode must be one of auto, standard, shortcut, adventure"}
	}
	if req.Vehicle == "" {
		req.Vehicle = reg.DefaultVehicle
	}
	if !ValidVehicle(reg.Country, req.Vehicle) {
		return &ValidationError{fmt.Sprintf("vehicle %q is not available in %s", req.Vehicle, reg.Name)}
	}
	switch req.Lang {
	case "en", "hi", "sv":
	case "":
		req.Lang = "en"
	default:
		return &ValidationError{"lang must be en, hi or sv"}
	}
	return nil
}

func (r *Router) resolveRegion(ctx context.Context, p geo.Point, which string) (regions.Region, error) {
	id, ok, err := r.Lookup.RegionAt(ctx, p)
	if err != nil {
		return regions.Region{}, err
	}
	if !ok {
		near, d := r.Regions.NearestWhere(p, r.Seeded)
		return regions.Region{}, &OutOfCoverageError{Which: which, NearestRegion: near.ID, NearestName: near.Name, NearestDistKm: math.Round(d/100) / 10}
	}
	reg, ok := r.Regions.Get(id)
	if !ok {
		return regions.Region{}, fmt.Errorf("region %q not in regions.yaml", id)
	}
	return reg, nil
}

// Route answers POST /v1/route.
func (r *Router) Route(ctx context.Context, req Request) (*Response, error) {
	if !req.From.Valid() || !req.To.Valid() {
		return nil, &ValidationError{"from and to must be finite lat/lng"}
	}
	if geo.Haversine(req.From, req.To) < 20 {
		return nil, &ValidationError{"from and to are the same place"}
	}
	reg, err := r.resolveRegion(ctx, req.From, "from")
	if err != nil {
		return nil, err
	}
	regTo, err := r.resolveRegion(ctx, req.To, "to")
	if err != nil {
		return nil, err
	}
	if regTo.Country != reg.Country {
		return nil, ErrCrossRegion
	}
	if err := r.validate(&req, reg); err != nil {
		return nil, err
	}
	gh := r.Graphs[reg.Country]
	if gh == nil {
		return nil, fmt.Errorf("no routing graph configured for %s", reg.Country)
	}

	at := r.now()
	if req.DepartAt != nil {
		at = *req.DepartAt
	}
	daylight := IsDaylight(at, req.From.Lat, req.From.Lng)
	season := reg.ActiveSeason(at)
	plan := PlanProfiles(reg.Country, req.Vehicle, req.Mode, daylight)

	seasonName := "none"
	var seasonPtr *string
	if season != nil {
		seasonName = season.Name
		s := season.Name
		seasonPtr = &s
	}
	key := cache.Key("route", "v1", cache.Round5(req.From.Lat), cache.Round5(req.From.Lng), cache.Round5(req.To.Lat), cache.Round5(req.To.Lng),
		req.Vehicle, req.Mode, plan.Standard, plan.Shortcut, seasonName, fmt.Sprint(daylight), req.Lang)
	if r.Cache != nil {
		if b, ok, _ := r.Cache.Get(ctx, key); ok {
			var cached Response
			if json.Unmarshal(b, &cached) == nil {
				cached.RequestID = newID()
				cached.Cached = true
				r.remember(ctx, &cached)
				return &cached, nil
			}
		}
	}

	resp := &Response{
		RequestID:   newID(),
		Region:      reg.ID,
		Attribution: Attribution,
		Context: Context{
			Daylight: daylight, Season: seasonPtr, Vehicle: req.Vehicle, Mode: req.Mode,
			Profiles: compact([]string{plan.Standard, plan.Shortcut}),
		},
	}
	if season != nil {
		resp.Context.LandslideRisk = season.LandslideRisk
	}
	if rise, set, ok := SunTimes(at, req.From.Lat, req.From.Lng); ok {
		resp.Context.Sunrise, resp.Context.Sunset = rise.Format(time.RFC3339), set.Format(time.RFC3339)
	}
	model := SeasonModel(season)
	base := RouteRequest{
		Points:       [][2]float64{{req.From.Lng, req.From.Lat}, {req.To.Lng, req.To.Lat}},
		Locale:       ghLocale(req.Lang),
		Instructions: true,
		Details:      details,
		CustomModel:  model,
	}

	// Step 3: standard, shortcut alternatives and community candidates in parallel.
	var (
		wg                  sync.WaitGroup
		stdResp, scResp     *RouteResponse
		stdErr, scErr, cErr error
		cands               []Candidate
	)
	wg.Add(1)
	go func() {
		defer wg.Done()
		q := base
		q.Profile = plan.Standard
		stdResp, stdErr = gh.Route(ctx, q)
	}()
	if plan.Shortcut != "" {
		wg.Add(2)
		go func() {
			defer wg.Done()
			q := base
			q.Profile = plan.Shortcut
			q.Algorithm = "alternative_route"
			q.MaxPaths = 3
			q.MaxWeightF = 1.6
			q.MaxShareF = 0.7
			scResp, scErr = gh.Route(ctx, q)
			if scErr != nil && !IsNoRoute(scErr) {
				// Some graphs cannot do alternatives with the configured preparation; retry plain.
				q.Algorithm, q.MaxPaths, q.MaxWeightF, q.MaxShareF = "", 0, 0, 0
				scResp, scErr = gh.Route(ctx, q)
			}
		}()
		go func() {
			defer wg.Done()
			if r.Store == nil {
				return
			}
			// The ellipse needs the standard distance, which is not known yet:
			// query generously now and filter precisely afterwards.
			major := 1.25 * 1.6 * geo.Haversine(req.From, req.To)
			cands, cErr = r.Store.Candidates(ctx, CandidateQuery{
				RegionID: reg.ID, Vehicle: req.Vehicle, From: req.From, To: req.To,
				MajorAxisM: major, MinConfidence: plan.MinCommunityConfidence,
			})
		}()
	}
	wg.Wait()
	if stdErr != nil {
		if IsNoRoute(stdErr) {
			return nil, ErrNoRoute
		}
		return nil, fmt.Errorf("standard route: %w", stdErr)
	}
	if cErr != nil && r.Log != nil {
		r.Log.Warn("community candidates failed", "err", cErr)
	}

	stdPath := &stdResp.Paths[0]
	std, err := r.optionFromPath("std", "standard", plan.Standard, stdPath)
	if err != nil {
		return nil, err
	}
	resp.Options = append(resp.Options, *std)

	var shortcuts []Option
	if scErr == nil && scResp != nil {
		for i := range scResp.Paths {
			opt, err := r.optionFromPath("", "shortcut", plan.Shortcut, &scResp.Paths[i])
			if err != nil {
				continue
			}
			shortcuts = append(shortcuts, *opt)
		}
	} else if scErr != nil && !IsNoRoute(scErr) && r.Log != nil {
		r.Log.Warn("shortcut route failed", "err", scErr)
	}

	// Step 4: community overlay.
	if len(cands) > 0 && plan.Shortcut != "" {
		best := float64(std.DistanceM)
		for _, s := range shortcuts {
			best = math.Min(best, float64(s.DistanceM))
		}
		if opt := r.communityOption(ctx, gh, base, plan, req, cands, float64(std.DistanceM), best); opt != nil {
			shortcuts = append(shortcuts, *opt)
		}
	}

	shortcuts = dedupe(*std, shortcuts)
	sort.SliceStable(shortcuts, func(i, j int) bool { return shortcuts[i].DurationS < shortcuts[j].DurationS })
	if len(shortcuts) > 3 {
		shortcuts = shortcuts[:3]
	}
	for i := range shortcuts {
		shortcuts[i].ID = fmt.Sprintf("sc%d", i+1)
	}
	resp.Options = append(resp.Options, shortcuts...)

	// Step 5: warnings (gates need the DB; run per option in parallel).
	r.addBarrierWarnings(ctx, resp.Options)
	for i := range resp.Options {
		o := &resp.Options[i]
		addContextWarnings(o, daylight, season)
		if o.Kind == "shortcut" {
			o.Savings = &Savings{DistanceM: std.DistanceM - o.DistanceM, DurationS: std.DurationS - o.DurationS}
		}
	}

	// Step 6: recommendation.
	resp.Recommended = "std"
	if len(shortcuts) > 0 {
		sc := resp.Options[1]
		if ShouldRecommendShortcut(RecommendInput{
			StdDistanceM: std.DistanceM, StdDurationS: std.DurationS,
			ScDistanceM: sc.DistanceM, ScDurationS: sc.DurationS,
			Confidence: sc.Confidence, Daylight: daylight, TrackM: sc.SurfaceBreakdown.TrackM,
		}) {
			resp.Recommended = sc.ID
		}
		resp.Disclaimer = Disclaimer
	}

	// Step 7: cache.
	if r.Cache != nil {
		if b, err := json.Marshal(resp); err == nil {
			ttl := r.CacheTTL
			if ttl == 0 {
				ttl = 10 * time.Minute
			}
			_ = r.Cache.Set(ctx, key, b, ttl)
		}
		r.remember(ctx, resp)
	}
	return resp, nil
}

// remember stores a response by request id for GPX export and reroute context.
func (r *Router) remember(ctx context.Context, resp *Response) {
	if r.Cache == nil {
		return
	}
	if b, err := json.Marshal(resp); err == nil {
		_ = r.Cache.Set(ctx, cache.Key("routeresp", resp.RequestID), b, 2*time.Hour)
	}
}

// Lookup returns a stored route response by request id.
func (r *Router) Stored(ctx context.Context, id string) (*Response, bool) {
	if r.Cache == nil {
		return nil, false
	}
	b, ok, err := r.Cache.Get(ctx, cache.Key("routeresp", id))
	if err != nil || !ok {
		return nil, false
	}
	var resp Response
	if json.Unmarshal(b, &resp) != nil {
		return nil, false
	}
	return &resp, true
}

func compact(ss []string) []string {
	out := ss[:0]
	for _, s := range ss {
		if s != "" {
			out = append(out, s)
		}
	}
	return out
}

func (r *Router) optionFromPath(id, kind, profile string, p *Path) (*Option, error) {
	line, err := p.Line()
	if err != nil {
		return nil, err
	}
	spans := Spans(len(line), p.Details)
	o := &Option{
		ID: id, Kind: kind, Profile: profile,
		DistanceM:        int(math.Round(p.Distance)),
		DurationS:        int(math.Round(float64(p.Time) / 1000)),
		Polyline:         p.Points,
		Legs:             []Leg{{Source: "osm", FromIdx: 0, ToIdx: max(0, len(line)-1)}},
		SurfaceSpans:     spans,
		SurfaceBreakdown: Breakdown(line, spans),
		Instructions:     ConvertInstructions(p.Instructions, 0),
		Warnings:         UnpavedWarnings(line, spans, 100),
	}
	if o.Warnings == nil {
		o.Warnings = []Warning{}
	}
	return o, nil
}

// dedupe drops shortcut options that are effectively the standard route or each other.
func dedupe(std Option, opts []Option) []Option {
	same := func(a, b Option) bool {
		dd := math.Abs(float64(a.DistanceM - b.DistanceM))
		dt := math.Abs(float64(a.DurationS - b.DurationS))
		hasCommunity := func(o Option) bool { return len(o.Legs) > 1 }
		if hasCommunity(a) != hasCommunity(b) {
			return false
		}
		return dd <= math.Max(30, 0.01*float64(a.DistanceM)) && dt <= 20
	}
	var out []Option
	for _, o := range opts {
		if same(std, o) {
			continue
		}
		dup := false
		for _, k := range out {
			if same(k, o) {
				dup = true
				break
			}
		}
		if !dup {
			out = append(out, o)
		}
	}
	return out
}

type directed struct {
	c      Candidate
	line   []geo.Point // oriented A→B
	lb     float64
	totalS float64
	oa, bd *Path
}

// communityOption implements the overlay of SPEC §6.3 step 4 and returns the
// best route that uses exactly one community shortcut, or nil.
func (r *Router) communityOption(ctx context.Context, gh *GH, base RouteRequest, plan ProfilePlan, req Request, cands []Candidate, stdDist, bestSoFar float64) *Option {
	major := 1.25 * stdDist
	var ds []directed
	for _, c := range cands {
		if len(c.Line) < 2 {
			continue
		}
		dirs := [][]geo.Point{c.Line}
		if !c.Oneway {
			rev := make([]geo.Point, len(c.Line))
			for i, p := range c.Line {
				rev[len(c.Line)-1-i] = p
			}
			dirs = append(dirs, rev)
		}
		for _, l := range dirs {
			a, b := l[0], l[len(l)-1]
			if !geo.InEllipse(a, req.From, req.To, major) || !geo.InEllipse(b, req.From, req.To, major) {
				continue
			}
			lb := geo.Haversine(req.From, a) + c.LengthM + geo.Haversine(b, req.To)
			if lb >= bestSoFar {
				continue
			}
			ds = append(ds, directed{c: c, line: l, lb: lb})
		}
	}
	sort.Slice(ds, func(i, j int) bool { return ds[i].lb < ds[j].lb })
	if len(ds) > 5 {
		ds = ds[:5]
	}
	if r.OnCommunityEval != nil {
		r.OnCommunityEval(len(cands), len(ds))
	}
	if len(ds) == 0 {
		return nil
	}
	leg := func(from, to geo.Point) (*Path, error) {
		if geo.Haversine(from, to) < 5 {
			return nil, nil
		}
		q := base
		q.Profile = plan.Shortcut
		q.Points = [][2]float64{{from.Lng, from.Lat}, {to.Lng, to.Lat}}
		res, err := gh.Route(ctx, q)
		if err != nil {
			return nil, err
		}
		return &res.Paths[0], nil
	}
	var wg sync.WaitGroup
	for i := range ds {
		wg.Add(1)
		go func(d *directed) {
			defer wg.Done()
			a, b := d.line[0], d.line[len(d.line)-1]
			var e1, e2 error
			var inner sync.WaitGroup
			inner.Add(2)
			go func() { defer inner.Done(); d.oa, e1 = leg(req.From, a) }()
			go func() { defer inner.Done(); d.bd, e2 = leg(b, req.To) }()
			inner.Wait()
			if e1 != nil || e2 != nil {
				d.totalS = math.Inf(1)
				return
			}
			t := d.c.LengthM / (CommunitySpeedKmh(req.Vehicle, d.c.Surface) / 3.6)
			if d.oa != nil {
				t += float64(d.oa.Time) / 1000
			}
			if d.bd != nil {
				t += float64(d.bd.Time) / 1000
			}
			d.totalS = t
		}(&ds[i])
	}
	wg.Wait()
	bi := -1
	for i := range ds {
		if !math.IsInf(ds[i].totalS, 1) && (bi < 0 || ds[i].totalS < ds[bi].totalS) {
			bi = i
		}
	}
	if bi < 0 {
		return nil
	}
	return r.stitch(ds[bi], plan, req.Vehicle)
}

// stitch joins O→A, the community shortcut A→B and B→D into one option.
func (r *Router) stitch(d directed, plan ProfilePlan, vehicle string) *Option {
	var pts []geo.Point
	var spans []SurfaceSpan
	var instrs []Instr
	var legs []Leg
	var dist, dur float64

	// join appends line to pts, sharing the join vertex, and returns the index
	// in pts where line[0] now sits.
	join := func(line []geo.Point) int {
		if len(pts) == 0 {
			pts = append(pts, line...)
			return 0
		}
		off := len(pts) - 1
		pts = append(pts, line[1:]...)
		return off
	}

	appendPath := func(p *Path, last bool) {
		if p == nil {
			return
		}
		line, err := p.Line()
		if err != nil || len(line) < 2 {
			return
		}
		off := join(line)
		for _, s := range Spans(len(line), p.Details) {
			spans = append(spans, SurfaceSpan{FromIdx: s.FromIdx + off, ToIdx: s.ToIdx + off, Kind: s.Kind})
		}
		ins := ConvertInstructions(p.Instructions, off)
		if !last && len(ins) > 0 && ins[len(ins)-1].Sign == SignFinish {
			ins = ins[:len(ins)-1]
		}
		instrs = append(instrs, ins...)
		legs = append(legs, Leg{Source: "osm", FromIdx: off, ToIdx: len(pts) - 1})
		dist += p.Distance
		dur += float64(p.Time) / 1000
	}

	appendPath(d.oa, false)

	// Community leg.
	start := join(d.line)
	kind := d.c.Surface
	if kind != SurfPaved && kind != SurfUnpaved && kind != SurfTrack {
		kind = SurfUnpaved
	}
	spans = append(spans, SurfaceSpan{FromIdx: start, ToIdx: len(pts) - 1, Kind: kind})
	scTime := d.c.LengthM / (CommunitySpeedKmh(vehicle, d.c.Surface) / 3.6)
	label := "Take the rider shortcut"
	if d.c.Title != "" {
		label += " (" + d.c.Title + ")"
	}
	instrs = append(instrs, Instr{
		Text: label, DistanceM: int(math.Round(d.c.LengthM)), DurationS: int(math.Round(scTime)),
		Sign: SignContinue, Interval: [2]int{start, len(pts) - 1},
	})
	legs = append(legs, Leg{Source: "community", ShortcutID: d.c.ID, Title: d.c.Title, FromIdx: start, ToIdx: len(pts) - 1})
	dist += d.c.LengthM
	dur += scTime

	appendPath(d.bd, true)
	if len(instrs) == 0 || instrs[len(instrs)-1].Sign != SignFinish {
		instrs = append(instrs, Instr{Text: "Arrive at destination", Sign: SignFinish, Interval: [2]int{len(pts) - 1, len(pts) - 1}})
	}

	spans = mergeAdjacent(spans)
	conf := d.c.Confidence
	n30 := d.c.Confirmations30d
	o := &Option{
		Kind: "shortcut", Profile: plan.Shortcut,
		DistanceM: int(math.Round(dist)), DurationS: int(math.Round(dur)),
		Polyline: geo.Encode(pts), Legs: legs,
		SurfaceSpans: spans, SurfaceBreakdown: Breakdown(pts, spans),
		Confidence: &conf, LastConfirmedAt: d.c.LastConfirmedAt, Confirmations30d: &n30,
		Instructions: instrs,
	}
	o.Warnings = UnpavedWarnings(pts, spans, 100)
	if o.Warnings == nil {
		o.Warnings = []Warning{}
	}
	cum := geo.CumulativeDistances(pts)
	atStart := int(math.Round(cum[min(start, len(cum)-1)]))
	for _, f := range d.c.Flags {
		w := Warning{AtM: &atStart}
		switch f {
		case "gate":
			w.Type, w.Detail = "gate", "Gate on the rider shortcut, may be closed at night"
		case "floods_in_rain":
			w.Type, w.Detail = "floods", "Floods in rain"
		case "steep":
			w.Type = "steep"
		case "narrow":
			w.Type = "narrow"
		case "no_lights":
			w.Type = "no_lights"
		case "livestock":
			w.Type = "livestock"
		default:
			continue
		}
		o.Warnings = append(o.Warnings, w)
	}
	if conf < 0.6 {
		o.Warnings = append(o.Warnings, Warning{Type: "low_confidence", AtM: &atStart, Detail: fmt.Sprintf("%.0f%% confidence", conf*100)})
	}
	return o
}

func mergeAdjacent(spans []SurfaceSpan) []SurfaceSpan {
	var out []SurfaceSpan
	for _, s := range spans {
		if s.ToIdx <= s.FromIdx {
			continue
		}
		if n := len(out); n > 0 && out[n-1].Kind == s.Kind && out[n-1].ToIdx >= s.FromIdx {
			out[n-1].ToIdx = max(out[n-1].ToIdx, s.ToIdx)
			continue
		}
		out = append(out, s)
	}
	return out
}

func (r *Router) addBarrierWarnings(ctx context.Context, opts []Option) {
	if r.Store == nil {
		return
	}
	var wg sync.WaitGroup
	for i := range opts {
		wg.Add(1)
		go func(o *Option) {
			defer wg.Done()
			line, err := geo.Decode(o.Polyline)
			if err != nil || len(line) < 2 {
				return
			}
			bs, err := r.Store.BarriersNear(ctx, line, 8)
			if err != nil {
				if r.Log != nil && !errors.Is(err, context.Canceled) {
					r.Log.Warn("barrier lookup failed", "err", err)
				}
				return
			}
			for _, b := range bs {
				pr := geo.Project(line, b.Point)
				at := int(math.Round(pr.AlongM))
				detail := "Gate here may be closed after dark"
				if strings.Contains(b.Access, "permissive") || b.Access == "yes" {
					detail = "Gate, usually open"
				}
				o.Warnings = append(o.Warnings, Warning{Type: "gate", AtM: &at, Detail: detail, Lat: b.Point.Lat, Lng: b.Point.Lng})
			}
			sort.SliceStable(o.Warnings, func(i, j int) bool { return atM(o.Warnings[i]) < atM(o.Warnings[j]) })
		}(&opts[i])
	}
	wg.Wait()
}

func atM(w Warning) int {
	if w.AtM == nil {
		return -1
	}
	return *w.AtM
}

// addContextWarnings adds night and seasonal warnings to options with unpaved road.
func addContextWarnings(o *Option, daylight bool, season *regions.Season) {
	rough := o.SurfaceBreakdown.UnpavedM + o.SurfaceBreakdown.TrackM
	if rough == 0 {
		return
	}
	if !daylight {
		o.Warnings = append(o.Warnings, Warning{Type: "night", Detail: "Unpaved road after dark: ride slowly"})
	}
	if season != nil {
		w := Warning{Type: "seasonal", Season: season.Name, Detail: "Unpaved stretches may be slippery or flooded this season"}
		if season.LandslideRisk {
			w.Detail = "Landslide-prone season: check local conditions"
		}
		o.Warnings = append(o.Warnings, w)
	}
}
