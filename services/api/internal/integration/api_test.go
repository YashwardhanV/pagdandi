package integration

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"pagdandi/api/internal/auth"
	"pagdandi/api/internal/cache"
	"pagdandi/api/internal/config"
	"pagdandi/api/internal/geo"
	"pagdandi/api/internal/geocode"
	"pagdandi/api/internal/handlers"
	"pagdandi/api/internal/jobs"
	"pagdandi/api/internal/regions"
	"pagdandi/api/internal/routing"
	"pagdandi/api/internal/store"
)

func loadRegions(t *testing.T) *regions.Set {
	t.Helper()
	_, here, _, _ := runtime.Caller(0)
	set, err := regions.Load(filepath.Join(filepath.Dir(here), "..", "..", "..", "..", "data", "regions.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	return set
}

type api struct {
	srv   *handlers.Server
	h     http.Handler
	db    *store.DB
	mu    sync.Mutex
	links map[string]string // email → last magic link
}

func (a *api) do(t *testing.T, method, path string, body any, cookie *http.Cookie) *httptest.ResponseRecorder {
	t.Helper()
	var rd io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		rd = bytes.NewReader(b)
	}
	req := httptest.NewRequest(method, path, rd)
	req.Header.Set("Content-Type", "application/json")
	req.RemoteAddr = "203.0.113.7:5000"
	if cookie != nil {
		req.AddCookie(cookie)
	}
	rec := httptest.NewRecorder()
	a.h.ServeHTTP(rec, req)
	return rec
}

func decode(t *testing.T, rec *httptest.ResponseRecorder, v any) {
	t.Helper()
	if err := json.Unmarshal(rec.Body.Bytes(), v); err != nil {
		t.Fatalf("decode %q: %v", rec.Body.String(), err)
	}
}

// signIn runs the magic-link flow and returns the session cookie.
func (a *api) signIn(t *testing.T, email string) *http.Cookie {
	t.Helper()
	rec := a.do(t, http.MethodPost, "/v1/auth/magic-link", map[string]string{"email": email}, nil)
	if rec.Code != http.StatusAccepted {
		t.Fatalf("magic link: %d %s", rec.Code, rec.Body)
	}
	a.mu.Lock()
	link := a.links[email]
	a.mu.Unlock()
	u, err := url.Parse(link)
	if err != nil || link == "" {
		t.Fatalf("no link captured for %s", email)
	}
	rec = a.do(t, http.MethodGet, u.RequestURI(), nil, nil)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("callback: %d %s", rec.Code, rec.Body)
	}
	for _, c := range rec.Result().Cookies() {
		if c.Name == auth.CookieName && c.Value != "" {
			return c
		}
	}
	t.Fatal("no session cookie set")
	return nil
}

