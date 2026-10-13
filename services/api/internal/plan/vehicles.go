// Package plan turns "from here to there on this vehicle" into a handful of
// honest, different trails (Go mode), and "surprise me within N km" into
// places and loops (Wander mode).
package plan

import (
	"strings"

	"pagdandi/api/internal/gh"
	"pagdandi/api/internal/trail"
)

// Vehicle is one way of moving.
type Vehicle struct {
	ID       string  `json:"id"`
	Label    string  `json:"label"`
	LabelHi  string  `json:"label_hi"`
	Profile  string  `json:"-"`          // GraphHopper profile; "" for water
	Water    bool    `json:"water"`      // jetski
	Motor    bool    `json:"motor"`      // has an engine (fuel, legal rules)
	CruiseKm float64 `json:"cruise_kmh"` // for water and estimates
	MaxKm    float64 `json:"max_km"`     // longest sensible Wander radius
}

// Vehicles in display order.
var Vehicles = []Vehicle{
	{ID: "bike", Label: "Bike", LabelHi: "बाइक", Profile: "moto", Motor: true, CruiseKm: 45, MaxKm: 100},
	{ID: "scooty", Label: "Scooty", LabelHi: "स्कूटी", Profile: "moto", Motor: true, CruiseKm: 38, MaxKm: 100},
	{ID: "car", Label: "Car", LabelHi: "कार", Profile: "car", Motor: true, CruiseKm: 50, MaxKm: 100},
	{ID: "cycle", Label: "Cycle", LabelHi: "साइकिल", Profile: "cycle", CruiseKm: 15, MaxKm: 100},
	{ID: "walk", Label: "Walk", LabelHi: "पैदल", Profile: "walk", CruiseKm: 4.5, MaxKm: 30},
	{ID: "jetski", Label: "Jetski", LabelHi: "जेटस्की", Water: true, Motor: true, CruiseKm: 35, MaxKm: 100},
}

// VehicleByID looks a vehicle up (case-insensitive).
func VehicleByID(id string) (Vehicle, bool) {
	for _, v := range Vehicles {
		if strings.EqualFold(v.ID, id) {
			return v, true
		}
	}
	return Vehicle{}, false
}

func ptr(f float64) *float64 { return &f }

func st(cond, mult string) gh.Statement   { return gh.Statement{If: cond, MultiplyBy: mult} }
func limit(cond, lim string) gh.Statement { return gh.Statement{If: cond, LimitTo: lim} }

const (
	mainRoads    = "road_class == MOTORWAY || road_class == TRUNK || road_class == PRIMARY"
	paved        = "surface == ASPHALT || surface == CONCRETE || surface == PAVED || surface == PAVING_STONES || surface == CEMENT"
	unpaved      = "surface == DIRT || surface == GROUND || surface == GRAVEL || surface == SAND || surface == GRASS || surface == UNPAVED || surface == COMPACTED || surface == FINE_GRAVEL"
	softGround   = "surface == DIRT || surface == GROUND || surface == SAND || surface == GRASS"
	roughTrack   = "road_class == TRACK && (track_type == GRADE4 || track_type == GRADE5)"
	midTrack     = "road_class == TRACK && track_type == GRADE3"
	footOnly     = "road_class == PATH || road_class == FOOTWAY || road_class == BRIDLEWAY || road_class == STEPS || road_class == PEDESTRIAN"
	minorOrTrack = "road_class == TRACK || road_class == UNCLASSIFIED || road_class == RESIDENTIAL || road_class == SERVICE || road_class == LIVING_STREET"
)

// Model is the per-request custom model for a trail kind on a vehicle. Models
// only lower priority or speed relative to the base profile (keeps LM valid).
func Model(kind string, v Vehicle) *gh.Model {
	m := &gh.Model{}
	switch kind {
	case trail.Usual:
		m.DistanceInfluence = ptr(40)
		m.Priority = []gh.Statement{
			st("road_class == TRACK", "0.08"),
			st(unpaved, "0.35"),
			st(footOnly, "0.4"),
			st("road_class == RESIDENTIAL || road_class == SERVICE || road_class == LIVING_STREET", "0.75"),
		}
	case trail.Shortest:
		m.DistanceInfluence = ptr(600)
		m.Priority = []gh.Statement{st(roughTrack, "0.5")}
	case trail.Kaccha:
		m.DistanceInfluence = ptr(70)
		m.Priority = []gh.Statement{
			st(mainRoads, "0.3"),
			st("road_class == SECONDARY", "0.45"),
			st("road_class == TERTIARY", "0.65"),
			st(paved, "0.7"),
		}
	case trail.Quiet:
		m.DistanceInfluence = ptr(60)
		m.Priority = []gh.Statement{
			st(mainRoads, "0.25"),
			st("road_class == SECONDARY", "0.4"),
			st("road_class == TERTIARY", "0.7"),
			st("urban_density == CITY", "0.7"),
			st("road_class == TRACK", "0.7"),
		}
	case trail.Scenic, trail.Fresh, trail.Loop:
		m.DistanceInfluence = ptr(50)
		m.Priority = []gh.Statement{
			st(mainRoads, "0.5"),
			st("road_class == SECONDARY", "0.75"),
			st("urban_density == CITY", "0.8"),
		}
	}
	switch v.ID {
	case "scooty":
		// Small wheels: no grade 4–5 tracks, gentle on sand and ruts, 50 km/h top.
		m.Priority = append(m.Priority, st(roughTrack, "0.05"), st(midTrack, "0.5"), st("surface == SAND", "0.3"))
		m.Speed = append(m.Speed, limit("true", "50"), limit(softGround, "15"))
	case "car":
		m.Priority = append(m.Priority, st(midTrack, "0.5"))
	case "cycle":
		if kind != trail.Shortest {
			m.Priority = append(m.Priority, st("road_class == TRUNK", "0.35"), st("road_class == PRIMARY", "0.6"))
		}
	case "walk":
		if kind == trail.Kaccha {
			m.Priority = append(m.Priority, st(minorOrTrack+" || "+footOnly, "1"), st("road_class == SECONDARY || road_class == TERTIARY", "0.5"))
		}
	}
	return m
}
