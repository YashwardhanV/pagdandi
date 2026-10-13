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
)

// latestProtomapsBuild returns the URL of the newest daily Protomaps basemap build.
func latestProtomapsBuild(ctx context.Context) (string, error) {
	if v := os.Getenv("PROTOMAPS_BUILD_URL"); v != "" {
		return v, nil
	}
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, "https://build-metadata.protomaps.dev/builds.json", nil)
	if resp, err := http.DefaultClient.Do(req); err == nil {
		defer resp.Body.Close()
		var builds []struct {
			Key string `json:"key"`
		}
		if resp.StatusCode == 200 && json.NewDecoder(resp.Body).Decode(&builds) == nil {
			var keys []string
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

// cmdTiles extracts the basemap (z0–15) for the coverage circle.
func cmdTiles(ctx context.Context, o *opts) error {
	dir := filepath.Join(o.build, "tiles")
	_ = os.MkdirAll(dir, 0o755)
	out := filepath.Join(dir, o.reg.ID+".pmtiles")
	if !o.force && fresh(out, coveragePath(o)) {
		o.log.Info("cached", "file", filepath.Base(out))
		return nil
	}
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
	return writeJSON(filepath.Join(dir, "manifest.json"), map[string]any{
		"file": o.reg.ID + ".pmtiles", "source": url, "built_at": time.Now().UTC().Format(time.RFC3339),
	})
}
