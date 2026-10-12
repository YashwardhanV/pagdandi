package routing

import (
	"strconv"

	"pagdandi/api/internal/regions"
)

// ProfilePlan names the GraphHopper profiles used for one request.
type ProfilePlan struct {
	Standard string // always present: the usual way
	Shortcut string // empty when mode == standard
	// MinCommunityConfidence is the confidence floor for overlay candidates.
	MinCommunityConfidence float64
}

// Vehicles per country.
var vehiclesByCountry = map[string][]string{
	"IN": {"scooter", "motorcycle"},
	"SE": {"bicycle", "moped"},
}

// ValidVehicle reports whether a vehicle may be routed in the country.
func ValidVehicle(country, vehicle string) bool {
	for _, v := range vehiclesByCountry[country] {
		if v == vehicle {
			return true
		}
	}
	return false
}

// PlanProfiles picks profiles from country, vehicle, mode and daylight (SPEC §6.1).
func PlanProfiles(country, vehicle, mode string, daylight bool) ProfilePlan {
	var p ProfilePlan
	p.MinCommunityConfidence = 0.5
	switch country {
	case "SE":
		prefix := "bike"
		if vehicle == "moped" {
			prefix = "moped"
		}
		p.Standard = prefix + "_standard"
		if mode != "standard" {
			p.Shortcut = prefix + "_shortcut"
		}
	default: // IN: scooter and motorcycle share the two-wheeler profiles
		p.Standard = "tw_standard"
		switch mode {
		case "standard":
		case "adventure":
			p.Shortcut = "tw_adventure"
		default:
			if daylight {
				p.Shortcut = "tw_shortcut"
			} else {
				p.Shortcut = "tw_shortcut_night"
			}
		}
	}
	if mode == "adventure" {
		// Low-confidence shortcuts appear only in adventure mode (SPEC §7.3).
		p.MinCommunityConfidence = 0
	}
	return p
}

// unpavedCondition matches GraphHopper surfaces we treat as unpaved.
const unpavedCondition = "surface == DIRT || surface == GROUND || surface == GRAVEL || surface == FINE_GRAVEL || surface == SAND || surface == GRASS || surface == UNPAVED || surface == COMPACTED || road_class == TRACK"

// SeasonModel returns the per-request custom model for an active season. It
// only ever lowers priority (multiply_by in [0,1]) so landmarks stay valid.
func SeasonModel(s *regions.Season) *CustomModel {
	if s == nil || s.UnpavedPenalty <= 1 {
		return nil
	}
	f := 1 / s.UnpavedPenalty
	return &CustomModel{Priority: []Statement{{If: unpavedCondition, MultiplyBy: strconv.FormatFloat(f, 'f', 4, 64)}}}
}

// CommunitySpeedKmh is the assumed riding speed on a community shortcut.
func CommunitySpeedKmh(vehicle, surface string) float64 {
	switch vehicle {
	case "bicycle":
		switch surface {
		case "paved":
			return 16
		case "unpaved":
			return 12
		default:
			return 10
		}
	case "moped":
		switch surface {
		case "paved":
			return 25
		case "unpaved":
			return 15
		default:
			return 12
		}
	default: // scooter / motorcycle
		switch surface {
		case "paved":
			return 20
		case "unpaved":
			return 15
		default:
			return 12
		}
	}
}
