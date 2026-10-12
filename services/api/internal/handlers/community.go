package handlers

import (
	"errors"
	"fmt"
	"math"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"time"

	"pagdandi/api/internal/auth"
	"pagdandi/api/internal/geo"
	"pagdandi/api/internal/httpx"
	"pagdandi/api/internal/shortcuts"
	"pagdandi/api/internal/store"
)

// SnapMaxM is the endpoint snapping tolerance (SPEC §7.1).
const SnapMaxM = 30.0

var (
	allowedFlags    = []string{"gate", "floods_in_rain", "steep", "narrow", "no_lights", "livestock"}
	allowedSurfaces = []string{"paved", "unpaved", "track"}
	allowedVerdicts = []string{"passable", "blocked", "partly"}
	allowedReasons  = []string{"gate_locked", "flooded", "construction", "private", "unsafe", "missing", "other"}
)

type pointT struct {
	Lat float64    `json:"lat"`
	Lng float64    `json:"lng"`
	T   *time.Time `json:"t,omitempty"`
}

func cleanList(in, allowed []string, field string) ([]string, error) {
	out := []string{}
	for _, v := range in {
		if !slices.Contains(allowed, v) {
			return nil, badParam(field, fmt.Sprintf("unknown value %q", v))
		}
		if !slices.Contains(out, v) {
			out = append(out, v)
		}
	}
	return out, nil
}

func cleanTitle(t *string) (*string, error) {
	if t == nil {
		return nil, nil
	}
	s := strings.TrimSpace(*t)
	if s == "" {
		return nil, nil
	}
	if len([]rune(s)) > 80 {
		return nil, badParam("title", "at most 80 characters")
	}
	return &s, nil
}

type snapResult struct {
	OK        bool    `json:"ok"`
	NodeID    int64   `json:"node_id,omitempty"`
	Lat       float64 `json:"lat"`
	Lng       float64 `json:"lng"`
	DistanceM float64 `json:"distance_m"`
}

func (s *Server) snapPoint(r *http.Request, p geo.Point) (snapResult, error) {
	id, at, d, err := s.DB.NearestNode(r.Context(), p, SnapMaxM)
	if errors.Is(err, store.ErrNotFound) {
		return snapResult{OK: false, Lat: at.Lat, Lng: at.Lng, DistanceM: math.Round(d)}, nil
	}
	if err != nil {
		return snapResult{}, err
	}
	return snapResult{OK: true, NodeID: id, Lat: at.Lat, Lng: at.Lng, DistanceM: math.Round(d*10) / 10}, nil
}

// snap answers POST /v1/shortcuts/snap so the draw tool can show snapping feedback.
func (s *Server) snap(w http.ResponseWriter, r *http.Request) error {
	var p geo.Point
	if err := httpx.DecodeJSON(w, r, &p, 1<<10); err != nil {
		return err
	}
	if !p.Valid() {
		return badParam("lat/lng", "out of range")
	}
	res, err := s.snapPoint(r, p)
	if err != nil {
		return err
	}
	httpx.WriteJSON(w, http.StatusOK, res)
	return nil
}

type createShortcutBody struct {
	Points   []pointT `json:"points"`
	Source   string   `json:"source"` // drawn | recorded
	Title    *string  `json:"title"`
	Surface  string   `json:"surface"`
	Vehicles []string `json:"vehicles"`
	Flags    []string `json:"flags"`
	Oneway   bool     `json:"oneway"`
}

