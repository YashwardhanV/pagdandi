// Package mining finds desire paths in opt-in ride traces (SPEC §7.7):
// map-match, keep stretches that leave the network, index them as H3 cells,
// and promote sequences ridden by at least three distinct users.
package mining

import (
	"bytes"
	"context"
	"encoding/xml"
	"fmt"
	"log/slog"
	"math"
	"sort"
	"time"

	h3 "github.com/uber/h3-go/v4"

	"pagdandi/api/internal/geo"
	"pagdandi/api/internal/routing"
	"pagdandi/api/internal/store"
)

// Parameters from SPEC §7.7.
const (
	OffNetworkM    = 25.0  // trace must be this far from any routable edge
	MinStretchM    = 100.0 // for at least this long
	H3Resolution   = 11
	MinUsers       = 3 // k-anonymity
	WindowDays     = 30
	sampleSpacingM = 8.0
	overlapNeeded  = 0.6
)

// OffNetwork returns the stretches of trace that stay more than OffNetworkM
// from the matched network geometry for at least MinStretchM.
func OffNetwork(trace, network []geo.Point) [][]geo.Point {
	var out [][]geo.Point
	var cur []geo.Point
	flush := func() {
		if len(cur) >= 2 && geo.Length(cur) >= MinStretchM {
			out = append(out, cur)
		}
		cur = nil
	}
	for _, p := range trace {
		d := math.Inf(1)
		if len(network) > 0 {
			d = geo.Project(network, p).DistM
		}
		if d > OffNetworkM {
			cur = append(cur, p)
		} else {
			flush()
		}
	}
	flush()
	return out
}

// Densify inserts points so that no gap exceeds step metres.
func Densify(line []geo.Point, step float64) []geo.Point {
	if len(line) < 2 {
		return line
	}
	out := []geo.Point{line[0]}
	for i := 1; i < len(line); i++ {
		a, b := line[i-1], line[i]
		d := geo.Haversine(a, b)
		n := int(d / step)
		for k := 1; k <= n; k++ {
			t := float64(k) / float64(n+1)
			out = append(out, geo.Point{Lat: a.Lat + (b.Lat-a.Lat)*t, Lng: a.Lng + (b.Lng-a.Lng)*t})
		}
		out = append(out, b)
	}
	return out
}

// Cells returns the ordered sequence of distinct consecutive H3 cells along a line.
func Cells(line []geo.Point) []string {
	var out []string
	for _, p := range Densify(line, sampleSpacingM) {
		c, err := h3.LatLngToCell(h3.NewLatLng(p.Lat, p.Lng), H3Resolution)
		if err != nil {
			continue
		}
		s := c.String()
		if len(out) == 0 || out[len(out)-1] != s {
			out = append(out, s)
		}
	}
	return out
}

func expand(cells []string) map[string]bool {
	set := map[string]bool{}
	for _, s := range cells {
		c := h3.CellFromString(s)
		disk, err := h3.GridDisk(c, 1)
		if err != nil {
			set[s] = true
			continue
		}
		for _, d := range disk {
			set[d.String()] = true
		}
	}
	return set
}

// overlap is the share of b's cells that fall within one ring of a's cells.
func overlap(aExpanded map[string]bool, b []string) float64 {
	if len(b) == 0 {
		return 0
	}
	n := 0
	for _, s := range b {
		if aExpanded[s] {
			n++
		}
	}
	return float64(n) / float64(len(b))
}

// Segment is one user's off-network stretch.
type Segment struct {
	UserID    string
	RegionID  string
	Cells     []string
	Line      []geo.Point
	CreatedAt time.Time
}

// Sequence is a cluster of segments that follow the same cells.
type Sequence struct {
	Members        []int
	DistinctUsers  int
	Representative int // index of the segment that best matches the others
}

// Link groups segments whose cell sequences overlap (both ways ≥ 60 % within
// one H3 ring) and counts distinct users per group.
func Link(segs []Segment) []Sequence {
	n := len(segs)
	parent := make([]int, n)
	for i := range parent {
		parent[i] = i
	}
	var find func(int) int
	find = func(i int) int {
		for parent[i] != i {
			parent[i] = parent[parent[i]]
			i = parent[i]
		}
		return i
	}
	exp := make([]map[string]bool, n)
	for i, s := range segs {
		exp[i] = expand(s.Cells)
	}
	score := make([]float64, n)
	for i := 0; i < n; i++ {
		for j := i + 1; j < n; j++ {
			o1 := overlap(exp[i], segs[j].Cells)
			o2 := overlap(exp[j], segs[i].Cells)
			if math.Min(o1, o2) >= overlapNeeded {
				parent[find(i)] = find(j)
				score[i] += o2
				score[j] += o1
			}
		}
	}
	groups := map[int][]int{}
	for i := range segs {
		r := find(i)
		groups[r] = append(groups[r], i)
	}
	var out []Sequence
	for _, members := range groups {
		users := map[string]bool{}
		rep := members[0]
		for _, m := range members {
			users[segs[m].UserID] = true
			if score[m] > score[rep] || (score[m] == score[rep] && geo.Length(segs[m].Line) > geo.Length(segs[rep].Line)) {
				rep = m
			}
		}
		out = append(out, Sequence{Members: members, DistinctUsers: len(users), Representative: rep})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].DistinctUsers > out[j].DistinctUsers })
	return out
}

