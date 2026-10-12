package handlers

import (
	"context"
	"errors"
	"fmt"
	"html"
	"math"
	"net/http"
	"regexp"
	"strings"
	"time"

	h3 "github.com/uber/h3-go/v4"

	"pagdandi/api/internal/geo"
	"pagdandi/api/internal/httpx"
	"pagdandi/api/internal/regions"
	"pagdandi/api/internal/routing"
	"pagdandi/api/internal/store"
)

// regionOut merges the stored region (coverage bbox) with regions.yaml settings.
type regionOut struct {
	ID             string             `json:"id"`
	Name           string             `json:"name"`
	Country        string             `json:"country"`
	Timezone       string             `json:"timezone"`
	Center         [2]float64         `json:"center"`
	RadiusKm       float64            `json:"radius_km"`
	BBox           [4]float64         `json:"bbox"`
	Langs          []string           `json:"langs"`
	DefaultVehicle string             `json:"default_vehicle"`
	Vehicles       []string           `json:"vehicles"`
	Seasons        []regions.Season   `json:"seasons"`
	ActiveSeason   *string            `json:"active_season"`
	Corridors      []regions.Corridor `json:"corridors,omitempty"`
	Notes          string             `json:"notes,omitempty"`
	Seeded         bool               `json:"seeded"`
}

func (s *Server) listRegions(w http.ResponseWriter, r *http.Request) error {
	rows, err := s.DB.Regions(r.Context())
	if err != nil {
		return err
	}
	stored := map[string]store.RegionRow{}
	for _, x := range rows {
		stored[x.ID] = x
	}
	now := time.Now()
	out := make([]regionOut, 0, len(s.Regions.Regions))
	for _, reg := range s.Regions.Regions {
		o := regionOut{
			ID: reg.ID, Name: reg.Name, Country: reg.Country, Timezone: reg.Timezone, Center: reg.Center,
			RadiusKm: reg.RadiusKm, Langs: reg.Langs, DefaultVehicle: reg.DefaultVehicle, Vehicles: reg.Vehicles(),
			Seasons: reg.Seasons, Corridors: reg.Corridors, Notes: reg.Notes,
		}
		if o.Seasons == nil {
			o.Seasons = []regions.Season{}
		}
		if a := reg.ActiveSeason(now); a != nil {
			n := a.Name
			o.ActiveSeason = &n
		}
		if st, ok := stored[reg.ID]; ok {
			o.BBox, o.Seeded = st.BBox, true
		} else {
			c := reg.CenterPoint()
			n := geo.Destination(c, 0, reg.RadiusKm*1000)
			e := geo.Destination(c, 90, reg.RadiusKm*1000)
			o.BBox = [4]float64{2*c.Lng - e.Lng, 2*c.Lat - n.Lat, e.Lng, n.Lat}
		}
		out = append(out, o)
	}
	w.Header().Set("Cache-Control", "public, max-age=300")
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"regions": out})
	return nil
}

func (s *Server) listDestinations(w http.ResponseWriter, r *http.Request) error {
	id := r.PathValue("id")
	if _, ok := s.Regions.Get(id); !ok {
		return httpx.Err(http.StatusNotFound, "NOT_FOUND", "Unknown region.")
	}
	ds, err := s.DB.Destinations(r.Context(), id)
	if err != nil {
		return err
	}
	if ds == nil {
		ds = []store.Destination{}
	}
	w.Header().Set("Cache-Control", "public, max-age=300")
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"region": id, "destinations": ds})
	return nil
}

func (s *Server) geocode(w http.ResponseWriter, r *http.Request) error {
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	if len(q) > 120 {
		return badParam("q", "too long")
	}
	near, err := pointParam(r, false)
	if err != nil {
		return err
	}
	res, err := s.Geo.Search(r.Context(), q, near, langParam(r), 10)
	if err != nil {
		s.Log.Warn("geocode failed", "err", err)
		return httpx.Err(http.StatusBadGateway, "GEOCODER_UNAVAILABLE", "Place search is unavailable right now.")
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"results": res})
	return nil
}

func (s *Server) reverse(w http.ResponseWriter, r *http.Request) error {
	p, err := pointParam(r, true)
	if err != nil {
		return err
	}
	res, err := s.Geo.Reverse(r.Context(), *p, langParam(r))
	if err != nil {
		return httpx.Err(http.StatusBadGateway, "GEOCODER_UNAVAILABLE", "Place lookup is unavailable right now.")
	}
	httpx.WriteJSON(w, http.StatusOK, res)
	return nil
}

type routeBody struct {
	From     geo.Point  `json:"from"`
	To       geo.Point  `json:"to"`
	Vehicle  string     `json:"vehicle"`
	Mode     string     `json:"mode"`
	DepartAt *time.Time `json:"depart_at"`
	Lang     string     `json:"lang"`
}

func (s *Server) route(w http.ResponseWriter, r *http.Request) error {
	var body routeBody
	if err := httpx.DecodeJSON(w, r, &body, 8<<10); err != nil {
		return err
	}
	start := time.Now()
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()
	resp, err := s.Router.Route(ctx, routing.Request{From: body.From, To: body.To, Vehicle: body.Vehicle, Mode: body.Mode, DepartAt: body.DepartAt, Lang: body.Lang})
	if err != nil {
		return routeError(err)
	}
	cached := "false"
	if resp.Cached {
		cached = "true"
		s.Metrics.RouteCache.WithLabelValues("hit").Inc()
	} else {
		s.Metrics.RouteCache.WithLabelValues("miss").Inc()
	}
	s.Metrics.RouteLatency.WithLabelValues(strings.Join(resp.Context.Profiles, "+"), cached).Observe(time.Since(start).Seconds())
	go s.recordStat(resp, body.From, body.To)
	httpx.WriteJSON(w, http.StatusOK, resp)
	return nil
}