func (s *Server) createShortcut(w http.ResponseWriter, r *http.Request) error {
	u, err := auth.Require(r)
	if err != nil {
		return err
	}
	var b createShortcutBody
	if err := httpx.DecodeJSON(w, r, &b, 512<<10); err != nil {
		return err
	}
	if b.Source != "drawn" && b.Source != "recorded" {
		return badParam("source", "must be drawn or recorded")
	}
	if len(b.Points) < 2 || len(b.Points) > 5000 {
		return badParam("points", "need between 2 and 5000 points")
	}
	if !slices.Contains(allowedSurfaces, b.Surface) {
		return badParam("surface", "must be paved, unpaved or track")
	}
	flags, err := cleanList(b.Flags, allowedFlags, "flags")
	if err != nil {
		return err
	}
	title, err := cleanTitle(b.Title)
	if err != nil {
		return err
	}
	line := make([]geo.Point, len(b.Points))
	for i, p := range b.Points {
		line[i] = geo.Point{Lat: p.Lat, Lng: p.Lng}
		if !line[i].Valid() {
			return badParam("points", "invalid coordinate")
		}
	}
	if b.Source == "recorded" {
		line = geo.Simplify(line, 3)
	}
	length := geo.Length(line)
	if length < 30 || length > 20000 {
		return httpx.Err(http.StatusUnprocessableEntity, "BAD_LENGTH", "A shortcut must be between 30 m and 20 km long.")
	}
	regionID, ok, err := s.DB.RegionAt(r.Context(), line[0])
	if err != nil {
		return err
	}
	if !ok {
		return httpx.Err(http.StatusUnprocessableEntity, "OUT_OF_COVERAGE", "This shortcut is outside the area Pagdandi covers.")
	}
	reg, _ := s.Regions.Get(regionID)
	vehicles := b.Vehicles
	if len(vehicles) == 0 {
		vehicles = reg.Vehicles()
	}
	if vehicles, err = cleanList(vehicles, append([]string{}, reg.Vehicles()...), "vehicles"); err != nil {
		return err
	}
	if n, err := s.DB.CountUserShortcutsToday(r.Context(), u.ID); err == nil && n >= 10 {
		return httpx.Err(http.StatusTooManyRequests, "DAILY_LIMIT", "You can add up to 10 shortcuts a day.")
	}

	start, err := s.snapPoint(r, line[0])
	if err != nil {
		return err
	}
	end, err := s.snapPoint(r, line[len(line)-1])
	if err != nil {
		return err
	}
	if !start.OK || !end.OK {
		which, res := "start", start
		if start.OK {
			which, res = "end", end
		}
		e := httpx.Err(http.StatusUnprocessableEntity, "SNAP_FAILED",
			fmt.Sprintf("The %s of your shortcut must touch a road. It is %.0f m from the nearest one; move it within %.0f m.", which, res.DistanceM, SnapMaxM))
		e.Details = map[string]any{"which": which, "distance_m": res.DistanceM, "nearest": map[string]float64{"lat": res.Lat, "lng": res.Lng}}
		return e
	}
	if start.NodeID == end.NodeID {
		return httpx.Err(http.StatusUnprocessableEntity, "SNAP_FAILED", "Both ends snap to the same road point. Draw a longer shortcut.")
	}
	line[0] = geo.Point{Lat: start.Lat, Lng: start.Lng}
	line[len(line)-1] = geo.Point{Lat: end.Lat, Lng: end.Lng}

	if b.Source == "recorded" {
		ts := make([]time.Time, 0, len(b.Points))
		for _, p := range b.Points {
			if p.T != nil {
				ts = append(ts, *p.T)
			}
		}
		raw := make([]geo.Point, len(b.Points))
		for i, p := range b.Points {
			raw[i] = geo.Point{Lat: p.Lat, Lng: p.Lng}
		}
		if len(ts) == len(raw) {
			_, _ = s.DB.SaveTrace(r.Context(), u.ID, regionID, raw, ts, "shortcut_submission")
		}
	}

	status := shortcuts.StatusPending
	if shortcuts.ClampTrust(u.Trust) >= 1.5 {
		status = shortcuts.StatusActive
	}
	sc, err := s.DB.CreateShortcut(r.Context(), store.NewShortcut{
		RegionID: regionID, Title: title, Line: line, LengthM: int(math.Round(geo.Length(line))),
		StartNode: start.NodeID, EndNode: end.NodeID, Oneway: b.Oneway, Surface: b.Surface,
		Vehicles: vehicles, Flags: flags, Status: status, CreatedBy: u.ID,
	})
	if err != nil {
		return err
	}
	return writeJSONStatus(w, http.StatusCreated, map[string]any{
		"shortcut": sc,
		"message":  "Thanks. Your shortcut goes live after two riders confirm it.",
	})
}

func zoomTolerance(r *http.Request) float64 {
	z, err := strconv.ParseFloat(r.URL.Query().Get("zoom"), 64)
	if err != nil || z < 0 || z > 22 {
		z = 14
	}
	// About two pixels at this zoom.
	return 2 * 360 / (256 * math.Pow(2, z))
}

