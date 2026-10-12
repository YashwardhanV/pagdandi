package routing

// RecommendInput describes the standard route and one shortcut option.
type RecommendInput struct {
	StdDistanceM int
	StdDurationS int
	ScDistanceM  int
	ScDurationS  int
	// Confidence of the community leg; nil when the option is OSM-only.
	Confidence *float64
	Daylight   bool
	// TrackM is the metres of track on the shortcut option.
	TrackM int
	// Lit reports whether the track is known to be lit; GraphHopper 11 has no
	// lit encoded value, so this is false unless a community shortcut says otherwise.
	Lit bool
}

// ShouldRecommendShortcut applies SPEC §6.3 step 6: recommend the shortcut if
// it saves ≥2 min or ≥20 % distance, its confidence is ≥0.6 (or it is OSM-only),
// and it is not an unlit night track.
func ShouldRecommendShortcut(in RecommendInput) bool {
	savesTime := in.StdDurationS-in.ScDurationS >= 120
	savesDist := in.StdDistanceM > 0 && float64(in.StdDistanceM-in.ScDistanceM) >= 0.2*float64(in.StdDistanceM)
	if !savesTime && !savesDist {
		return false
	}
	// A shortcut that is slower is never recommended, even if shorter.
	if in.ScDurationS > in.StdDurationS {
		return false
	}
	if in.Confidence != nil && *in.Confidence < 0.6 {
		return false
	}
	if !in.Daylight && in.TrackM > 0 && !in.Lit {
		return false
	}
	return true
}