func newAPI(t *testing.T) *api {
	t.Helper()
	db := testDB(t)
	ctx := context.Background()
	for _, q := range []string{
		`truncate users, sessions, magic_links, shortcuts, shortcut_reports, traces, candidate_paths, candidate_answers,
		   user_places, offnet_segments, moderation_actions, route_stats cascade`,
	} {
		if _, err := db.Pool.Exec(ctx, q); err != nil {
			t.Fatal(err)
		}
	}
	set := loadRegions(t)
	ind, _ := set.Get("indore")
	ring, _ := json.Marshal(map[string]any{"type": "Polygon", "coordinates": [][][2]float64{geo.Circle(ind.CenterPoint(), ind.RadiusKm*1000, 64)}})
	if err := db.UpsertRegion(ctx, store.RegionRow{ID: ind.ID, Name: ind.Name, Country: ind.Country, Timezone: ind.Timezone,
		Center: ind.Center, RadiusKm: int(ind.RadiusKm), Config: json.RawMessage(`{}`)}, string(ring)); err != nil {
		t.Fatal(err)
	}
	cfg := &config.Config{PublicBaseURL: "http://api.test", WebBaseURL: "http://web.test", SessionSecret: "test-secret-test-secret-test-secret!!",
		CORSOrigins: []string{"http://web.test"}, Env: "dev", SMTPHost: "capture"}
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	c := cache.Cache(cache.NewMemory())
	if u := os.Getenv("TEST_REDIS_URL"); u != "" {
		if rc, err := cache.NewRedis(u); err == nil && rc.Ping(ctx) == nil {
			_ = rc.C.FlushDB(ctx).Err()
			c = rc
		}
	}
	ghURL := os.Getenv("PAGDANDI_FIXTURE_GH_URL")
	if ghURL == "" {
		ghURL = "http://127.0.0.1:1" // unreachable: route tests that need GH skip themselves
	}
	graphs := map[string]*routing.GH{"IN": routing.NewGH(ghURL), "SE": routing.NewGH("http://127.0.0.1:1")}
	a := &api{db: db, links: map[string]string{}}
	authSvc := auth.New(cfg, db, log)
	linkRe := regexp.MustCompile(`http://api\.test/v1/auth/callback\?\S+`)
	authSvc.Mail = func(to, subject, body string) error {
		a.mu.Lock()
		a.links[to] = linkRe.FindString(body)
		a.mu.Unlock()
		return nil
	}
	a.srv = &handlers.Server{
		Cfg: cfg, DB: db, Cache: c, Regions: set,
		Router: &routing.Router{Regions: set, Lookup: db, Graphs: graphs, Store: db, Cache: c, Log: log,
			Seeded: func(id string) bool { return id == "indore" }},
		Geo:  &geocode.Service{DB: db, Lookup: db, Cache: c},
		Auth: authSvc, Jobs: &jobs.Jobs{DB: db, Regions: set, Log: log}, Graphs: graphs, Log: log,
		Metrics: handlers.NewMetrics(),
	}
	a.h = a.srv.Handler()
	return a
}

func TestAPIPublicEndpoints(t *testing.T) {
	a := newAPI(t)
	var regs struct {
		Regions []struct {
			ID     string `json:"id"`
			Seeded bool   `json:"seeded"`
		} `json:"regions"`
	}
	rec := a.do(t, http.MethodGet, "/v1/regions", nil, nil)
	decode(t, rec, &regs)
	if rec.Code != 200 || len(regs.Regions) != 18 {
		t.Fatalf("regions: %d, %d entries", rec.Code, len(regs.Regions))
	}
	seeded := 0
	for _, r := range regs.Regions {
		if r.Seeded {
			seeded++
		}
	}
	if seeded != 1 {
		t.Fatalf("only indore is seeded in the test DB, got %d", seeded)
	}

	// Out of coverage: Mumbai is not seeded here.
	rec = a.do(t, http.MethodPost, "/v1/route", map[string]any{"from": fxA, "to": map[string]float64{"lat": 19.076, "lng": 72.8777}}, nil)
	var e struct {
		Error struct {
			Code    string         `json:"code"`
			Details map[string]any `json:"details"`
		} `json:"error"`
	}
	decode(t, rec, &e)
	if rec.Code != http.StatusUnprocessableEntity || e.Error.Code != "OUT_OF_COVERAGE" || e.Error.Details["nearest_region"] != "indore" {
		t.Fatalf("out of coverage: %d %s", rec.Code, rec.Body)
	}
	// Validation.
	rec = a.do(t, http.MethodPost, "/v1/route", map[string]any{"from": map[string]any{"lat": 999, "lng": 0}, "to": fxC}, nil)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("bad coordinate: %d", rec.Code)
	}
	rec = a.do(t, http.MethodPost, "/v1/route", map[string]any{"from": fxA, "to": fxC, "surprise": true}, nil)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("unknown field: %d", rec.Code)
	}
	if rec := a.do(t, http.MethodGet, "/v1/regions/nowhere/destinations", nil, nil); rec.Code != 404 {
		t.Fatalf("unknown region destinations: %d", rec.Code)
	}
	if rec := a.do(t, http.MethodGet, "/v1/shortcuts?bbox=1,2,3", nil, nil); rec.Code != 400 {
		t.Fatalf("bad bbox: %d", rec.Code)
	}
	if rec := a.do(t, http.MethodGet, "/v1/nope", nil, nil); rec.Code != 404 {
		t.Fatalf("unknown path: %d", rec.Code)
	}
	if rec := a.do(t, http.MethodGet, "/metrics", nil, nil); rec.Code != 200 || !strings.Contains(rec.Body.String(), "pagdandi_http_requests_total") {
		t.Fatalf("metrics: %d", rec.Code)
	}
}

