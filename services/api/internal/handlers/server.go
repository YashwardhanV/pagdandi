// Package handlers implements the /v1 HTTP API (SPEC §8.2).
package handlers

import (
	"context"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"

	"pagdandi/api/internal/auth"
	"pagdandi/api/internal/cache"
	"pagdandi/api/internal/config"
	"pagdandi/api/internal/geocode"
	"pagdandi/api/internal/httpx"
	"pagdandi/api/internal/jobs"
	"pagdandi/api/internal/regions"
	"pagdandi/api/internal/routing"
	"pagdandi/api/internal/store"
)

// Server holds handler dependencies.
type Server struct {
	Cfg     *config.Config
	DB      *store.DB
	Cache   cache.Cache
	Regions *regions.Set
	Router  *routing.Router
	Geo     *geocode.Service
	Auth    *auth.Service
	Jobs    *jobs.Jobs
	Graphs  map[string]*routing.GH
	Log     *slog.Logger
	Metrics *Metrics
}

// Metrics are the Prometheus collectors (SPEC §8.4).
type Metrics struct {
	Reg          *prometheus.Registry
	HTTPRequests *prometheus.CounterVec
	HTTPLatency  *prometheus.HistogramVec
	RouteLatency *prometheus.HistogramVec
	GHLatency    *prometheus.HistogramVec
	RouteCache   *prometheus.CounterVec
	Shortcuts    *prometheus.GaugeVec
}

// NewMetrics registers collectors.
func NewMetrics() *Metrics {
	m := &Metrics{Reg: prometheus.NewRegistry()}
	buckets := []float64{.01, .025, .05, .1, .2, .35, .5, .7, 1, 1.5, 2.5, 5}
	m.HTTPRequests = prometheus.NewCounterVec(prometheus.CounterOpts{Name: "pagdandi_http_requests_total", Help: "HTTP requests."}, []string{"route", "status"})
	m.HTTPLatency = prometheus.NewHistogramVec(prometheus.HistogramOpts{Name: "pagdandi_http_request_duration_seconds", Help: "HTTP latency.", Buckets: buckets}, []string{"route"})
	m.RouteLatency = prometheus.NewHistogramVec(prometheus.HistogramOpts{Name: "pagdandi_route_duration_seconds", Help: "POST /v1/route latency by profile set.", Buckets: buckets}, []string{"profiles", "cached"})
	m.GHLatency = prometheus.NewHistogramVec(prometheus.HistogramOpts{Name: "pagdandi_graphhopper_duration_seconds", Help: "GraphHopper call latency.", Buckets: buckets}, []string{"endpoint", "profile", "ok"})
	m.RouteCache = prometheus.NewCounterVec(prometheus.CounterOpts{Name: "pagdandi_route_cache_total", Help: "Route cache lookups."}, []string{"result"})
	m.Shortcuts = prometheus.NewGaugeVec(prometheus.GaugeOpts{Name: "pagdandi_shortcuts", Help: "Shortcuts by status."}, []string{"status"})
	m.Reg.MustRegister(m.HTTPRequests, m.HTTPLatency, m.RouteLatency, m.GHLatency, m.RouteCache, m.Shortcuts,
		prometheus.NewGoCollector(), prometheus.NewProcessCollector(prometheus.ProcessCollectorOpts{}))
	return m
}

// ObserveGH is a routing.GH Observe hook.
func (m *Metrics) ObserveGH(endpoint, profile string, d time.Duration, err error) {
	m.GHLatency.WithLabelValues(endpoint, profile, strconv.FormatBool(err == nil)).Observe(d.Seconds())
}

// RefreshShortcutGauge updates the shortcut counts.
func (s *Server) RefreshShortcutGauge(ctx context.Context) {
	counts, err := s.DB.ShortcutCountsByStatus(ctx)
	if err != nil {
		return
	}
	for _, st := range []string{"pending", "active", "stale", "blocked", "removed"} {
		s.Metrics.Shortcuts.WithLabelValues(st).Set(float64(counts[st]))
	}
}