func (s *Server) listShortcuts(w http.ResponseWriter, r *http.Request) error {
	b, err := bboxParam(r, 1.5)
	if err != nil {
		return err
	}
	includeLow := r.URL.Query().Get("adventure") == "1"
	list, err := s.DB.ShortcutsInBBox(r.Context(), b, zoomTolerance(r), includeLow, 500)
	if err != nil {
		return err
	}
	if list == nil {
		list = []store.Shortcut{}
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"shortcuts": list})
	return nil
}

type historyPoint struct {
	Day        string  `json:"day"`
	Confidence float64 `json:"confidence"`
}

func (s *Server) getShortcut(w http.ResponseWriter, r *http.Request) error {
	sc, err := s.DB.GetShortcut(r.Context(), r.PathValue("id"))
	if errors.Is(err, store.ErrNotFound) || (err == nil && sc.Status == shortcuts.StatusRemoved) {
		return httpx.Err(http.StatusNotFound, "NOT_FOUND", "Shortcut not found.")
	}
	if err != nil {
		if strings.Contains(err.Error(), "uuid") {
			return httpx.Err(http.StatusNotFound, "NOT_FOUND", "Shortcut not found.")
		}
		return err
	}
	reports, err := s.DB.ReportsFor(r.Context(), sc.ID)
	if err != nil {
		return err
	}
	summary, err := s.DB.RecentReportSummary(r.Context(), sc.ID)
	if err != nil {
		return err
	}
	if summary == nil {
		summary = []store.ReportSummary{}
	}
	// Confidence history: the posterior as it stood at the end of each of the last 90 days.
	reg, _ := s.Regions.Get(sc.RegionID)
	var hist []historyPoint
	today := time.Now().UTC().Truncate(24 * time.Hour)
	now := time.Now()
	for d := 87; d >= 0; d -= 3 { // every third day, always ending today
		at := today.AddDate(0, 0, -d).Add(24*time.Hour - time.Second)
		if at.After(now) {
			at = now
		}
		if at.Before(sc.CreatedAt) {
			continue
		}
		var upto []shortcuts.Report
		for _, rp := range reports {
			if !rp.CreatedAt.After(at) {
				upto = append(upto, rp)
			}
		}
		_, _, c := shortcuts.Confidence(upto, at, reg.HalfLifeDays(at))
		hist = append(hist, historyPoint{Day: at.Format("2006-01-02"), Confidence: math.Round(c*1000) / 1000})
	}
	if hist == nil {
		hist = []historyPoint{}
	}
	confirmations30 := 0
	for _, rp := range reports {
		if rp.Verdict == shortcuts.VerdictPassable && time.Since(rp.CreatedAt) < 30*24*time.Hour {
			confirmations30++
		}
	}
	mine := false
	if u := auth.Current(r); u != nil && sc.CreatedBy != nil && *sc.CreatedBy == u.ID {
		mine = true
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{
		"shortcut": sc, "reports": summary, "history": hist, "confirmations_30d": confirmations30,
		"report_count": len(reports), "mine": mine,
		"osm_edit_url": fmt.Sprintf("https://www.openstreetmap.org/edit#map=18/%.5f/%.5f", sc.Line[len(sc.Line)/2].Lat, sc.Line[len(sc.Line)/2].Lng),
	})
	return nil
}

type patchShortcutBody struct {
	Title    *string  `json:"title"`
	Flags    []string `json:"flags"`
	Vehicles []string `json:"vehicles"`
	Surface  *string  `json:"surface"`
}

