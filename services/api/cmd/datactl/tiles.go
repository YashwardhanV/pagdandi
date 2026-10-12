package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"pagdandi/api/internal/geo"
)

// latestProtomapsBuild returns the URL of the newest daily Protomaps basemap build.
func latestProtomapsBuild(ctx context.Context) (string, error) {
	if v := os.Getenv("PROTOMAPS_BUILD_URL"); v != "" {
		return v, nil
	}
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, "https://build-metadata.protomaps.dev/builds.json", nil)
	resp, err := http.DefaultClient.Do(req)
	if err == nil {
		defer resp.Body.Close()
		var builds []struct {
			Key string `json:"key"`
		}
		if resp.StatusCode == 200 && json.NewDecoder(resp.Body).Decode(&builds) == nil && len(builds) > 0 {
			keys := make([]string, 0, len(builds))
			for _, b := range builds {
				if strings.HasSuffix(b.Key, ".pmtiles") {
					keys = append(keys, b.Key)
				}
			}
			sort.Strings(keys)
			if len(keys) > 0 {
				return "https://build.protomaps.com/" + keys[len(keys)-1], nil
			}
		}
	}
	// Fall back to probing recent dates.
	for d := 0; d < 10; d++ {
		u := "https://build.protomaps.com/" + time.Now().UTC().AddDate(0, 0, -d).Format("20060102") + ".pmtiles"
		hreq, _ := http.NewRequestWithContext(ctx, http.MethodHead, u, nil)
		if r, err := http.DefaultClient.Do(hreq); err == nil {
			r.Body.Close()
			if r.StatusCode == 200 {
				return u, nil
			}
		}
	}
	return "", fmt.Errorf("no recent Protomaps build found")
}

// cmdTiles extracts the country's basemap at z0–15 within coverage, then
// cuts a small offline pack (25 km around each city) per region (SPEC §10.3).
func cmdTiles(ctx context.Context, o *opts) error {
	if err := needCoverage(o); err != nil {
		return err
	}
	dir := filepath.Join(o.build, "tiles")
	_ = os.MkdirAll(filepath.Join(dir, "packs"), 0o755)
	out := filepath.Join(dir, o.country+".pmtiles")
	manifestPath := filepath.Join(dir, "manifest.json")
	manifest := map[string]any{}
	if b, err := os.ReadFile(manifestPath); err == nil {
		_ = json.Unmarshal(b, &manifest)
	}
	if o.force || !fresh(out, coveragePath(o)) {
		url, err := latestProtomapsBuild(ctx)
		if err != nil {
			return err
		}
		tmp := out + ".part"
		if err := sh(ctx, o, pmtilesBin(o), "extract", url, tmp, "--region="+coveragePath(o), fmt.Sprintf("--maxzoom=%d", o.maxzoom), "--download-threads=4"); err != nil {
			return err
		}
		_ = os.Remove(out)
		if err := os.Rename(tmp, out); err != nil {
			return err
		}
		manifest[o.country] = map[string]any{"file": o.country + ".pmtiles", "source": url, "built_at": time.Now().UTC().Format(time.RFC3339)}
	}
	sel, err := loadSelection(o)
	if err != nil {
		return err
	}
	packs, _ := manifest["packs"].(map[string]any)
	if packs == nil {
		packs = map[string]any{}
	}
	for _, id := range sel.Regions {
		reg, _ := o.regions.Get(id)
		if strings.ToLower(reg.Country) != o.country {
			continue
		}
		pack := filepath.Join(dir, "packs", id+".pmtiles")
		if !o.force && fresh(pack, out) {
			continue
		}
		c := reg.CenterPoint()
		n, e := geo.Destination(c, 0, 25000), geo.Destination(c, 90, 25000)
		s, w := geo.Destination(c, 180, 25000), geo.Destination(c, 270, 25000)
		bbox := fmt.Sprintf("--bbox=%.5f,%.5f,%.5f,%.5f", w.Lng, s.Lat, e.Lng, n.Lat)
		if err := sh(ctx, o, pmtilesBin(o), "extract", out, pack, bbox, fmt.Sprintf("--maxzoom=%d", o.maxzoom)); err != nil {
			return fmt.Errorf("offline pack %s: %w", id, err)
		}
		st, _ := os.Stat(pack)
		packs[id] = map[string]any{"file": "packs/" + id + ".pmtiles", "bytes": st.Size(), "radius_km": 25}
	}
	manifest["packs"] = packs
	return writeJSON(manifestPath, manifest)
}