// Handler builds the full HTTP handler.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	h := httpx.H

	// Ops.
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		httpx.WriteJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})
	mux.HandleFunc("GET /readyz", h(s.ready))
	mux.Handle("GET /metrics", promhttp.HandlerFor(s.Metrics.Reg, promhttp.HandlerOpts{}))

	// Public.
	mux.HandleFunc("GET /v1/regions", h(s.listRegions))
	mux.HandleFunc("GET /v1/regions/{id}/destinations", h(s.listDestinations))
	mux.HandleFunc("GET /v1/geocode", h(s.geocode))
	mux.HandleFunc("GET /v1/reverse", h(s.reverse))
	routeLimit := httpx.RateLimit(s.Cache, 60, time.Minute, func(r *http.Request) (string, int) {
		if u := auth.Current(r); u != nil {
			return "route:u:" + u.ID, 120
		}
		return "route:ip:" + httpx.ClientIP(r), 60
	})
	mux.Handle("POST /v1/route", routeLimit(h(s.route)))
	mux.HandleFunc("GET /v1/route/{id}/gpx", h(s.gpx))
	mux.HandleFunc("GET /v1/shortcuts", h(s.listShortcuts))
	mux.HandleFunc("GET /v1/shortcuts/{id}", h(s.getShortcut))

	// Signed in.
	mux.HandleFunc("POST /v1/shortcuts", h(s.createShortcut))
	mux.HandleFunc("PATCH /v1/shortcuts/{id}", h(s.patchShortcut))
	mux.HandleFunc("POST /v1/shortcuts/{id}/reports", h(s.report))
	mux.HandleFunc("POST /v1/shortcuts/snap", h(s.snap))
	mux.HandleFunc("POST /v1/traces", h(s.uploadTrace))
	mux.HandleFunc("GET /v1/candidates", h(s.listCandidates))
	mux.HandleFunc("POST /v1/candidates/{id}/answer", h(s.answerCandidate))
	mux.HandleFunc("GET /v1/me", h(s.me))
	mux.HandleFunc("PATCH /v1/me", h(s.patchMe))
	mux.HandleFunc("DELETE /v1/me", h(s.deleteMe))
	mux.HandleFunc("GET /v1/me/contributions", h(s.contributions))
	mux.HandleFunc("GET /v1/me/places", h(s.places))
	mux.HandleFunc("PUT /v1/me/places/{kind}", h(s.putPlace))
	mux.HandleFunc("DELETE /v1/me/places/{kind}", h(s.deletePlace))

	// Auth.
	authLimit := httpx.RateLimit(s.Cache, 10, time.Minute, func(r *http.Request) (string, int) { return "auth:ip:" + httpx.ClientIP(r), 0 })
	mux.Handle("POST /v1/auth/magic-link", authLimit(h(s.Auth.MagicLink)))
	mux.HandleFunc("GET /v1/auth/callback", h(s.Auth.Callback))
	mux.HandleFunc("POST /v1/auth/logout", h(s.Auth.Logout))
	mux.HandleFunc("GET /v1/auth/google/start", h(s.Auth.GoogleStart))
	mux.HandleFunc("GET /v1/auth/google/callback", h(s.Auth.GoogleCallback))
	mux.HandleFunc("GET /v1/auth/providers", func(w http.ResponseWriter, r *http.Request) {
		httpx.WriteJSON(w, http.StatusOK, map[string]bool{"email": true, "google": s.Cfg.GoogleEnabled()})
	})

	// Moderation.
	mux.HandleFunc("GET /v1/admin/queue", h(s.adminQueue))
	mux.HandleFunc("POST /v1/admin/shortcuts/{id}/action", h(s.adminAction))

	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		httpx.WriteError(w, r, httpx.Err(http.StatusNotFound, "NOT_FOUND", "No such endpoint."))
	})

	obs := func(route string, status int, d time.Duration) {
		s.Metrics.HTTPRequests.WithLabelValues(route, strconv.Itoa(status)).Inc()
		s.Metrics.HTTPLatency.WithLabelValues(route).Observe(d.Seconds())
	}
	return httpx.Chain(mux,
		httpx.RequestIDs(),
		httpx.Logging(s.Log, obs),
		httpx.Recover(s.Log),
		httpx.SecurityHeaders(),
		httpx.CORS(s.Cfg.CORSOrigins),
		s.Auth.Middleware(),
	)
}

func (s *Server) ready(w http.ResponseWriter, r *http.Request) error {
	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
	defer cancel()
	checks := map[string]string{}
	ok := true
	set := func(name string, err error) {
		if err != nil {
			checks[name] = err.Error()
			ok = false
		} else {
			checks[name] = "ok"
		}
	}
	set("postgres", s.DB.Ping(ctx))
	set("redis", s.Cache.Ping(ctx))
	for _, c := range s.Regions.Countries() {
		if gh := s.Graphs[c]; gh != nil {
			set("graphhopper_"+strings.ToLower(c), gh.Health(ctx))
		}
	}
	status := http.StatusOK
	if !ok {
		status = http.StatusServiceUnavailable
	}
	httpx.WriteJSON(w, status, map[string]any{"ready": ok, "checks": checks})
	return nil
}