// Miner runs the nightly job.
type Miner struct {
	DB     *store.DB
	Graphs map[string]*routing.GH // country → GH
	// Country maps a region id to its country code.
	Country func(regionID string) string
	Log     *slog.Logger
	Now     func() time.Time
}

func (m *Miner) now() time.Time {
	if m.Now != nil {
		return m.Now()
	}
	return time.Now()
}

func gpx(line []geo.Point, ts []time.Time) []byte {
	type trkpt struct {
		Lat  float64 `xml:"lat,attr"`
		Lon  float64 `xml:"lon,attr"`
		Time string  `xml:"time,omitempty"`
	}
	var pts []trkpt
	for i, p := range line {
		tp := trkpt{Lat: p.Lat, Lon: p.Lng}
		if i < len(ts) {
			tp.Time = ts[i].UTC().Format(time.RFC3339)
		}
		pts = append(pts, tp)
	}
	var buf bytes.Buffer
	buf.WriteString(xml.Header)
	buf.WriteString(`<gpx version="1.1" creator="Pagdandi" xmlns="http://www.topografix.com/GPX/1/1"><trk><trkseg>`)
	for _, p := range pts {
		fmt.Fprintf(&buf, `<trkpt lat="%.7f" lon="%.7f">`, p.Lat, p.Lon)
		if p.Time != "" {
			fmt.Fprintf(&buf, "<time>%s</time>", p.Time)
		}
		buf.WriteString("</trkpt>")
	}
	buf.WriteString("</trkseg></trk></gpx>")
	return buf.Bytes()
}

func matchProfile(country string) string {
	if country == "SE" {
		return "bike_shortcut"
	}
	return "tw_adventure"
}

// ExtractNew map-matches new traces and stores their off-network stretches.
func (m *Miner) ExtractNew(ctx context.Context) error {
	traces, err := m.DB.TracesToMine(ctx, m.now().AddDate(0, 0, -WindowDays))
	if err != nil {
		return err
	}
	stored := 0
	for _, t := range traces {
		country := "IN"
		if m.Country != nil && t.RegionID != "" {
			country = m.Country(t.RegionID)
		}
		var network []geo.Point
		if gh := m.Graphs[country]; gh != nil {
			res, err := gh.Match(ctx, matchProfile(country), gpx(t.Line, t.Times))
			if err == nil && len(res.Paths) > 0 {
				network, _ = res.Paths[0].Line()
			} else if err != nil {
				m.Log.Warn("map match failed", "trace", t.ID, "err", err)
				continue
			}
		}
		for _, seg := range OffNetwork(t.Line, network) {
			if err := m.DB.SaveOffnetSegment(ctx, t.ID, t.UserID, t.RegionID, Cells(seg), seg); err != nil {
				return err
			}
			stored++
		}
	}
	m.Log.Info("desire-path extraction", "traces", len(traces), "segments", stored)
	return nil
}

// Aggregate links the last 30 days of segments and upserts candidates with ≥3 users.
func (m *Miner) Aggregate(ctx context.Context) error {
	rows, err := m.DB.OffnetSince(ctx, m.now().AddDate(0, 0, -WindowDays))
	if err != nil {
		return err
	}
	segs := make([]Segment, len(rows))
	for i, r := range rows {
		segs[i] = Segment{UserID: r.UserID, RegionID: r.RegionID, Cells: r.Cells, Line: r.Line, CreatedAt: r.CreatedAt}
	}
	n := 0
	for _, seq := range Link(segs) {
		if seq.DistinctUsers < MinUsers {
			continue
		}
		rep := segs[seq.Representative]
		first, last := rep.CreatedAt, rep.CreatedAt
		for _, i := range seq.Members {
			if segs[i].CreatedAt.Before(first) {
				first = segs[i].CreatedAt
			}
			if segs[i].CreatedAt.After(last) {
				last = segs[i].CreatedAt
			}
		}
		if rep.RegionID == "" {
			continue
		}
		if err := m.DB.UpsertCandidate(ctx, rep.RegionID, geo.Simplify(rep.Line, 4), seq.DistinctUsers, first, last); err != nil {
			return err
		}
		n++
	}
	m.Log.Info("desire-path aggregation", "segments", len(segs), "candidates", n)
	return nil
}
