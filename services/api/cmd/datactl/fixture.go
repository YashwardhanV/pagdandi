package main

import (
	"context"
	"os"
	"path/filepath"
)

// cmdFixture builds the synthetic-town graph through the real pipeline steps
// (overrides, military areas, node/barrier index, GraphHopper import) into
// data/build/fixture, for the deterministic profile tests of SPEC §14.
// Point DATABASE_URL at the test database: nodes load under country code ZZ.
func cmdFixture(ctx context.Context, o *opts) error {
	o.build = filepath.Join(o.root, "data", "build", "fixture")
	o.country = "in"
	o.nodesCountry = "ZZ"
	o.minNetwork = 1
	o.portOverride = 8999
	o.force = true
	if err := os.MkdirAll(osmDir(o), 0o755); err != nil {
		return err
	}
	src := filepath.Join(o.root, "tests", "fixtures", "synthetic-town.osm")
	if err := sh(ctx, o, osmiumBin(o), "sort", "--overwrite", "-o", extractPath(o), src); err != nil {
		return err
	}
	for _, step := range []func(context.Context, *opts) error{cmdOverrides, cmdAreas, cmdNodes} {
		if err := step(ctx, o); err != nil {
			return err
		}
	}
	if err := copyFile(overridePath(o), graphInputPath(o)); err != nil {
		return err
	}
	return cmdGraph(ctx, o)
}
