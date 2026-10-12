package routing

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"pagdandi/api/internal/geo"
)

// GH is a minimal GraphHopper HTTP client (POST /route, /match, /health).
//
// BaseURL may list several instances separated by commas. Calls go to the
// last instance that answered; on a connection error the next one is tried.
// This lets a refreshed graph start on a standby port before the old instance
// stops, so graph swaps need no downtime (SPEC §5 blue/green).
type GH struct {
	BaseURL string
	HTTP    *http.Client

	mu      sync.Mutex
	current int
	// Observe, if set, is called with the endpoint, profile and latency of every call.
	Observe func(endpoint, profile string, d time.Duration, err error)
}

// NewGH returns a client with sensible timeouts.
func NewGH(baseURL string) *GH {
	return &GH{BaseURL: baseURL, HTTP: &http.Client{Timeout: 8 * time.Second}}
}

// Statement is one custom-model rule.
type Statement struct {
	If         string  `json:"if,omitempty"`
	ElseIf     string  `json:"else_if,omitempty"`
	Else       *string `json:"else,omitempty"`
	MultiplyBy string  `json:"multiply_by,omitempty"`
	LimitTo    string  `json:"limit_to,omitempty"`
}

// CustomModel is the per-request model merged onto the server profile.
type CustomModel struct {
	Priority []Statement `json:"priority,omitempty"`
	Speed    []Statement `json:"speed,omitempty"`
}

// RouteRequest is the subset of GraphHopper's POST /route body we use.
type RouteRequest struct {
	Points       [][2]float64 `json:"points"` // [lng, lat]
	Profile      string       `json:"profile"`
	Locale       string       `json:"locale,omitempty"`
	Instructions bool         `json:"instructions"`
	CalcPoints   bool         `json:"calc_points"`
	PointsEnc    bool         `json:"points_encoded"`
	Details      []string     `json:"details,omitempty"`
	Algorithm    string       `json:"algorithm,omitempty"`
	MaxPaths     int          `json:"alternative_route.max_paths,omitempty"`
	MaxWeightF   float64      `json:"alternative_route.max_weight_factor,omitempty"`
	MaxShareF    float64      `json:"alternative_route.max_share_factor,omitempty"`
	CustomModel  *CustomModel `json:"custom_model,omitempty"`
	SnapPrevent  []string     `json:"snap_preventions,omitempty"`
}

// Instruction is one GraphHopper maneuver.
type Instruction struct {
	Text       string  `json:"text"`
	StreetName string  `json:"street_name"`
	Distance   float64 `json:"distance"`
	Time       int64   `json:"time"` // ms
	Sign       int     `json:"sign"`
	Interval   [2]int  `json:"interval"`
	ExitNumber int     `json:"exit_number,omitempty"`
}

// Detail is one path-detail span [from, to, value].
type Detail struct {
	From  int
	To    int
	Value any
}

// UnmarshalJSON decodes [from, to, value].
func (d *Detail) UnmarshalJSON(b []byte) error {
	var raw []any
	if err := json.Unmarshal(b, &raw); err != nil {
		return err
	}
	if len(raw) != 3 {
		return fmt.Errorf("path detail: want 3 elements, got %d", len(raw))
	}
	f, ok1 := raw[0].(float64)
	t, ok2 := raw[1].(float64)
	if !ok1 || !ok2 {
		return errors.New("path detail: bad interval")
	}
	d.From, d.To, d.Value = int(f), int(t), raw[2]
	return nil
}

// MarshalJSON encodes [from, to, value].
func (d Detail) MarshalJSON() ([]byte, error) { return json.Marshal([]any{d.From, d.To, d.Value}) }

// Path is one GraphHopper route.
type Path struct {
	Distance     float64             `json:"distance"`
	Time         int64               `json:"time"` // ms
	Weight       float64             `json:"weight"`
	Points       string              `json:"points"`
	Instructions []Instruction       `json:"instructions"`
	Details      map[string][]Detail `json:"details"`
	Snapped      string              `json:"snapped_waypoints"`

	decoded []geo.Point
}

