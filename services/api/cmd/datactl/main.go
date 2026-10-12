// Command datactl builds Pagdandi's data: coverage polygons, OSM extracts with
// regional overrides, military areas, routable-node index, community overlay,
// GraphHopper graphs, PMTiles basemaps and the Postgres seed (SPEC §5).
//
//	datactl coverage  [--region ID]          coverage_{in,se}.geojson, clusters.json
//	datactl download  --country in|se        Geofabrik extracts (with fallback mirror)
//	datactl extract   --country in|se        osmium extract to coverage
//	datactl overrides --country in|se        inject access rules (§4.3)
//	datactl areas     --country in|se        military polygons → GraphHopper areas
//	datactl nodes     --country in|se        routable nodes + barriers → Postgres
//	datactl overlay   --country in|se        baked community shortcuts → extract (§7.6)
//	datactl graph     --country in|se [--verify]  GraphHopper import, blue/green swap
//	datactl tiles     --country in|se        Protomaps PMTiles extract + offline packs
//	datactl seed      [--region ID]          regions, coverage, destinations → Postgres
//	datactl all       [--region ID]          everything above, in order
//	datactl fixture                          synthetic-town test graph (SPEC §14)
//	datactl migrate                          apply database migrations
//	datactl seed-dev                         demo shortcuts around Bilawali, Indore
//	datactl admin --email X [--role R]       grant moderator/admin
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"time"
	_ "time/tzdata"

	"pagdandi/api/internal/regions"
	"pagdandi/api/internal/store"
)

type opts struct {
	root    string
	build   string
	region  string
	country string
	force   bool
	verify  bool
	regions *regions.Set
	log     *slog.Logger
	dbURL   string
	javaXmx string
	maxzoom int
	// Fixture builds use an isolated build dir, a fake node country code,
	// tiny-network import settings and their own port.
	nodesCountry string
	minNetwork   int
	portOverride int
}

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}
	cmd := os.Args[1]
	args := os.Args[2:]
	var adminArgs []string
	if cmd == "admin" {
		adminArgs, args = args, nil
	}
	fs := flag.NewFlagSet(cmd, flag.ExitOnError)
	var o opts
	regionsFile := fs.String("regions", "", "regions.yaml path (default <root>/data/regions.yaml)")
	fs.StringVar(&o.root, "root", "", "repository root (auto-detected)")
	fs.StringVar(&o.region, "region", "", "build only the cluster containing this region id (or 'all')")
	fs.StringVar(&o.country, "country", "", "in or se")
	fs.BoolVar(&o.force, "force", false, "redo steps even if outputs are fresh")
	fs.BoolVar(&o.verify, "verify", false, "graph: health-check the new graph before swapping")
	fs.IntVar(&o.maxzoom, "maxzoom", 15, "tiles: max zoom")
	_ = fs.Parse(args)

	o.log = slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo}))
	if o.root == "" {
		o.root = findRoot()
	}
	o.build = filepath.Join(o.root, "data", "build")
	if o.region == "all" {
		o.region = ""
	}
	o.country = strings.ToLower(o.country)
	o.dbURL = envOr("DATABASE_URL", "postgres://pagdandi:pagdandi@localhost:5432/pagdandi?sslmode=disable")
	o.javaXmx = envOr("GH_JAVA_XMX", "3g")
	o.minNetwork = 200
	if *regionsFile == "" {
		*regionsFile = filepath.Join(o.root, "data", "regions.yaml")
	}
	set, err := regions.Load(*regionsFile)
	if err != nil {
		fatal(err)
	}
	o.regions = set
	if o.region != "" {
		if _, ok := set.Get(o.region); !ok {
			fatal(fmt.Errorf("unknown region %q", o.region))
		}
	}
	_ = os.MkdirAll(o.build, 0o755)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	start := time.Now()
	if cmd == "admin" {
		if err := cmdAdmin(ctx, &o, adminArgs); err != nil {
			fatal(err)
		}
		return
	}
	if err := run(ctx, cmd, &o); err != nil {
		fatal(err)
	}
	o.log.Info("done", "cmd", cmd, "took", time.Since(start).Round(time.Second))
}

