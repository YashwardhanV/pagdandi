// Package shortcuts holds the community-shortcut domain: confidence maths,
// status lifecycle, trust, snapping and persistence.
package shortcuts

import (
	"math"
	"time"
)

// Verdicts a rider can give.
const (
	VerdictPassable = "passable"
	VerdictBlocked  = "blocked"
	VerdictPartly   = "partly"
)

// Statuses of a shortcut.
const (
	StatusPending = "pending"
	StatusActive  = "active"
	StatusStale   = "stale"
	StatusBlocked = "blocked"
	StatusRemoved = "removed"
)

// Report is one verification, with the reporter's trust at evaluation time.
type Report struct {
	UserID    string
	Verdict   string
	Reason    string
	CreatedAt time.Time
	Trust     float64
}

// DefaultHalfLifeDays applies when no season is active.
const DefaultHalfLifeDays = 30.0

// ClampTrust keeps trust within [0.5, 2.0].
func ClampTrust(t float64) float64 {
	if t == 0 || math.IsNaN(t) {
		return 1
	}
	return math.Max(0.5, math.Min(2.0, t))
}

// Confidence computes the Beta-posterior confidence of SPEC §7.3:
// prior α=β=1; each report adds w·t where w = 0.5^(age/half_life).
func Confidence(reports []Report, now time.Time, halfLifeDays float64) (alpha, beta, conf float64) {
	if halfLifeDays <= 0 {
		halfLifeDays = DefaultHalfLifeDays
	}
	alpha, beta = 1, 1
	for _, r := range reports {
		age := now.Sub(r.CreatedAt).Hours() / 24
		if age < 0 {
			age = 0
		}
		w := math.Pow(0.5, age/halfLifeDays)
		t := ClampTrust(r.Trust)
		switch r.Verdict {
		case VerdictPassable:
			alpha += w * t
		case VerdictBlocked:
			beta += w * t
		case VerdictPartly:
			alpha += 0.5 * w * t
			beta += 0.5 * w * t
		}
	}
	return alpha, beta, alpha / (alpha + beta)
}

// Band names a confidence value for the UI.
func Band(conf float64) string {
	switch {
	case conf >= 0.75:
		return "high"
	case conf >= 0.5:
		return "medium"
	default:
		return "low"
	}
}

// LifecycleInput is everything NextStatus needs to know about a shortcut.
type LifecycleInput struct {
	Status       string
	CreatedBy    string
	CreatorTrust float64
	CreatedAt    time.Time
	Reports      []Report
	Confidence   float64
	Now          time.Time
}

// NextStatus applies the hourly lifecycle rules of SPEC §7.4. Removal is a
// moderator action and is never undone here.
func NextStatus(in LifecycleInput) string {
	if in.Status == StatusRemoved {
		return StatusRemoved
	}
	var lastReport time.Time
	blocked7d := 0
	passableUsers := map[string]bool{}
	var latest *Report
	for i := range in.Reports {
		r := &in.Reports[i]
		if r.CreatedAt.After(lastReport) {
			lastReport = r.CreatedAt
			latest = r
		}
		if r.Verdict == VerdictBlocked && in.Now.Sub(r.CreatedAt) <= 7*24*time.Hour {
			blocked7d++
		}
		if r.Verdict == VerdictPassable && r.UserID != in.CreatedBy {
			passableUsers[r.UserID] = true
		}
	}

	// Any state → blocked.
	if blocked7d >= 2 && in.Confidence < 0.4 {
		return StatusBlocked
	}

	switch in.Status {
	case StatusPending:
		if len(passableUsers) >= 2 || ClampTrust(in.CreatorTrust) >= 1.5 {
			return StatusActive
		}
		return StatusPending
	case StatusActive:
		ref := lastReport
		if ref.IsZero() {
			ref = in.CreatedAt
		}
		if in.Now.Sub(ref) >= 60*24*time.Hour {
			return StatusStale
		}
		return StatusActive
	case StatusStale:
		// A fresh passable confirmation brings it back.
		if latest != nil && latest.Verdict == VerdictPassable && in.Now.Sub(latest.CreatedAt) < 60*24*time.Hour && in.Confidence >= 0.5 {
			return StatusActive
		}
		return StatusStale
	case StatusBlocked:
		// Recovers once riders confirm it again and the blocked evidence has decayed.
		if latest != nil && latest.Verdict == VerdictPassable && in.Confidence >= 0.5 {
			return StatusActive
		}
		return StatusBlocked
	}
	return in.Status
}

// TrustDelta is the trust change per report that agreed (+) or disagreed (−)
// with the eventual consensus.
const TrustDelta = 0.05

// Consensus returns "passable", "blocked" or "" (no consensus yet) for a confidence.
func Consensus(conf float64) string {
	switch {
	case conf >= 0.75:
		return VerdictPassable
	case conf <= 0.25:
		return VerdictBlocked
	}
	return ""
}

// TrustFromHistory recomputes a user's trust from scratch: start at 1.0 and
// move by TrustDelta for every report that agreed or disagreed with the
// shortcut's consensus. "partly" reports are neutral.
func TrustFromHistory(agree, disagree int) float64 {
	return ClampTrust(1.0 + TrustDelta*float64(agree-disagree))
}
