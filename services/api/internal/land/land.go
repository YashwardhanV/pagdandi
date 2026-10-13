// Package land loads the feature build (land grid, gems, anchors, waters,
// fresh roads) into memory with spatial indexes for the planners.
package land

import (
	"fmt"
	"log/slog"
	"path/filepath"
	"strings"

	"pagdandi/api/internal/features"
	"pagdandi/api/internal/geo"
	"pagdandi/api/internal/raster"
)

// Land is everything the planners know about the ground.
type Land struct {
	Grid    *raster.Grid
	Gems    *features.Bucket[features.Gem]
	Anchors *features.Bucket[features.Anchor]
	Waters  []features.Water
	Fresh   *features.Bucket[features.Fresh]
	FreshBy map[int64]features.Fresh // by OSM way id
	Meta    features.Meta
}

// Load reads a features directory. Missing optional layers load empty.
func Load(dir string, log *slog.Logger) (*Land, error) {
	l := &Land{FreshBy: map[int64]features.Fresh{}}
	g, err := raster.Read(filepath.Join(dir, features.RasterFile))
	if err != nil {
		return nil, fmt.Errorf("land grid (run `make data`): %w", err)
	}
	l.Grid = g
	var gems []features.Gem
	var anchors []features.Anchor
	var fresh []features.Fresh
	for name, v := range map[string]any{features.GemsFile: &gems, features.AnchorsFile: &anchors, features.WatersFile: &l.Waters, features.FreshFile: &fresh, features.MetaFile: &l.Meta} {
		if err := features.ReadJSON(dir, name, v); err != nil {
			log.Warn("feature layer missing", "file", name, "err", err)
		}
	}
	l.Gems = features.NewBucket(gems, 0.05, features.Gem.Point)
	l.Anchors = features.NewBucket(anchors, 0.02, features.Anchor.Point)
	l.Fresh = features.NewBucket(fresh, 0.02, features.Fresh.Mid)
	for _, f := range fresh {
		l.FreshBy[f.ID] = f
	}
	log.Info("land loaded", "grid", fmt.Sprintf("%dx%d", g.W, g.H), "gems", len(gems), "anchors", len(anchors), "waters", len(l.Waters), "fresh", len(fresh))
	return l, nil
}

// WaterNear returns the name of the closest named water body anchor within radiusM.
func (l *Land) WaterNear(p geo.Point, radiusM float64) string {
	for _, a := range l.Anchors.Near(p, radiusM) {
		if (a.Kind == "water" || a.Kind == "river") && a.Name != "" {
			return a.Name
		}
	}
	for _, w := range l.Waters {
		if w.BBox.Contains(p) && !strings.HasPrefix(w.Name, "Unnamed") {
			return w.Name
		}
	}
	return ""
}

// NamedNear returns the name of the closest anchor of one of kinds within radiusM.
func (l *Land) NamedNear(p geo.Point, radiusM float64, kinds ...string) (features.Anchor, bool) {
	for _, a := range l.Anchors.Near(p, radiusM) {
		if a.Name == "" {
			continue
		}
		for _, k := range kinds {
			if a.Kind == k {
				return a, true
			}
		}
	}
	return features.Anchor{}, false
}