func TestAPIRouteOnFixture(t *testing.T) {
	fixtureGH(t)
	a := newAPI(t)
	rec := a.do(t, http.MethodPost, "/v1/route", map[string]any{"from": fxA, "to": fxC, "mode": "auto", "depart_at": "2026-10-12T06:30:00Z"}, nil)
	if rec.Code != 200 {
		t.Fatalf("route: %d %s", rec.Code, rec.Body)
	}
	var resp routing.Response
	decode(t, rec, &resp)
	if resp.Region != "indore" || len(resp.Options) < 2 || resp.Attribution == "" {
		t.Fatalf("route response: %+v", resp)
	}
	// GPX export by request id.
	rec = a.do(t, http.MethodGet, "/v1/route/"+resp.RequestID+"/gpx?option=std", nil, nil)
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), "<trkpt") || rec.Header().Get("Content-Type") != "application/gpx+xml" {
		t.Fatalf("gpx: %d %s", rec.Code, rec.Body)
	}
}

func TestAPICommunityFlow(t *testing.T) {
	a := newAPI(t)
	ctx := context.Background()
	var n int
	if err := a.db.Pool.QueryRow(ctx, `select count(*) from osm_nodes where country = 'ZZ'`).Scan(&n); err != nil || n == 0 {
		t.Skip("fixture nodes not loaded (run `datactl fixture` against the test DB)")
	}
	creator := a.signIn(t, "creator@example.com")
	rider1 := a.signIn(t, "rider1@example.com")
	rider2 := a.signIn(t, "rider2@example.com")

	// Not signed in.
	if rec := a.do(t, http.MethodPost, "/v1/shortcuts", map[string]any{}, nil); rec.Code != 401 {
		t.Fatalf("anonymous create: %d", rec.Code)
	}
	// Endpoint too far from any road.
	bad := map[string]any{"source": "drawn", "surface": "unpaved", "points": []map[string]float64{
		{"lat": 23.0015, "lng": 77.0110}, {"lat": 23.0100, "lng": 77.00605}}}
	rec := a.do(t, http.MethodPost, "/v1/shortcuts", bad, creator)
	if rec.Code != http.StatusUnprocessableEntity || !strings.Contains(rec.Body.String(), "SNAP_FAILED") {
		t.Fatalf("snap failure: %d %s", rec.Code, rec.Body)
	}
	// Good shortcut: A to the ring's node 14, endpoints a few metres off.
	good := map[string]any{"source": "drawn", "surface": "unpaved", "title": "Farm cut", "flags": []string{"gate", "floods_in_rain"},
		"points": []map[string]float64{{"lat": 23.00008, "lng": 77.00005}, {"lat": 23.0050, "lng": 77.0045}, {"lat": 23.01005, "lng": 77.00602}}}
	rec = a.do(t, http.MethodPost, "/v1/shortcuts", good, creator)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create: %d %s", rec.Code, rec.Body)
	}
	var created struct {
		Shortcut store.Shortcut `json:"shortcut"`
	}
	decode(t, rec, &created)
	sc := created.Shortcut
	if sc.Status != "pending" || sc.StartNode != 1 || sc.EndNode != 14 || sc.RegionID != "indore" {
		t.Fatalf("created shortcut: %+v", sc)
	}

	// Reports: the creator's own does not activate; two riders do.
	report := func(c *http.Cookie, verdict string) *httptest.ResponseRecorder {
		body := map[string]any{"verdict": verdict}
		if verdict == "blocked" {
			body["reason"] = "flooded"
		}
		return a.do(t, http.MethodPost, "/v1/shortcuts/"+sc.ID+"/reports", body, c)
	}
	if rec := report(creator, "passable"); rec.Code != 201 {
		t.Fatalf("creator report: %d %s", rec.Code, rec.Body)
	}
	if rec := report(creator, "passable"); rec.Code != http.StatusConflict {
		t.Fatalf("second report same day: %d", rec.Code)
	}
	if rec := report(rider1, "passable"); rec.Code != 201 {
		t.Fatalf("rider1: %d", rec.Code)
	}
	rec = report(rider2, "passable")
	var rr struct {
		Status     string  `json:"status"`
		Confidence float64 `json:"confidence"`
		Band       string  `json:"band"`
	}
	decode(t, rec, &rr)
	if rr.Status != "active" || rr.Confidence < 0.79 || rr.Band != "high" {
		t.Fatalf("after two independent confirmations: %+v", rr)
	}
	if rec := a.do(t, http.MethodPost, "/v1/shortcuts/"+sc.ID+"/reports", map[string]any{"verdict": "blocked"}, rider1); rec.Code != 400 {
		t.Fatalf("blocked without reason must be rejected: %d", rec.Code)
	}

	// Listing and detail.
	rec = a.do(t, http.MethodGet, "/v1/shortcuts?bbox=76.99,22.99,77.02,23.02&zoom=15", nil, nil)
	var list struct {
		Shortcuts []store.Shortcut `json:"shortcuts"`
	}
	decode(t, rec, &list)
	if len(list.Shortcuts) != 1 || list.Shortcuts[0].ID != sc.ID {
		t.Fatalf("bbox listing: %+v", list)
	}
	rec = a.do(t, http.MethodGet, "/v1/shortcuts/"+sc.ID, nil, nil)
	var detail struct {
		History          []map[string]any `json:"history"`
		Confirmations30d int              `json:"confirmations_30d"`
		Reports          []map[string]any `json:"reports"`
	}
	decode(t, rec, &detail)
	if rec.Code != 200 || detail.Confirmations30d != 3 || len(detail.History) == 0 || strings.Contains(rec.Body.String(), "rider1@") {
		t.Fatalf("detail: %d %s", rec.Code, rec.Body)
	}

	// Editing: rider is not the owner; creator is.
	if rec := a.do(t, http.MethodPatch, "/v1/shortcuts/"+sc.ID, map[string]any{"title": "Mine now"}, rider1); rec.Code != 403 {
		t.Fatalf("non-owner edit: %d", rec.Code)
	}
	if rec := a.do(t, http.MethodPatch, "/v1/shortcuts/"+sc.ID, map[string]any{"flags": []string{"steep"}}, creator); rec.Code != 200 {
		t.Fatalf("owner edit: %d %s", rec.Code, rec.Body)
	}

	// The overlay now finds this active shortcut as a routing candidate.
	cands, err := a.db.Candidates(ctx, routing.CandidateQuery{Vehicle: "scooter", From: fxA, To: geo.Point{Lat: 23.0105, Lng: 77.0062}, MajorAxisM: 3000, MinConfidence: 0.5})
	if err != nil || len(cands) != 1 || cands[0].Confirmations30d != 3 {
		t.Fatalf("candidates: %v %+v", err, cands)
	}

	// Moderation requires a moderator.
	if rec := a.do(t, http.MethodGet, "/v1/admin/queue", nil, rider1); rec.Code != 403 {
		t.Fatalf("rider in admin: %d", rec.Code)
	}
	if err := a.db.SetRole(ctx, "rider2@example.com", "moderator"); err != nil {
		t.Fatal(err)
	}
	if rec := a.do(t, http.MethodGet, "/v1/admin/queue", nil, rider2); rec.Code != 200 {
		t.Fatalf("moderator queue: %d", rec.Code)
	}
	if rec := a.do(t, http.MethodPost, "/v1/admin/shortcuts/"+sc.ID+"/action", map[string]any{"action": "remove", "note": "private road"}, rider2); rec.Code != 200 {
		t.Fatalf("remove: %d %s", rec.Code, rec.Body)
	}
	if rec := a.do(t, http.MethodGet, "/v1/shortcuts/"+sc.ID, nil, nil); rec.Code != 404 {
		t.Fatalf("removed shortcut must 404: %d", rec.Code)
	}
}