func (s *Server) patchShortcut(w http.ResponseWriter, r *http.Request) error {
	u, err := auth.Require(r)
	if err != nil {
		return err
	}
	sc, err := s.DB.GetShortcut(r.Context(), r.PathValue("id"))
	if err != nil {
		return httpx.Err(http.StatusNotFound, "NOT_FOUND", "Shortcut not found.")
	}
	isOwner := sc.CreatedBy != nil && *sc.CreatedBy == u.ID
	if !isOwner && u.Role != "moderator" && u.Role != "admin" {
		return httpx.Err(http.StatusForbidden, "FORBIDDEN", "Only the creator or a moderator can edit this shortcut.")
	}
	var b patchShortcutBody
	if err := httpx.DecodeJSON(w, r, &b, 8<<10); err != nil {
		return err
	}
	title, err := cleanTitle(b.Title)
	if err != nil {
		return err
	}
	var flags, vehicles []string
	if b.Flags != nil {
		if flags, err = cleanList(b.Flags, allowedFlags, "flags"); err != nil {
			return err
		}
	}
	if b.Vehicles != nil {
		reg, _ := s.Regions.Get(sc.RegionID)
		if vehicles, err = cleanList(b.Vehicles, reg.Vehicles(), "vehicles"); err != nil {
			return err
		}
		if len(vehicles) == 0 {
			return badParam("vehicles", "at least one")
		}
	}
	if b.Surface != nil && !slices.Contains(allowedSurfaces, *b.Surface) {
		return badParam("surface", "must be paved, unpaved or track")
	}
	out, err := s.DB.UpdateShortcutMeta(r.Context(), sc.ID, title, flags, vehicles, b.Surface)
	if err != nil {
		return err
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"shortcut": out})
	return nil
}

type reportBody struct {
	Verdict string  `json:"verdict"`
	Reason  *string `json:"reason"`
	Note    *string `json:"note"`
}

func (s *Server) report(w http.ResponseWriter, r *http.Request) error {
	u, err := auth.Require(r)
	if err != nil {
		return err
	}
	var b reportBody
	if err := httpx.DecodeJSON(w, r, &b, 4<<10); err != nil {
		return err
	}
	if !slices.Contains(allowedVerdicts, b.Verdict) {
		return badParam("verdict", "must be passable, blocked or partly")
	}
	if b.Reason != nil && !slices.Contains(allowedReasons, *b.Reason) {
		return badParam("reason", "unknown reason")
	}
	if b.Verdict == "blocked" && b.Reason == nil {
		return badParam("reason", "required when blocked")
	}
	if b.Note != nil && len([]rune(*b.Note)) > 280 {
		return badParam("note", "at most 280 characters")
	}
	sc, err := s.DB.GetShortcut(r.Context(), r.PathValue("id"))
	if err != nil || sc.Status == shortcuts.StatusRemoved {
		return httpx.Err(http.StatusNotFound, "NOT_FOUND", "Shortcut not found.")
	}
	if n, err := s.DB.CountUserReportsToday(r.Context(), u.ID); err == nil && n >= 100 {
		return httpx.Err(http.StatusTooManyRequests, "DAILY_LIMIT", "You have reached today's report limit.")
	}
	if err := s.DB.AddReport(r.Context(), store.NewReport{ShortcutID: sc.ID, UserID: u.ID, Verdict: b.Verdict, Reason: b.Reason, Note: b.Note}); err != nil {
		if errors.Is(err, store.ErrConflict) {
			return httpx.Err(http.StatusConflict, "ALREADY_REPORTED", "You already reported this shortcut today. Thanks.")
		}
		return err
	}
	res, err := s.Jobs.RecomputeOne(r.Context(), sc.ID)
	if err != nil {
		return err
	}
	return writeJSONStatus(w, http.StatusCreated, map[string]any{
		"confidence": math.Round(res.Confidence*1000) / 1000, "band": shortcuts.Band(res.Confidence), "status": res.Status,
	})
}

type traceBody struct {
	Points []pointT `json:"points"`
}

