// Command datactl builds everything Pagdandi serves for the region in
// data/region.yaml: the routing graph, map tiles and feature layers.
//
//	datactl coverage   coverage circle → data/build/coverage.geojson
//	datactl download   Geofabrik extract(s) (mirror fallback, reused for 7 days)
//	datactl extract    osmium extract to the coverage circle
//	datactl areas      military areas → GraphHopper block list
//	datactl features   scenic land grid, gems, anchors, water bodies, fresh roads
//	datactl graph      GraphHopper import (car, moto, cycle, walk; elevation)
//	datactl tiles      Protomaps basemap extract for the circle
//	datactl all        everything above, in order
//
// Every step skips work whose outputs are newer than its inputs; --force redoes it.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime"
	"strings"
	"time"
	_ "time/tzdata"

	"pagdandi/api/internal/region"
)

type opts struct {
	root    string
	build   string
	force   bool
	reg     *region.Region
	log     *slog.Logger
	javaXmx string
	maxzoom int
	// elevation can be switched off when the elevation tile host is unreachable.
	elevation bool
}

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}
	cmd := os.Args[1]
	fs := flag.NewFlagSet(cmd, flag.ExitOnError)
	var o opts
	regionFile := fs.String("region", "", "region.yaml path (default <root>/data/region.yaml)")
	fs.StringVar(&o.root, "root", "", "repository root (auto-detected)")
	fs.BoolVar(&o.force, "force", false, "redo steps even if outputs are fresh")
	fs.IntVar(&o.maxzoom, "maxzoom", 15, "tiles: max zoom")
	fs.BoolVar(&o.elevation, "elevation", envOr("GH_ELEVATION", "true") != "false", "graph: import elevation (needs internet on first import)")
	_ = fs.Parse(os.Args[2:])

	o.log = slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo}))
	if o.root == "" {
		o.root = findRoot()
	}
	o.build = filepath.Join(o.root, "data", "build")
	o.javaXmx = envOr("GH_JAVA_XMX", "3g")
	if *regionFile == "" {
		*regionFile = filepath.Join(o.root, "data", "region.yaml")
	}
	reg, err := region.Load(*regionFile)
	if err != nil {
		fatal(err)
	}
	o.reg = reg
	_ = os.MkdirAll(o.build, 0o755)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	start := time.Now()
	if err := run(ctx, cmd, &o); err != nil {
		fatal(err)
	}
	o.log.Info("done", "cmd", cmd, "took", time.Since(start).Round(time.Second))
}

var steps = []string{"coverage", "download", "extract", "areas", "features", "graph", "tiles"}

func run(ctx context.Context, cmd string, o *opts) error {
	switch cmd {
	case "coverage":
		return cmdCoverage(o)
	case "download":
		return cmdDownload(ctx, o)
	case "extract":
		return cmdExtract(ctx, o)
	case "areas":
		return cmdAreas(ctx, o)
	case "features":
		return cmdFeatures(ctx, o)
	case "graph":
		return cmdGraph(ctx, o)
	case "tiles":
		return cmdTiles(ctx, o)
	case "all":
		for _, s := range steps {
			o.log.Info("step", "name", s)
			if err := run(ctx, s, o); err != nil {
				if s == "tiles" {
					// Tiles are a big download; the web app can fall back to hosted tiles.
					o.log.Warn("tiles failed; set VITE_TILES=remote to use hosted tiles", "err", err)
					continue
				}
				return fmt.Errorf("%s: %w", s, err)
			}
		}
		return nil
	default:
		usage()
		return fmt.Errorf("unknown command %q", cmd)
	}
}

// --- helpers --------------------------------------------------------------------

// tool finds an external binary: env override, the native tools dir, then PATH.
func tool(o *opts, env, rel, name string) string {
	if v := os.Getenv(env); v != "" {
		return v
	}
	p := filepath.Join(o.build, "tools", rel)
	if runtime.GOOS == "windows" {
		if _, err := os.Stat(p + ".exe"); err == nil {
			return p + ".exe"
		}
	}
	if _, err := os.Stat(p); err == nil {
		return p
	}
	return name
}

func osmiumBin(o *opts) string  { return tool(o, "OSMIUM", "osmenv/Library/bin/osmium", "osmium") }
func pmtilesBin(o *opts) string { return tool(o, "PMTILES", "pmtiles/pmtiles", "pmtiles") }

func sh(ctx context.Context, o *opts, name string, args ...string) error {
	o.log.Info("exec", "cmd", filepath.Base(name)+" "+strings.Join(args, " "))
	c := exec.CommandContext(ctx, name, args...)
	c.Stdout, c.Stderr = os.Stdout, os.Stderr
	return c.Run()
}

func download(ctx context.Context, o *opts, url, dst string) error {
	if st, err := os.Stat(dst); err == nil && !o.force && time.Since(st.ModTime()) < 7*24*time.Hour && st.Size() > 0 {
		o.log.Info("cached", "file", filepath.Base(dst))
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	var last error
	for attempt, backoff := 0, 2*time.Second; attempt < 3; attempt, backoff = attempt+1, backoff*4 {
		if attempt > 0 {
			o.log.Warn("retrying download", "url", url, "in", backoff, "err", last)
			time.Sleep(backoff)
		}
		o.log.Info("download", "url", url)
		if last = fetch(ctx, url, dst); last == nil {
			return nil
		}
	}
	return last
}

func fetch(ctx context.Context, url, dst string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", "Pagdandi-datactl/2.0")
	resp, err := (&http.Client{Timeout: 3 * time.Hour}).Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return fmt.Errorf("%s: %s", url, resp.Status)
	}
	tmp := dst + ".part"
	f, err := os.Create(tmp)
	if err != nil {
		return err
	}
	n, err := io.Copy(f, resp.Body)
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		return err
	}
	if resp.ContentLength > 0 && n != resp.ContentLength {
		return fmt.Errorf("short download: %d of %d bytes", n, resp.ContentLength)
	}
	return os.Rename(tmp, dst)
}

func writeJSON(path string, v any) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	b, err := json.MarshalIndent(v, "", " ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, b, 0o644)
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

func findRoot() string {
	dir, _ := os.Getwd()
	for d := dir; ; d = filepath.Dir(d) {
		if _, err := os.Stat(filepath.Join(d, "data", "region.yaml")); err == nil {
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
	fmt.Fprintln(os.Stderr, "usage: datactl <"+strings.Join(steps, "|")+"|all> [--force] [--elevation=false]")
}