func routeError(err error) error {
	var oc *routing.OutOfCoverageError
	var ve *routing.ValidationError
	switch {
	case errors.As(err, &oc):
		e := httpx.Err(http.StatusUnprocessableEntity, "OUT_OF_COVERAGE",
			fmt.Sprintf("Pagdandi does not cover this area yet. The nearest region is %s, %.0f km away.", oc.NearestName, oc.NearestDistKm))
		e.Details = map[string]any{"which": oc.Which, "nearest_region": oc.NearestRegion, "nearest_region_name": oc.NearestName, "distance_km": oc.NearestDistKm}
		return e
	case errors.As(err, &ve):
		return httpx.Err(http.StatusBadRequest, "BAD_REQUEST", ve.Msg)
	case errors.Is(err, routing.ErrCrossRegion):
		return httpx.Err(http.StatusUnprocessableEntity, "CROSS_COUNTRY", "Start and destination are in different countries.")
	case errors.Is(err, routing.ErrNoRoute):
		return httpx.Err(http.StatusUnprocessableEntity, "NO_ROUTE", "No road route found between these points for this vehicle.")
	case errors.Is(err, context.DeadlineExceeded):
		return httpx.Err(http.StatusGatewayTimeout, "TIMEOUT", "Routing took too long. Please try again.")
	}
	var ge *routing.Error
	if errors.As(err, &ge) || strings.Contains(err.Error(), "connect") {
		return httpx.Err(http.StatusBadGateway, "ROUTING_UNAVAILABLE", "The routing engine is unavailable right now.")
	}
	return err
}

// recordStat stores anonymised analytics: only H3 r7 cells (~5 km²), never coordinates.
func (s *Server) recordStat(resp *routing.Response, from, to geo.Point) {
	if resp.Cached {
		return
	}
	cell := func(p geo.Point) string {
		c, err := h3.LatLngToCell(h3.NewLatLng(p.Lat, p.Lng), 7)
		if err != nil {
			return ""
		}
		return c.String()
	}
	var savedM, savedS int
	for _, o := range resp.Options {
		if o.ID == resp.Recommended && o.Savings != nil {
			savedM, savedS = o.Savings.DistanceM, o.Savings.DurationS
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_ = s.DB.RecordRouteStat(ctx, resp.Region, cell(from), cell(to), "recommended:"+resp.Recommended, savedM, savedS)
}

var safeID = regexp.MustCompile(`^[0-9A-Za-z]{10,40}$`)

func (s *Server) gpx(w http.ResponseWriter, r *http.Request) error {
	id := r.PathValue("id")
	if !safeID.MatchString(id) {
		return httpx.Err(http.StatusNotFound, "NOT_FOUND", "Route not found.")
	}
	resp, ok := s.Router.Stored(r.Context(), id)
	if !ok {
		return httpx.Err(http.StatusNotFound, "NOT_FOUND", "Route expired. Plan it again to export.")
	}
	optID := r.URL.Query().Get("option")
	if optID == "" {
		optID = resp.Recommended
	}
	var opt *routing.Option
	for i := range resp.Options {
		if resp.Options[i].ID == optID {
			opt = &resp.Options[i]
		}
	}
	if opt == nil {
		return httpx.Err(http.StatusNotFound, "NOT_FOUND", "No such route option.")
	}
	line, err := geo.Decode(opt.Polyline)
	if err != nil {
		return err
	}
	name := "Pagdandi " + map[string]string{"standard": "usual way", "shortcut": "shortcut"}[opt.Kind]
	var b strings.Builder
	b.WriteString(`<?xml version="1.0" encoding="UTF-8"?>` + "\n")
	b.WriteString(`<gpx version="1.1" creator="Pagdandi" xmlns="http://www.topografix.com/GPX/1/1">` + "\n")
	fmt.Fprintf(&b, "<metadata><name>%s</name><copyright author=\"OpenStreetMap contributors\"><license>https://opendatacommons.org/licenses/odbl/</license></copyright></metadata>\n", html.EscapeString(name))
	fmt.Fprintf(&b, "<trk><name>%s</name><desc>%.1f km, %d min</desc><trkseg>\n", html.EscapeString(name), float64(opt.DistanceM)/1000, int(math.Round(float64(opt.DurationS)/60)))
	for _, p := range line {
		fmt.Fprintf(&b, `<trkpt lat="%.6f" lon="%.6f"/>`+"\n", p.Lat, p.Lng)
	}
	b.WriteString("</trkseg></trk></gpx>\n")
	w.Header().Set("Content-Type", "application/gpx+xml")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="pagdandi-%s-%s.gpx"`, resp.Region, opt.ID))
	_, err = w.Write([]byte(b.String()))
	return err
}

// writeJSONStatus is a tiny helper for 201/202 replies.
func writeJSONStatus(w http.ResponseWriter, status int, v any) error {
	httpx.WriteJSON(w, status, v)
	return nil
}
