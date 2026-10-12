package regions

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

func repoFile(t *testing.T, rel string) string {
	_, here, _, _ := runtime.Caller(0)
	return filepath.Join(filepath.Dir(here), "..", "..", "..", "..", rel)
}

func TestLoadRepoRegions(t *testing.T) {
	p := repoFile(t, "data/regions.yaml")
	if _, err := os.Stat(p); err != nil {
		t.Skip("regions.yaml not found")
	}
	s, err := Load(p)
	if err != nil {
		t.Fatal(err)
	}
	if len(s.Regions) != 18 {
		t.Fatalf("want 18 regions, got %d", len(s.Regions))
	}
	sth, ok := s.Get("stockholm")
	if !ok || sth.Country != "SE" || sth.Timezone != "Europe/Stockholm" || sth.DefaultVehicle != "bicycle" {
		t.Fatalf("stockholm defaults wrong: %+v", sth)
	}
	ind, _ := s.Get("indore")
	if ind.Country != "IN" || ind.RadiusKm != 150 || len(ind.Seasons) != 1 || ind.Seasons[0].Name != "monsoon" {
		t.Fatalf("indore defaults wrong: %+v", ind)
	}
	cbe, _ := s.Get("coimbatore")
	if len(cbe.Seasons) != 2 {
		t.Fatalf("coimbatore seasons must override defaults, got %d", len(cbe.Seasons))
	}
	if got := s.Countries(); len(got) != 2 || got[0] != "IN" || got[1] != "SE" {
		t.Fatalf("countries = %v", got)
	}
}

const sample = `
defaults:
  radius_km: 150
  corridor_buffer_km: 20
  timezone: Asia/Kolkata
  country: IN
  langs: [en, hi]
  seasons:
    - {name: monsoon, from: "06-01", to: "09-30", unpaved_penalty: 1.6, confidence_half_life_days: 7}
regions:
  - id: indore
    name: Indore
    center: [22.7196, 75.8577]
  - id: stockholm
    name: Stockholm
    country: SE
    timezone: Europe/Stockholm
    center: [59.3293, 18.0686]
    seasons:
      - {name: winter, from: "11-01", to: "03-15", unpaved_penalty: 1.8, confidence_half_life_days: 7}
      - {name: tjallossning, from: "03-15", to: "05-01", unpaved_penalty: 1.5, confidence_half_life_days: 10}
`

func TestActiveSeason(t *testing.T) {
	s, err := Parse([]byte(sample))
	if err != nil {
		t.Fatal(err)
	}
	ind, _ := s.Get("indore")
	ist, _ := time.LoadLocation("Asia/Kolkata")
	cases := []struct {
		at   time.Time
		want string
	}{
		{time.Date(2026, 7, 15, 12, 0, 0, 0, ist), "monsoon"},
		{time.Date(2026, 6, 1, 0, 30, 0, 0, ist), "monsoon"},
		{time.Date(2026, 9, 30, 23, 0, 0, 0, ist), "monsoon"},
		{time.Date(2026, 10, 1, 0, 0, 0, 0, ist), ""},
		// 31 May 19:00 UTC is already 1 June in India.
		{time.Date(2026, 5, 31, 19, 0, 0, 0, time.UTC), "monsoon"},
	}
	for _, c := range cases {
		got := ind.ActiveSeason(c.at)
		name := ""
		if got != nil {
			name = got.Name
		}
		if name != c.want {
			t.Errorf("%v: got %q want %q", c.at, name, c.want)
		}
	}
	if hl := ind.HalfLifeDays(time.Date(2026, 1, 1, 0, 0, 0, 0, ist)); hl != 30 {
		t.Fatalf("default half-life = %v", hl)
	}

	sth, _ := s.Get("stockholm")
	sto, _ := time.LoadLocation("Europe/Stockholm")
	if se := sth.ActiveSeason(time.Date(2027, 1, 10, 12, 0, 0, 0, sto)); se == nil || se.Name != "winter" {
		t.Fatalf("january should be winter, got %+v", se)
	}
	// Overlap day picks the most conservative season.
	if se := sth.ActiveSeason(time.Date(2027, 3, 15, 12, 0, 0, 0, sto)); se == nil || se.Name != "winter" {
		t.Fatalf("15 March should resolve to winter (higher penalty), got %+v", se)
	}
	if se := sth.ActiveSeason(time.Date(2027, 4, 2, 12, 0, 0, 0, sto)); se == nil || se.Name != "tjallossning" {
		t.Fatalf("April should be tjallossning, got %+v", se)
	}
	if se := sth.ActiveSeason(time.Date(2027, 7, 2, 12, 0, 0, 0, sto)); se != nil {
		t.Fatalf("July should have no season, got %+v", se)
	}
}

func TestParseRejectsLooseningSeason(t *testing.T) {
	bad := `
regions:
  - id: x
    name: X
    timezone: UTC
    country: IN
    center: [1, 1]
    seasons:
      - {name: dry, from: "01-01", to: "02-01", unpaved_penalty: 0.5}
`
	if _, err := Parse([]byte(bad)); err == nil {
		t.Fatal("penalty < 1 must be rejected")
	}
}