func run(ctx context.Context, cmd string, o *opts) error {
	needCountry := func() error {
		if o.country != "in" && o.country != "se" {
			return errors.New("--country in|se is required")
		}
		return nil
	}
	switch cmd {
	case "coverage":
		return cmdCoverage(ctx, o)
	case "seed":
		return cmdSeed(ctx, o)
	case "all":
		return cmdAll(ctx, o)
	case "fixture":
		return cmdFixture(ctx, o)
	case "migrate":
		return cmdMigrate(ctx, o)
	case "seed-dev":
		return cmdSeedDev(ctx, o)
	case "download", "extract", "overrides", "areas", "nodes", "overlay", "graph", "tiles":
		if err := needCountry(); err != nil {
			return err
		}
		return countryStep(ctx, cmd, o)
	default:
		usage()
		return fmt.Errorf("unknown command %q", cmd)
	}
}

func countryStep(ctx context.Context, cmd string, o *opts) error {
	switch cmd {
	case "download":
		return cmdDownload(ctx, o)
	case "extract":
		return cmdExtract(ctx, o)
	case "overrides":
		return cmdOverrides(ctx, o)
	case "areas":
		return cmdAreas(ctx, o)
	case "nodes":
		return cmdNodes(ctx, o)
	case "overlay":
		return cmdOverlay(ctx, o)
	case "graph":
		return cmdGraph(ctx, o)
	case "tiles":
		return cmdTiles(ctx, o)
	}
	return nil
}

// cmdAll runs the whole pipeline for the selected regions.
func cmdAll(ctx context.Context, o *opts) error {
	if err := cmdCoverage(ctx, o); err != nil {
		return fmt.Errorf("coverage: %w", err)
	}
	sel, err := loadSelection(o)
	if err != nil {
		return err
	}
	for _, c := range sel.Countries {
		oc := *o
		oc.country = strings.ToLower(c)
		for _, step := range []string{"download", "extract", "overrides", "areas", "nodes", "overlay", "graph"} {
			o.log.Info("step", "country", oc.country, "step", step)
			if err := countryStep(ctx, step, &oc); err != nil {
				return fmt.Errorf("%s %s: %w", step, oc.country, err)
			}
		}
		// Tiles are a large download; failure falls back to hosted tiles in dev (SPEC §5).
		if err := cmdTiles(ctx, &oc); err != nil {
			o.log.Warn("tiles failed; the web app falls back to remote tiles (VITE_TILES_MODE=remote)", "country", oc.country, "err", err)
		}
	}
	return cmdSeed(ctx, o)
}

func openDB(ctx context.Context, o *opts) (*store.DB, error) {
	db, err := store.Open(ctx, o.dbURL)
	if err != nil {
		return nil, fmt.Errorf("postgres (run `make bootstrap` first): %w", err)
	}
	if err := db.Migrate(ctx); err != nil {
		db.Close()
		return nil, err
	}
	return db, nil
}

func findRoot() string {
	dir, _ := os.Getwd()
	for d := dir; ; d = filepath.Dir(d) {
		if _, err := os.Stat(filepath.Join(d, "data", "regions.yaml")); err == nil {
			return d
		}
		if filepath.Dir(d) == d {
			return dir
		}
	}
}

func envOr(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, "datactl:", err)
	os.Exit(1)
}

func usage() {
	fmt.Fprintln(os.Stderr, `usage: datactl <coverage|download|extract|overrides|areas|nodes|overlay|graph|tiles|seed|all|fixture|migrate|seed-dev|admin> [--region ID] [--country in|se] [--force]`)
}

// fresh reports whether out exists and is newer than every input.
func fresh(out string, inputs ...string) bool {
	st, err := os.Stat(out)
	if err != nil || st.Size() == 0 {
		return false
	}
	for _, in := range inputs {
		if is, err := os.Stat(in); err == nil && is.ModTime().After(st.ModTime()) {
			return false
		}
	}
	return true
}
