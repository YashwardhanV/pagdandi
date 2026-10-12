package routing

import (
	"errors"
	"fmt"
	"time"

	"pagdandi/api/internal/geo"
)

// Request is a validated POST /v1/route body.
type Request struct {
	From     geo.Point  `json:"from"`
	To       geo.Point  `json:"to"`
	Vehicle  string     `json:"vehicle"`
	Mode     string     `json:"mode"` // auto | standard | shortcut | adventure
	DepartAt *time.Time `json:"depart_at,omitempty"`
	Lang     string     `json:"lang"`
}

// Modes accepted by the API.
var Modes = []string{"auto", "standard", "shortcut", "adventure"}

// Context echoes what shaped the answer.
type Context struct {
	Daylight      bool     `json:"daylight"`
	Season        *string  `json:"season"`
	LandslideRisk bool     `json:"landslide_risk,omitempty"`
	Vehicle       string   `json:"vehicle"`
	Mode          string   `json:"mode"`
	Profiles      []string `json:"profiles"`
	Sunrise       string   `json:"sunrise,omitempty"`
	Sunset        string   `json:"sunset,omitempty"`
}

// SurfaceBreakdown is metres by surface class.
type SurfaceBreakdown struct {
	PavedM   int `json:"paved_m"`
	UnpavedM int `json:"unpaved_m"`
	TrackM   int `json:"track_m"`
}

// Surface classes.
const (
	SurfPaved   = "paved"
	SurfUnpaved = "unpaved"
	SurfTrack   = "track"
)

// SurfaceSpan marks a run of polyline points sharing a surface class.
type SurfaceSpan struct {
	FromIdx int    `json:"from_idx"`
	ToIdx   int    `json:"to_idx"`
	Kind    string `json:"kind"`
}

// Leg marks which part of the polyline came from OSM and which from a rider shortcut.
type Leg struct {
	Source     string `json:"source"` // osm | community
	ShortcutID string `json:"shortcut_id,omitempty"`
	Title      string `json:"title,omitempty"`
	FromIdx    int    `json:"from_idx"`
	ToIdx      int    `json:"to_idx"`
}

// Warning is something the rider should know about an option.
type Warning struct {
	Type    string  `json:"type"` // unpaved | gate | steep | seasonal | night | low_confidence | floods | narrow | livestock | no_lights
	AtM     *int    `json:"at_m,omitempty"`
	LengthM *int    `json:"length_m,omitempty"`
	Surface string  `json:"surface,omitempty"`
	Season  string  `json:"season,omitempty"`
	Detail  string  `json:"detail,omitempty"`
	Lat     float64 `json:"lat,omitempty"`
	Lng     float64 `json:"lng,omitempty"`
}

// Instr is one maneuver for navigation.
type Instr struct {
	Text      string `json:"text"`
	Street    string `json:"street,omitempty"`
	DistanceM int    `json:"distance_m"`
	DurationS int    `json:"duration_s"`
	Sign      int    `json:"sign"`
	Interval  [2]int `json:"interval"`
	Exit      int    `json:"exit,omitempty"`
}

// Savings is how much an option saves against the standard route.
type Savings struct {
	DistanceM int `json:"distance_m"`
	DurationS int `json:"duration_s"`
}

// Option is one route alternative.
type Option struct {
	ID               string           `json:"id"`
	Kind             string           `json:"kind"` // standard | shortcut
	Profile          string           `json:"profile"`
	DistanceM        int              `json:"distance_m"`
	DurationS        int              `json:"duration_s"`
	Polyline         string           `json:"polyline"`
	Legs             []Leg            `json:"legs"`
	SurfaceBreakdown SurfaceBreakdown `json:"surface_breakdown"`
	SurfaceSpans     []SurfaceSpan    `json:"surface_spans"`
	Confidence       *float64         `json:"confidence,omitempty"`
	LastConfirmedAt  *time.Time       `json:"last_confirmed_at,omitempty"`
	Confirmations30d *int             `json:"confirmations_30d,omitempty"`
	Warnings         []Warning        `json:"warnings"`
	Savings          *Savings         `json:"savings,omitempty"`
	Instructions     []Instr          `json:"instructions"`
}

// Response is the POST /v1/route body.
type Response struct {
	RequestID   string   `json:"request_id"`
	Region      string   `json:"region"`
	Context     Context  `json:"context"`
	Options     []Option `json:"options"`
	Recommended string   `json:"recommended"`
	Attribution string   `json:"attribution"`
	Disclaimer  string   `json:"disclaimer,omitempty"`
	Cached      bool     `json:"cached,omitempty"`
}

// Attribution is shown with every route.
const Attribution = "© OpenStreetMap contributors"

// Disclaimer is shown on shortcut routes (SPEC §17).
const Disclaimer = "Unverified roads can change. Ride at your own judgement."

// OutOfCoverageError means an endpoint lies outside every coverage polygon.
type OutOfCoverageError struct {
	Which         string // from | to
	NearestRegion string
	NearestName   string
	NearestDistKm float64
}

func (e *OutOfCoverageError) Error() string {
	return fmt.Sprintf("%s point is outside coverage; nearest region is %s (%.0f km away)", e.Which, e.NearestName, e.NearestDistKm)
}

// ErrNoRoute means no connection exists for the profile.
var ErrNoRoute = errors.New("no route found between these points")

// ErrCrossRegion means origin and destination are in different countries' graphs.
var ErrCrossRegion = errors.New("origin and destination are in different countries")

// ValidationError is a bad request field.
type ValidationError struct{ Msg string }

func (e *ValidationError) Error() string { return e.Msg }