// Line decodes (and memoises) the path geometry.
func (p *Path) Line() ([]geo.Point, error) {
	if p.decoded != nil {
		return p.decoded, nil
	}
	pts, err := geo.Decode(p.Points)
	if err != nil {
		return nil, err
	}
	p.decoded = pts
	return pts, nil
}

// RouteResponse is GraphHopper's /route response.
type RouteResponse struct {
	Paths []Path `json:"paths"`
}

// Error is a non-2xx GraphHopper reply.
type Error struct {
	Status  int
	Message string
}

func (e *Error) Error() string { return fmt.Sprintf("graphhopper %d: %s", e.Status, e.Message) }

// IsNoRoute reports whether err means GraphHopper found no connection or could
// not snap a point (both are user-facing "no route" outcomes, not outages).
func IsNoRoute(err error) bool {
	var ge *Error
	return errors.As(err, &ge) && ge.Status == http.StatusBadRequest
}

// Route calls POST /route.
func (g *GH) Route(ctx context.Context, req RouteRequest) (*RouteResponse, error) {
	req.PointsEnc = true
	req.CalcPoints = true
	start := time.Now()
	var out RouteResponse
	err := g.post(ctx, "/route", req, &out)
	if g.Observe != nil {
		g.Observe("route", req.Profile, time.Since(start), err)
	}
	if err != nil {
		return nil, err
	}
	if len(out.Paths) == 0 {
		return nil, &Error{Status: http.StatusBadRequest, Message: "no path"}
	}
	return &out, nil
}

// MatchRequest maps a GPS trace onto the network (GPX body).
func (g *GH) Match(ctx context.Context, profile string, gpx []byte) (*RouteResponse, error) {
	var out RouteResponse
	path := fmt.Sprintf("/match?profile=%s&points_encoded=true&instructions=false&gps_accuracy=25", profile)
	if err := g.send(ctx, http.MethodPost, path, "application/gpx+xml", gpx, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// Health checks GET /health on any instance.
func (g *GH) Health(ctx context.Context) error {
	var out json.RawMessage
	err := g.send(ctx, http.MethodGet, "/health", "", nil, &out)
	var se *json.SyntaxError
	if errors.As(err, &se) { // /health answers plain "OK"
		return nil
	}
	return err
}

func (g *GH) instances() []string {
	var out []string
	for _, u := range strings.Split(g.BaseURL, ",") {
		if u = strings.TrimRight(strings.TrimSpace(u), "/"); u != "" {
			out = append(out, u)
		}
	}
	return out
}

func (g *GH) post(ctx context.Context, path string, body, out any) error {
	b, err := json.Marshal(body)
	if err != nil {
		return err
	}
	return g.send(ctx, http.MethodPost, path, "application/json", b, out)
}

// send tries the current instance first, then the others on connection errors.
func (g *GH) send(ctx context.Context, method, path, ctype string, body []byte, out any) error {
	urls := g.instances()
	if len(urls) == 0 {
		return errors.New("graphhopper: no base URL configured")
	}
	g.mu.Lock()
	start := g.current % len(urls)
	g.mu.Unlock()
	var lastErr error
	for i := 0; i < len(urls); i++ {
		idx := (start + i) % len(urls)
		var rd io.Reader
		if body != nil {
			rd = bytes.NewReader(body)
		}
		hreq, err := http.NewRequestWithContext(ctx, method, urls[idx]+path, rd)
		if err != nil {
			return err
		}
		if ctype != "" {
			hreq.Header.Set("Content-Type", ctype)
		}
		err = g.do(hreq, out)
		var ge *Error
		if err == nil || errors.As(err, &ge) || ctx.Err() != nil {
			if err == nil && idx != start {
				g.mu.Lock()
				g.current = idx
				g.mu.Unlock()
			}
			return err
		}
		lastErr = err
	}
	return lastErr
}

func (g *GH) do(hreq *http.Request, out any) error {
	resp, err := g.HTTP.Do(hreq)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 32<<20))
	if err != nil {
		return err
	}
	if resp.StatusCode/100 != 2 {
		var e struct {
			Message string `json:"message"`
		}
		_ = json.Unmarshal(body, &e)
		if e.Message == "" {
			e.Message = string(body)
		}
		return &Error{Status: resp.StatusCode, Message: e.Message}
	}
	return json.Unmarshal(body, out)
}