func TestAPIProfileTracesAndDeletion(t *testing.T) {
	a := newAPI(t)
	u := a.signIn(t, "me@example.com")
	rec := a.do(t, http.MethodGet, "/v1/me", nil, u)
	if rec.Code != 200 {
		t.Fatalf("me: %d", rec.Code)
	}
	// Traces need consent.
	t0 := time.Date(2026, 10, 10, 8, 0, 0, 0, time.UTC)
	var pts []map[string]any
	for i := 0; i < 120; i++ {
		pts = append(pts, map[string]any{"lat": 22.70, "lng": 75.80 + float64(i)*0.0001, "t": t0.Add(time.Duration(i) * 3 * time.Second)})
	}
	if rec := a.do(t, http.MethodPost, "/v1/traces", map[string]any{"points": pts}, u); rec.Code != 403 {
		t.Fatalf("trace without consent: %d", rec.Code)
	}
	if rec := a.do(t, http.MethodPatch, "/v1/me", map[string]any{"traces_consent": true, "display_name": "Asha", "home_region": "indore"}, u); rec.Code != 200 {
		t.Fatalf("consent: %d %s", rec.Code, rec.Body)
	}
	if rec := a.do(t, http.MethodPut, "/v1/me/places/home", map[string]float64{"lat": 22.70, "lng": 75.806}, u); rec.Code != 204 {
		t.Fatalf("home: %d %s", rec.Code, rec.Body)
	}
	rec = a.do(t, http.MethodPost, "/v1/traces", map[string]any{"points": pts}, u)
	var tr struct {
		StoredPieces int `json:"stored_pieces"`
	}
	decode(t, rec, &tr)
	if rec.Code != 202 || tr.StoredPieces != 1 {
		// ~1.2 km trace: 200 m trimmed each end, 300 m cut around home in the middle → only the far piece is long enough.
		t.Fatalf("trace upload: %d %s", rec.Code, rec.Body)
	}
	var stored int
	_ = a.db.Pool.QueryRow(context.Background(), `select count(*) from traces where ST_DWithin(geom::geography, ST_SetSRID(ST_MakePoint(75.806, 22.70), 4326)::geography, 299)`).Scan(&stored)
	if stored != 0 {
		t.Fatal("no stored trace may come within 300 m of home")
	}
	// Account deletion removes the user and their personal data.
	if rec := a.do(t, http.MethodDelete, "/v1/me", nil, u); rec.Code != 204 {
		t.Fatalf("delete: %d", rec.Code)
	}
	if rec := a.do(t, http.MethodGet, "/v1/me", nil, u); rec.Code != 401 {
		t.Fatalf("after delete: %d", rec.Code)
	}
	var left int
	_ = a.db.Pool.QueryRow(context.Background(), `select (select count(*) from traces) + (select count(*) from user_places) + (select count(*) from sessions)`).Scan(&left)
	if left != 0 {
		t.Fatalf("personal rows left after deletion: %d", left)
	}
}

func TestAPIAuthRateLimit(t *testing.T) {
	a := newAPI(t)
	var last int
	for i := 0; i < 12; i++ {
		last = a.do(t, http.MethodPost, "/v1/auth/magic-link", map[string]string{"email": "x@example.com"}, nil).Code
	}
	if last != http.StatusTooManyRequests {
		t.Fatalf("expected 429 after many sign-in requests, got %d", last)
	}
	// Expired or reused links redirect to the sign-in page with an error.
	rec := a.do(t, http.MethodGet, "/v1/auth/callback?token=nope", nil, nil)
	if rec.Code != http.StatusSeeOther || !strings.Contains(rec.Header().Get("Location"), "error=expired") {
		t.Fatalf("bad token: %d %s", rec.Code, rec.Header().Get("Location"))
	}
}