func (s *Server) uploadTrace(w http.ResponseWriter, r *http.Request) error {
	u, err := auth.Require(r)
	if err != nil {
		return err
	}
	if u.TracesConsentAt == nil {
		return httpx.Err(http.StatusForbidden, "NO_CONSENT", "Turn on ride-trace sharing in Settings first.")
	}
	var b traceBody
	if err := httpx.DecodeJSON(w, r, &b, 2<<20); err != nil {
		return err
	}
	if len(b.Points) < 2 || len(b.Points) > 20000 {
		return badParam("points", "need between 2 and 20000 points")
	}
	pts := make([]shortcuts.TimedPoint, 0, len(b.Points))
	for _, p := range b.Points {
		pt := geo.Point{Lat: p.Lat, Lng: p.Lng}
		if !pt.Valid() || p.T == nil {
			return badParam("points", "each point needs lat, lng and t")
		}
		pts = append(pts, shortcuts.TimedPoint{Point: pt, T: *p.T})
	}
	// Privacy (SPEC §17): trim 200 m at each end, cut 300 m around Home/Work.
	pts = shortcuts.TrimEnds(pts, 200)
	places, err := s.DB.Places(r.Context(), u.ID)
	if err != nil {
		return err
	}
	var private []geo.Point
	for _, p := range places {
		private = append(private, geo.Point{Lat: p.Lat, Lng: p.Lng})
	}
	saved := 0
	for _, piece := range shortcuts.CutAround(pts, private, 300, 100) {
		line := make([]geo.Point, len(piece))
		ts := make([]time.Time, len(piece))
		for i, p := range piece {
			line[i], ts[i] = p.Point, p.T
		}
		regionID, ok, err := s.DB.RegionAt(r.Context(), line[0])
		if err != nil {
			return err
		}
		if !ok {
			continue
		}
		if _, err := s.DB.SaveTrace(r.Context(), u.ID, regionID, line, ts, "desire_mining"); err != nil {
			return err
		}
		saved++
	}
	return writeJSONStatus(w, http.StatusAccepted, map[string]any{"stored_pieces": saved, "retention_days": 90})
}

func (s *Server) listCandidates(w http.ResponseWriter, r *http.Request) error {
	u, err := auth.Require(r)
	if err != nil {
		return err
	}
	b, err := bboxParam(r, 0.5)
	if err != nil {
		return err
	}
	cs, err := s.DB.CandidatesInBBox(r.Context(), b, u.ID)
	if err != nil {
		return err
	}
	if cs == nil {
		cs = []store.Candidate{}
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"candidates": cs})
	return nil
}

func (s *Server) answerCandidate(w http.ResponseWriter, r *http.Request) error {
	u, err := auth.Require(r)
	if err != nil {
		return err
	}
	var b struct {
		Yes bool `json:"yes"`
	}
	if err := httpx.DecodeJSON(w, r, &b, 1<<10); err != nil {
		return err
	}
	c, err := s.DB.AnswerCandidate(r.Context(), r.PathValue("id"), u.ID, b.Yes)
	if errors.Is(err, store.ErrConflict) {
		return httpx.Err(http.StatusConflict, "ALREADY_ANSWERED", "You already answered this one. Thanks.")
	}
	if errors.Is(err, store.ErrNotFound) {
		return httpx.Err(http.StatusNotFound, "NOT_FOUND", "Candidate not found.")
	}
	if err != nil {
		return err
	}
	promoted := ""
	switch {
	case c.YesCount >= 2 && c.Status != "promoted":
		// Two "yes" answers promote it to a pending shortcut (SPEC §7.7).
		if id, err := s.promoteCandidate(r, c); err == nil {
			promoted = id
		} else {
			s.Log.Warn("candidate promotion failed", "candidate", c.ID, "err", err)
		}
	case c.NoCount >= 3 && c.NoCount > c.YesCount:
		_ = s.DB.SetCandidateStatus(r.Context(), c.ID, "rejected")
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"candidate": c, "promoted_shortcut_id": promoted})
	return nil
}

func (s *Server) promoteCandidate(r *http.Request, c *store.Candidate) (string, error) {
	line := append([]geo.Point(nil), c.Line...)
	start, err := s.snapPoint(r, line[0])
	if err != nil {
		return "", err
	}
	end, err := s.snapPoint(r, line[len(line)-1])
	if err != nil {
		return "", err
	}
	if !start.OK || !end.OK || start.NodeID == end.NodeID {
		return "", fmt.Errorf("endpoints do not snap")
	}
	line[0] = geo.Point{Lat: start.Lat, Lng: start.Lng}
	line[len(line)-1] = geo.Point{Lat: end.Lat, Lng: end.Lng}
	reg, _ := s.Regions.Get(c.RegionID)
	title := "Riders' path"
	sc, err := s.DB.CreateShortcut(r.Context(), store.NewShortcut{
		RegionID: c.RegionID, Title: &title, Line: line, LengthM: int(math.Round(geo.Length(line))),
		StartNode: start.NodeID, EndNode: end.NodeID, Surface: "unpaved", Vehicles: reg.Vehicles(), Flags: []string{},
		Status: shortcuts.StatusPending,
	})
	if err != nil {
		return "", err
	}
	return sc.ID, s.DB.SetCandidateStatus(r.Context(), c.ID, "promoted")
}
