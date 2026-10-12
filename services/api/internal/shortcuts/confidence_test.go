package shortcuts

import (
	"math"
	"testing"
	"time"
)

var now = time.Date(2026, 10, 12, 12, 0, 0, 0, time.UTC)

func daysAgo(d float64) time.Time { return now.Add(-time.Duration(d * 24 * float64(time.Hour))) }

func TestConfidencePrior(t *testing.T) {
	a, b, c := Confidence(nil, now, 30)
	if a != 1 || b != 1 || c != 0.5 {
		t.Fatalf("prior = %v %v %v", a, b, c)
	}
}

func TestConfidenceFreshReports(t *testing.T) {
	rs := []Report{
		{UserID: "a", Verdict: VerdictPassable, CreatedAt: now, Trust: 1},
		{UserID: "b", Verdict: VerdictPassable, CreatedAt: now, Trust: 1},
		{UserID: "c", Verdict: VerdictPassable, CreatedAt: now, Trust: 1},
	}
	_, _, c := Confidence(rs, now, 30)
	if math.Abs(c-0.8) > 1e-9 { // (1+3)/(1+3+1)
		t.Fatalf("conf = %v", c)
	}
	if Band(c) != "high" {
		t.Fatalf("band = %s", Band(c))
	}
}

func TestConfidenceDecay(t *testing.T) {
	// One report exactly one half-life old counts as 0.5.
	rs := []Report{{Verdict: VerdictPassable, CreatedAt: daysAgo(30), Trust: 1}}
	a, b, _ := Confidence(rs, now, 30)
	if math.Abs(a-1.5) > 1e-9 || b != 1 {
		t.Fatalf("alpha/beta = %v/%v", a, b)
	}
	// In monsoon (half-life 7) the same report weighs far less.
	a, _, _ = Confidence(rs, now, 7)
	if want := 1 + math.Pow(0.5, 30.0/7); math.Abs(a-want) > 1e-9 {
		t.Fatalf("monsoon alpha = %v want %v", a, want)
	}
}

func TestConfidenceTrustAndPartly(t *testing.T) {
	rs := []Report{
		{Verdict: VerdictBlocked, CreatedAt: now, Trust: 2.0},
		{Verdict: VerdictPartly, CreatedAt: now, Trust: 1.0},
		{Verdict: VerdictPassable, CreatedAt: now, Trust: 9.0}, // clamped to 2
	}
	a, b, c := Confidence(rs, now, 30)
	if math.Abs(a-3.5) > 1e-9 || math.Abs(b-3.5) > 1e-9 || math.Abs(c-0.5) > 1e-9 {
		t.Fatalf("a=%v b=%v c=%v", a, b, c)
	}
}

func TestBands(t *testing.T) {
	cases := map[float64]string{0.9: "high", 0.75: "high", 0.74: "medium", 0.5: "medium", 0.49: "low"}
	for v, want := range cases {
		if got := Band(v); got != want {
			t.Errorf("Band(%v) = %s want %s", v, got, want)
		}
	}
}

func TestLifecycle(t *testing.T) {
	pass := func(u string, d float64) Report {
		return Report{UserID: u, Verdict: VerdictPassable, CreatedAt: daysAgo(d), Trust: 1}
	}
	block := func(u string, d float64) Report {
		return Report{UserID: u, Verdict: VerdictBlocked, CreatedAt: daysAgo(d), Trust: 1}
	}
	cases := []struct {
		name string
		in   LifecycleInput
		want string
	}{
		{"pending stays with one confirmation",
			LifecycleInput{Status: StatusPending, CreatedBy: "c", CreatorTrust: 1, Reports: []Report{pass("a", 1)}, Confidence: 0.67, Now: now}, StatusPending},
		{"creator's own report does not count",
			LifecycleInput{Status: StatusPending, CreatedBy: "c", CreatorTrust: 1, Reports: []Report{pass("c", 1), pass("a", 1)}, Confidence: 0.75, Now: now}, StatusPending},
		{"two independent confirmations activate",
			LifecycleInput{Status: StatusPending, CreatedBy: "c", CreatorTrust: 1, Reports: []Report{pass("a", 1), pass("b", 2)}, Confidence: 0.75, Now: now}, StatusActive},
		{"same user twice is not independent",
			LifecycleInput{Status: StatusPending, CreatedBy: "c", CreatorTrust: 1, Reports: []Report{pass("a", 1), pass("a", 3)}, Confidence: 0.75, Now: now}, StatusPending},
		{"trusted creator activates immediately",
			LifecycleInput{Status: StatusPending, CreatedBy: "c", CreatorTrust: 1.5, Now: now, Confidence: 0.5}, StatusActive},
		{"active goes stale after 60 quiet days",
			LifecycleInput{Status: StatusActive, Reports: []Report{pass("a", 61)}, Confidence: 0.55, Now: now}, StatusStale},
		{"active stays active with recent report",
			LifecycleInput{Status: StatusActive, Reports: []Report{pass("a", 10)}, Confidence: 0.6, Now: now}, StatusActive},
		{"active with no reports uses creation date",
			LifecycleInput{Status: StatusActive, CreatedAt: daysAgo(70), Confidence: 0.5, Now: now}, StatusStale},
		{"two blocked in a week with low confidence blocks",
			LifecycleInput{Status: StatusActive, Reports: []Report{block("a", 1), block("b", 2)}, Confidence: 0.33, Now: now}, StatusBlocked},
		{"two blocked but confidence still ok stays",
			LifecycleInput{Status: StatusActive, Reports: []Report{block("a", 1), block("b", 2)}, Confidence: 0.45, Now: now}, StatusActive},
		{"old blocked reports do not block",
			LifecycleInput{Status: StatusActive, Reports: []Report{block("a", 8), block("b", 9)}, Confidence: 0.3, Now: now}, StatusActive},
		{"stale returns with fresh passable",
			LifecycleInput{Status: StatusStale, Reports: []Report{pass("a", 90), pass("b", 1)}, Confidence: 0.6, Now: now}, StatusActive},
		{"blocked recovers on passable",
			LifecycleInput{Status: StatusBlocked, Reports: []Report{block("a", 20), block("b", 21), pass("c", 0)}, Confidence: 0.55, Now: now}, StatusActive},
		{"removed is final",
			LifecycleInput{Status: StatusRemoved, Reports: []Report{pass("a", 0), pass("b", 0)}, Confidence: 0.9, Now: now}, StatusRemoved},
	}
	for _, c := range cases {
		if got := NextStatus(c.in); got != c.want {
			t.Errorf("%s: got %s want %s", c.name, got, c.want)
		}
	}
}

func TestTrust(t *testing.T) {
	if TrustFromHistory(0, 0) != 1 {
		t.Fatal("start at 1")
	}
	if TrustFromHistory(100, 0) != 2 || TrustFromHistory(0, 100) != 0.5 {
		t.Fatal("clamped to [0.5,2]")
	}
	if math.Abs(TrustFromHistory(10, 0)-1.5) > 1e-9 {
		t.Fatal("10 agreements reach trusted rider")
	}
	if Consensus(0.8) != VerdictPassable || Consensus(0.2) != VerdictBlocked || Consensus(0.5) != "" {
		t.Fatal("consensus thresholds")
	}
}
