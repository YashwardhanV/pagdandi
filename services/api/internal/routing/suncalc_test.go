package routing

import (
	"testing"
	"time"
)

func within(t *testing.T, label string, got, want time.Time, tol time.Duration) {
	t.Helper()
	d := got.Sub(want)
	if d < 0 {
		d = -d
	}
	if d > tol {
		t.Errorf("%s: got %s want %s (off by %s)", label, got.Format(time.RFC3339), want.Format(time.RFC3339), d)
	}
}

func TestSunTimes(t *testing.T) {
	ist, _ := time.LoadLocation("Asia/Kolkata")
	sto, _ := time.LoadLocation("Europe/Stockholm")

	// Indore, 21 June 2026: sunrise ≈ 05:41 IST, sunset ≈ 19:13 IST (NOAA).
	rise, set, ok := SunTimes(time.Date(2026, 6, 21, 12, 0, 0, 0, ist), 22.7196, 75.8577)
	if !ok {
		t.Fatal("expected sun times")
	}
	within(t, "indore rise", rise, time.Date(2026, 6, 21, 5, 41, 0, 0, ist), 4*time.Minute)
	within(t, "indore set", set, time.Date(2026, 6, 21, 19, 13, 0, 0, ist), 4*time.Minute)

	// Stockholm, 21 December 2026: sunrise ≈ 08:43, sunset ≈ 14:48 CET.
	rise, set, ok = SunTimes(time.Date(2026, 12, 21, 12, 0, 0, 0, sto), 59.3293, 18.0686)
	if !ok {
		t.Fatal("expected sun times")
	}
	within(t, "stockholm rise", rise, time.Date(2026, 12, 21, 8, 43, 0, 0, sto), 5*time.Minute)
	within(t, "stockholm set", set, time.Date(2026, 12, 21, 14, 48, 0, 0, sto), 5*time.Minute)
}

func TestIsDaylight(t *testing.T) {
	ist, _ := time.LoadLocation("Asia/Kolkata")
	lat, lng := 22.7196, 75.8577
	if !IsDaylight(time.Date(2026, 10, 12, 13, 0, 0, 0, ist), lat, lng) {
		t.Error("1pm should be daylight")
	}
	if IsDaylight(time.Date(2026, 10, 12, 22, 0, 0, 0, ist), lat, lng) {
		t.Error("10pm should be night")
	}
	if IsDaylight(time.Date(2026, 10, 12, 4, 0, 0, 0, ist), lat, lng) {
		t.Error("4am should be night")
	}
	// Midsummer midnight in Tromsø is polar day; midwinter noon is polar night.
	if !IsDaylight(time.Date(2026, 6, 21, 0, 0, 0, 0, time.UTC), 69.65, 18.96) {
		t.Error("polar day expected")
	}
	if IsDaylight(time.Date(2026, 12, 21, 11, 0, 0, 0, time.UTC), 69.65, 18.96) {
		t.Error("polar night expected")
	}
}
