// Package server is Pagdandi's HTTP API. It is stateless: everything it
// serves comes from GraphHopper and the files `datactl` builds.
package server

import (
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"math"
	"net/http"
	"os"
	"path/filepath"
	"runtime/debug"
	"sort"
	"strconv"
	"strings"
	"time"

	"pagdandi/api/internal/features"
	"pagdandi/api/internal/geo"
	"pagdandi/api/internal/gh"
	"pagdandi/api/internal/land"
	"pagdandi/api/internal/plan"
	"pagdandi/api/internal/region"
	"pagdandi/api/internal/search"
)

// Server wires handlers.
type Server struct {
	Region   *region.Region
	Land     *land.Land
	Planner  *plan.Planner
	Search   *search.Service
	GH       *gh.Client
	Log      *slog.Logger
	TilesDir string
	WebDir   string   // built web app to serve at / (optional)
	Origins  []string // CORS origins allowed (dev)
}

// Handler returns the root handler.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) { writeJSON(w, 200, map[string]string{"status": "ok"}) })
	mux.HandleFunc("GET /readyz", s.ready)
	mux.HandleFunc("GET /v1/meta", s.meta)
	mux.HandleFunc("GET /v1/search", s.search)
	mux.HandleFunc("GET /v1/reverse", s.reverse)
	mux.HandleFunc("POST /v1/go", s.goTrails)
	mux.HandleFunc("POST /v1/wander", s.wander)
	mux.HandleFunc("GET /v1/gems", s.gems)
	mux.HandleFunc("GET /v1/fresh", s.fresh)
	mux.HandleFunc("GET /v1/waters", s.waters)
	mux.HandleFunc("GET /tiles/{file}", s.tiles)
	if s.WebDir != "" {
		mux.Handle("GET /", s.spa())
	}
	return s.recoverer(s.logger(s.cors(gzipJSON(mux))))
}

func (s *Server) ready(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
	defer cancel()
	if err := s.GH.Health(ctx); err != nil {
		writeJSON(w, 503, map[string]string{"status": "routing unavailable", "error": err.Error()})
		return
	}
	writeJSON(w, 200, map[string]string{"status": "ready"})
}

func (s *Server) meta(w http.ResponseWriter, r *http.Request) {
	c := s.Region.CenterPoint()
	tiles := s.Region.ID + ".pmtiles"
	_, err := os.Stat(filepath.Join(s.TilesDir, tiles))
	w.Header().Set("Cache-Control", "public, max-age=300")
	writeJSON(w, 200, map[string]any{
		"region": map[string]any{
			"id": s.Region.ID, "name": s.Region.Name, "name_hi": s.Region.NameHi, "center": c,
			"radius_km": s.Region.RadiusKm, "bbox": s.Region.BBox(), "ring": s.Region.Ring(), "timezone": s.Region.Timezone,
		},
		"vehicles": plan.Vehicles,
		"flavours": plan.Flavours,
		"data":     map[string]any{"osm_date": s.Land.Meta.OSMDate, "built_at": s.Land.Meta.BuiltAt, "fresh_months": s.Land.Meta.FreshFor, "counts": s.Land.Meta.Counts},
		"tiles":    map[string]any{"url": "/tiles/" + tiles, "available": err == nil},
	})
}

func (s *Server) search(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	near, _ := pointParam(q.Get("lat"), q.Get("lng"))
	res, err := s.Search.Search(r.Context(), q.Get("q"), near)
	if err != nil {
		s.Log.Warn("search failed", "err", err)
		writeErr(w, 502, "search_unavailable", "Search is unavailable right now")
		return
	}
	if res == nil {
		res = []search.Place{}
	}
	writeJSON(w, 200, map[string]any{"places": res})
}

func (s *Server) reverse(w http.ResponseWriter, r *http.Request) {
	p, ok := pointParam(r.URL.Query().Get("lat"), r.URL.Query().Get("lng"))
	if !ok {
		writeErr(w, 400, "bad_request", "lat and lng are required")
		return
	}
	res, err := s.Search.Reverse(r.Context(), p)
	if err != nil {
		writeErr(w, 502, "search_unavailable", err.Error())
		return
	}
	writeJSON(w, 200, res)
}

func (s *Server) goTrails(w http.ResponseWriter, r *http.Request) {
	var req plan.GoRequest
	if !readJSON(w, r, &req) {
		return
	}
	res, err := s.Planner.Go(r.Context(), req)
	if err != nil {
		s.planErr(w, err)
		return
	}
	writeJSON(w, 200, res)
}

