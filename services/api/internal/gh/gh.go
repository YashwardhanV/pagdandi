// Package gh is a small GraphHopper HTTP client (POST /route, GET /health).
package gh

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// Client talks to one GraphHopper server.
type Client struct {
	BaseURL string
	HTTP    *http.Client
}

// New returns a client with sensible timeouts.
func New(baseURL string) *Client {
	return &Client{BaseURL: strings.TrimRight(baseURL, "/"), HTTP: &http.Client{Timeout: 20 * time.Second}}
}

// Statement is one custom-model rule.
type Statement struct {
	If         string  `json:"if,omitempty"`
	ElseIf     string  `json:"else_if,omitempty"`
	Else       *string `json:"else,omitempty"`
	MultiplyBy string  `json:"multiply_by,omitempty"`
	LimitTo    string  `json:"limit_to,omitempty"`
}

// Model is a per-request custom model, merged onto the server profile.
type Model struct {
	DistanceInfluence *float64    `json:"distance_influence,omitempty"`
	Priority          []Statement `json:"priority,omitempty"`
	Speed             []Statement `json:"speed,omitempty"`
}

// Request is the subset of GraphHopper's POST /route body Pagdandi uses.
type Request struct {
	Points        [][2]float64 `json:"points"` // [lng, lat]
	Profile       string       `json:"profile"`
	Locale        string       `json:"locale,omitempty"`
	Instructions  bool         `json:"instructions"`
	CalcPoints    bool         `json:"calc_points"`
	PointsEncoded bool         `json:"points_encoded"`
	Elevation     bool         `json:"elevation"`
	Details       []string     `json:"details,omitempty"`
	PassThrough   bool         `json:"pass_through,omitempty"`
	Algorithm     string       `json:"algorithm,omitempty"`
	RoundTripDist float64      `json:"round_trip.distance,omitempty"`
	RoundTripSeed int64        `json:"round_trip.seed,omitempty"`
	Headings      []float64    `json:"headings,omitempty"`
	MaxPaths      int          `json:"alternative_route.max_paths,omitempty"`
	MaxWeightF    float64      `json:"alternative_route.max_weight_factor,omitempty"`
	MaxShareF     float64      `json:"alternative_route.max_share_factor,omitempty"`
	CustomModel   *Model       `json:"custom_model,omitempty"`
}

// Instruction is one manoeuvre.
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

// String returns the value as a lower-case string ("" for null).
func (d Detail) String() string {
	switch v := d.Value.(type) {
	case string:
		return strings.ToLower(v)
	case float64:
		return fmt.Sprint(int64(v))
	}
	return ""
}

// Int returns the value as an integer (0 if not numeric).
func (d Detail) Int() int64 {
	if v, ok := d.Value.(float64); ok {
		return int64(v)
	}
	return 0
}

// LineString is a GeoJSON geometry with optional elevation.
type LineString struct {
	Coordinates [][]float64 `json:"coordinates"` // [lng, lat(, ele)]
}

// Path is one route.
type Path struct {
	Distance     float64             `json:"distance"`
	Time         int64               `json:"time"` // ms
	Weight       float64             `json:"weight"`
	Ascend       float64             `json:"ascend"`
	Descend      float64             `json:"descend"`
	Points       LineString          `json:"points"`
	Instructions []Instruction       `json:"instructions"`
	Details      map[string][]Detail `json:"details"`
}

// Response is GraphHopper's /route response.
type Response struct {
	Paths []Path `json:"paths"`
}

// Error is a non-2xx GraphHopper reply.
type Error struct {
	Status  int
	Message string
}

func (e *Error) Error() string { return fmt.Sprintf("graphhopper %d: %s", e.Status, e.Message) }

// IsNoRoute reports whether err means "no connection / point not found" rather than an outage.
func IsNoRoute(err error) bool {
	var ge *Error
	return errors.As(err, &ge) && ge.Status == http.StatusBadRequest
}

// Route calls POST /route with GeoJSON points.
func (c *Client) Route(ctx context.Context, req Request) (*Path, error) {
	req.PointsEncoded = false
	req.CalcPoints = true
	var out Response
	if err := c.send(ctx, http.MethodPost, "/route", req, &out); err != nil {
		return nil, err
	}
	if len(out.Paths) == 0 || len(out.Paths[0].Points.Coordinates) < 2 {
		return nil, &Error{Status: http.StatusBadRequest, Message: "no path"}
	}
	return &out.Paths[0], nil
}

// Routes calls POST /route and returns every path (alternatives).
func (c *Client) Routes(ctx context.Context, req Request) ([]Path, error) {
	req.PointsEncoded = false
	req.CalcPoints = true
	var out Response
	if err := c.send(ctx, http.MethodPost, "/route", req, &out); err != nil {
		return nil, err
	}
	if len(out.Paths) == 0 {
		return nil, &Error{Status: http.StatusBadRequest, Message: "no path"}
	}
	return out.Paths, nil
}

// Health checks GET /health.
func (c *Client) Health(ctx context.Context) error {
	hreq, err := http.NewRequestWithContext(ctx, http.MethodGet, c.BaseURL+"/health", nil)
	if err != nil {
		return err
	}
	resp, err := c.HTTP.Do(hreq)
	if err != nil {
		return err
	}
	resp.Body.Close()
	if resp.StatusCode != 200 {
		return &Error{Status: resp.StatusCode, Message: "unhealthy"}
	}
	return nil
}

func (c *Client) send(ctx context.Context, method, path string, body, out any) error {
	b, err := json.Marshal(body)
	if err != nil {
		return err
	}
	hreq, err := http.NewRequestWithContext(ctx, method, c.BaseURL+path, bytes.NewReader(b))
	if err != nil {
		return err
	}
	hreq.Header.Set("Content-Type", "application/json")
	resp, err := c.HTTP.Do(hreq)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	rb, err := io.ReadAll(io.LimitReader(resp.Body, 64<<20))
	if err != nil {
		return err
	}
	if resp.StatusCode/100 != 2 {
		var e struct {
			Message string `json:"message"`
		}
		_ = json.Unmarshal(rb, &e)
		if e.Message == "" {
			e.Message = string(rb)
		}
		return &Error{Status: resp.StatusCode, Message: e.Message}
	}
	return json.Unmarshal(rb, out)
}