func (s *Server) wander(w http.ResponseWriter, r *http.Request) {
	var req plan.WanderRequest
	if !readJSON(w, r, &req) {
		return
	}
	res, err := s.Planner.Wander(r.Context(), req)
	if err != nil {
		s.planErr(w, err)
		return
	}
	writeJSON(w, 200, res)
}

func (s *Server) planErr(w http.ResponseWriter, err error) {
	var pe *plan.Error
	if errors.As(err, &pe) {
		status := map[string]int{"bad_request": 400, "outside": 422, "no_route": 422, "different_water": 422, "no_water": 422, "too_big": 422, "unavailable": 503}[pe.Code]
		if status == 0 {
			status = 422
		}
		writeErr(w, status, pe.Code, pe.Message)
		return
	}
	if errors.Is(err, context.Canceled) {
		return
	}
	s.Log.Error("plan failed", "err", err)
	writeErr(w, 500, "internal", "Something went wrong planning this. Try again.")
}

// gems lists destinations in a bbox (featured first), for map markers.
func (s *Server) gems(w http.ResponseWriter, r *http.Request) {
	bb, ok := bboxParam(r.URL.Query().Get("bbox"))
	if !ok {
		bb = s.Region.BBox()
	}
	limit := intParam(r.URL.Query().Get("limit"), 150, 1, 500)
	gs := s.Land.Gems.InBBox(bb)
	sort.Slice(gs, func(i, j int) bool {
		if gs[i].Featured != gs[j].Featured {
			return gs[i].Featured
		}
		return gs[i].Score > gs[j].Score
	})
	if len(gs) > limit {
		gs = gs[:limit]
	}
	if gs == nil {
		gs = []features.Gem{}
	}
	w.Header().Set("Cache-Control", "public, max-age=600")
	writeJSON(w, 200, map[string]any{"gems": gs})
}

// fresh returns new-on-the-map ways in a bbox as GeoJSON.
func (s *Server) fresh(w http.ResponseWriter, r *http.Request) {
	bb, ok := bboxParam(r.URL.Query().Get("bbox"))
	if !ok {
		writeErr(w, 400, "bad_request", "bbox=minLng,minLat,maxLng,maxLat is required")
		return
	}
	if (bb[2]-bb[0])*(bb[3]-bb[1]) > 1.2 {
		writeJSON(w, 200, map[string]any{"type": "FeatureCollection", "features": []any{}, "truncated": true})
		return
	}
	pad := 0.02
	fs := s.Land.Fresh.InBBox(geo.BBox{bb[0] - pad, bb[1] - pad, bb[2] + pad, bb[3] + pad})
	sort.Slice(fs, func(i, j int) bool { return fs[i].LenM > fs[j].LenM })
	if len(fs) > 2500 {
		fs = fs[:2500]
	}
	feats := make([]map[string]any, 0, len(fs))
	for _, f := range fs {
		feats = append(feats, map[string]any{
			"type": "Feature", "id": f.ID,
			"properties": map[string]any{"highway": f.Highway, "month": f.Month, "name": f.Name, "len_m": f.LenM},
			"geometry":   map[string]any{"type": "LineString", "coordinates": f.Line},
		})
	}
	w.Header().Set("Cache-Control", "public, max-age=600")
	writeJSON(w, 200, map[string]any{"type": "FeatureCollection", "features": feats})
}

func (s *Server) waters(w http.ResponseWriter, r *http.Request) {
	out := []features.Water{}
	for _, wt := range s.Land.Waters {
		if wt.AreaKm2 >= 0.3 {
			out = append(out, wt)
		}
	}
	w.Header().Set("Cache-Control", "public, max-age=600")
	writeJSON(w, 200, map[string]any{"waters": out})
}

// tiles serves the PMTiles archive with HTTP range support.
func (s *Server) tiles(w http.ResponseWriter, r *http.Request) {
	name := filepath.Base(r.PathValue("file"))
	if !strings.HasSuffix(name, ".pmtiles") {
		http.NotFound(w, r)
		return
	}
	f, err := os.Open(filepath.Join(s.TilesDir, name))
	if err != nil {
		http.NotFound(w, r)
		return
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Cache-Control", "public, max-age=86400")
	w.Header().Set("Content-Type", "application/vnd.pmtiles")
	http.ServeContent(w, r, name, st.ModTime(), f)
}

// spa serves the built web app, falling back to index.html for client routes.
func (s *Server) spa() http.Handler {
	fs := http.FileServer(http.Dir(s.WebDir))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p := filepath.Join(s.WebDir, filepath.FromSlash(filepath.Clean("/"+r.URL.Path)))
		if st, err := os.Stat(p); err != nil || st.IsDir() {
			if r.URL.Path != "/" {
				r.URL.Path = "/"
			}
			w.Header().Set("Cache-Control", "no-cache")
		} else if strings.HasPrefix(r.URL.Path, "/assets/") {
			w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
		}
		fs.ServeHTTP(w, r)
	})
}

// --- middleware ------------------------------------------------------------------------

type statusWriter struct {
	http.ResponseWriter
	status int
}

func (w *statusWriter) WriteHeader(c int) { w.status = c; w.ResponseWriter.WriteHeader(c) }

func (s *Server) logger(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		sw := &statusWriter{ResponseWriter: w, status: 200}
		next.ServeHTTP(sw, r)
		if strings.HasPrefix(r.URL.Path, "/v1/") || sw.status >= 500 {
			s.Log.Info("http", "method", r.Method, "path", r.URL.Path, "status", sw.status, "ms", time.Since(start).Milliseconds())
		}
	})
}

func (s *Server) recoverer(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if v := recover(); v != nil {
				s.Log.Error("panic", "err", v, "stack", string(debug.Stack()))
				writeErr(w, 500, "internal", "Something went wrong")
			}
		}()
		next.ServeHTTP(w, r)
	})
}

func (s *Server) cors(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if o := r.Header.Get("Origin"); o != "" {
			for _, a := range s.Origins {
				if a == o || a == "*" {
					w.Header().Set("Access-Control-Allow-Origin", o)
					w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Range")
					w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
					w.Header().Set("Access-Control-Expose-Headers", "Content-Length, Content-Range")
					w.Header().Add("Vary", "Origin")
				}
			}
			if r.Method == http.MethodOptions {
				w.WriteHeader(204)
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}

type gzipWriter struct {
	http.ResponseWriter
	zw *gzip.Writer
}

func (g *gzipWriter) Write(b []byte) (int, error) { return g.zw.Write(b) }

// gzipJSON compresses /v1 responses (trails carry a lot of geometry).
func gzipJSON(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.URL.Path, "/v1/") || !strings.Contains(r.Header.Get("Accept-Encoding"), "gzip") {
			next.ServeHTTP(w, r)
			return
		}
		w.Header().Set("Content-Encoding", "gzip")
		w.Header().Add("Vary", "Accept-Encoding")
		zw, _ := gzip.NewWriterLevel(w, gzip.BestSpeed)
		defer zw.Close()
		next.ServeHTTP(&gzipWriter{ResponseWriter: w, zw: zw}, r)
	})
}

// --- helpers ---------------------------------------------------------------------------

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Del("Content-Length")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeErr(w http.ResponseWriter, status int, code, msg string) {
	writeJSON(w, status, map[string]any{"error": map[string]string{"code": code, "message": msg}})
}

func readJSON(w http.ResponseWriter, r *http.Request, v any) bool {
	dec := json.NewDecoder(io.LimitReader(r.Body, 1<<20))
	if err := dec.Decode(v); err != nil {
		writeErr(w, 400, "bad_request", "invalid JSON: "+err.Error())
		return false
	}
	return true
}

func pointParam(lat, lng string) (geo.Point, bool) {
	a, err1 := strconv.ParseFloat(lat, 64)
	b, err2 := strconv.ParseFloat(lng, 64)
	p := geo.Point{Lat: a, Lng: b}
	return p, err1 == nil && err2 == nil && p.Valid()
}

func bboxParam(s string) (geo.BBox, bool) {
	parts := strings.Split(s, ",")
	if len(parts) != 4 {
		return geo.BBox{}, false
	}
	var b geo.BBox
	for i, p := range parts {
		v, err := strconv.ParseFloat(strings.TrimSpace(p), 64)
		if err != nil || math.IsNaN(v) {
			return geo.BBox{}, false
		}
		b[i] = v
	}
	return b, b.Valid()
}

func intParam(s string, def, lo, hi int) int {
	v, err := strconv.Atoi(s)
	if err != nil {
		return def
	}
	return max(lo, min(hi, v))
}
