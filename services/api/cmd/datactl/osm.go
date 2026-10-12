package main

import (
	"bufio"
	"context"
	"encoding/json"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"gopkg.in/yaml.v3"

	"pagdandi/api/internal/geo"
	"pagdandi/api/internal/store"
)

// tool finds an external binary: env override, the native tools dir, then PATH.
func tool(o *opts, env, rel, name string) string {
	if v := os.Getenv(env); v != "" {
		return v
	}
	p := filepath.Join(o.root, "data", "build", "tools", rel)
	if runtime.GOOS == "windows" && !strings.HasSuffix(p, ".exe") && !strings.HasSuffix(p, ".jar") {
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
func pythonBin(o *opts) string  { return tool(o, "OSM_PY", "osmenv/python", "python3") }
func pmtilesBin(o *opts) string { return tool(o, "PMTILES", "pmtiles/pmtiles", "pmtiles") }

func sh(ctx context.Context, o *opts, name string, args ...string) error {
	o.log.Info("exec", "cmd", filepath.Base(name)+" "+strings.Join(args, " "))
	c := exec.CommandContext(ctx, name, args...)
	c.Stdout, c.Stderr = os.Stdout, os.Stderr
	return c.Run()
}

func osmDir(o *opts) string { return filepath.Join(o.build, "osm") }

func coveragePath(o *opts) string {
	return filepath.Join(o.build, "coverage_"+o.country+".geojson")
}

func extractPath(o *opts) string { return filepath.Join(osmDir(o), "coverage_"+o.country+".osm.pbf") }
func overridePath(o *opts) string {
	return filepath.Join(osmDir(o), "coverage_"+o.country+".ovr.osm.pbf")
}
func graphInputPath(o *opts) string {
	return filepath.Join(osmDir(o), "coverage_"+o.country+".graph.osm.pbf")
}

func needCoverage(o *opts) error {
	if _, err := os.Stat(coveragePath(o)); err != nil {
		return fmt.Errorf("%s missing: run `datactl coverage` (does the selection include country %s?)", coveragePath(o), strings.ToUpper(o.country))
	}
	return nil
}

// --- download -------------------------------------------------------------------

type source struct {
	Name string `json:"name"`
	URL  string `json:"url"`
	Path string `json:"path"`
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
		last = fetch(ctx, url, dst)
		if last == nil {
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
	req.Header.Set("User-Agent", "Pagdandi-datactl/0.1")
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

// indiaZones picks the Geofabrik India sub-extracts whose boundary intersects coverage.
func indiaZones(ctx context.Context, o *opts) ([]source, error) {
	idx := filepath.Join(o.build, "geofabrik-index-v1.json")
	if err := download(ctx, o, "https://download.geofabrik.de/index-v1.json", idx); err != nil {
		return nil, err
	}
	b, err := os.ReadFile(idx)
	if err != nil {
		return nil, err
	}
	var fc struct {
		Features []struct {
			Properties struct {
				ID     string            `json:"id"`
				Parent string            `json:"parent"`
				URLs   map[string]string `json:"urls"`
			} `json:"properties"`
			Geometry json.RawMessage `json:"geometry"`
		} `json:"features"`
	}
	if err := json.Unmarshal(b, &fc); err != nil {
		return nil, err
	}
	covB, err := os.ReadFile(coveragePath(o))
	if err != nil {
		return nil, err
	}
	var cov struct {
		Geometry json.RawMessage `json:"geometry"`
	}
	if err := json.Unmarshal(covB, &cov); err != nil {
		return nil, err
	}
	db, err := openDB(ctx, o)
	if err != nil {
		return nil, err
	}
	defer db.Close()
	var out []source
	for _, f := range fc.Features {
		if f.Properties.Parent != "india" || f.Properties.URLs["pbf"] == "" {
			continue
		}
		var hit bool
		if err := db.Pool.QueryRow(ctx, `select ST_Intersects(ST_MakeValid(ST_GeomFromGeoJSON($1)), ST_GeomFromGeoJSON($2))`,
			string(f.Geometry), string(cov.Geometry)).Scan(&hit); err != nil {
			return nil, err
		}
		if hit {
			out = append(out, source{Name: f.Properties.ID, URL: f.Properties.URLs["pbf"]})
		}
	}
	if len(out) == 0 {
		out = []source{{Name: "india", URL: "https://download.geofabrik.de/asia/india-latest.osm.pbf"}}
	}
	return out, nil
}

func cmdDownload(ctx context.Context, o *opts) error {
	if err := needCoverage(o); err != nil {
		return err
	}
	var srcs []source
	var fallback source
	if o.country == "in" {
		zs, err := indiaZones(ctx, o)
		if err != nil {
			o.log.Warn("zone selection failed; using whole India", "err", err)
			zs = []source{{Name: "india", URL: "https://download.geofabrik.de/asia/india-latest.osm.pbf"}}
		}
		srcs = zs
		fallback = source{Name: "india", URL: "http://download.openstreetmap.fr/extracts/asia/india-latest.osm.pbf"}
	} else {
		srcs = []source{{Name: "sweden", URL: "https://download.geofabrik.de/europe/sweden-latest.osm.pbf"}}
		fallback = source{Name: "sweden", URL: "http://download.openstreetmap.fr/extracts/europe/sweden-latest.osm.pbf"}
	}
	var got []source
	for _, s := range srcs {
		s.Path = filepath.Join(osmDir(o), "src", s.Name+"-latest.osm.pbf")
		if err := download(ctx, o, s.URL, s.Path); err != nil {
			o.log.Warn("geofabrik download failed; trying openstreetmap.fr", "src", s.Name, "err", err)
			fb := fallback
			fb.Path = filepath.Join(osmDir(o), "src", "fallback-"+fb.Name+"-latest.osm.pbf")
			if err := download(ctx, o, fb.URL, fb.Path); err != nil {
				return fmt.Errorf("all mirrors failed: %w", err)
			}
			got = []source{fb}
			break
		}
		got = append(got, s)
	}
	o.log.Info("sources", "country", o.country, "files", len(got))
	return writeJSON(filepath.Join(osmDir(o), "sources_"+o.country+".json"), got)
}

func loadSources(o *opts) ([]source, error) {
	var s []source
	b, err := os.ReadFile(filepath.Join(osmDir(o), "sources_"+o.country+".json"))
	if err != nil {
		return nil, fmt.Errorf("run `datactl download --country %s` first: %w", o.country, err)
	}
	return s, json.Unmarshal(b, &s)
}

// --- extract --------------------------------------------------------------------

func cmdExtract(ctx context.Context, o *opts) error {
	if err := needCoverage(o); err != nil {
		return err
	}
	srcs, err := loadSources(o)
	if err != nil {
		return err
	}
	inputs := []string{coveragePath(o)}
	for _, s := range srcs {
		inputs = append(inputs, s.Path)
	}
	out := extractPath(o)
	if !o.force && fresh(out, inputs...) {
		o.log.Info("cached", "file", filepath.Base(out))
		return nil
	}
	tmp := filepath.Join(osmDir(o), "tmp")
	_ = os.MkdirAll(tmp, 0o755)
	var cuts []string
	for _, s := range srcs {
		cut := filepath.Join(tmp, s.Name+".cut.osm.pbf")
		if err := sh(ctx, o, osmiumBin(o), "extract", "-p", coveragePath(o), "-s", "complete_ways", "--overwrite", "-o", cut, s.Path); err != nil {
			return err
		}
		cuts = append(cuts, cut)
	}
	if len(cuts) == 1 {
		_ = os.Remove(out)
		return os.Rename(cuts[0], out)
	}
	args := append([]string{"merge", "--overwrite", "-o", out}, cuts...)
	if err := sh(ctx, o, osmiumBin(o), args...); err != nil {
		return err
	}
	for _, c := range cuts {
		_ = os.Remove(c)
	}
	return nil
}

// --- overrides ------------------------------------------------------------------

type restriction struct {
	ID           string     `yaml:"id" json:"id"`
	Name         string     `yaml:"name" json:"name"`
	NamePatterns []string   `yaml:"name_patterns" json:"name_patterns"`
	Highways     []string   `yaml:"highways" json:"highways"`
	BBox         [4]float64 `yaml:"bbox" json:"bbox"`
	Verify       bool       `yaml:"verify" json:"verify"`
}

func cmdOverrides(ctx context.Context, o *opts) error {
	in, out := extractPath(o), overridePath(o)
	rulesYAML := filepath.Join(o.root, "data", "overrides", "in_2w_restrictions.yaml")
	if !o.force && fresh(out, in, rulesYAML, filepath.Join(o.root, "infra", "tools", "osm_overrides.py")) {
		o.log.Info("cached", "file", filepath.Base(out))
		return nil
	}
	args := []string{filepath.Join(o.root, "infra", "tools", "osm_overrides.py"), "--country", o.country, "--in", in, "--out", out,
		"--report", filepath.Join(osmDir(o), "overrides_"+o.country+".report.json")}
	if o.country == "in" {
		b, err := os.ReadFile(rulesYAML)
		if err != nil {
			return err
		}
		var f struct {
			Restrictions []restriction `yaml:"restrictions"`
		}
		if err := yaml.Unmarshal(b, &f); err != nil {
			return err
		}
		rules := filepath.Join(osmDir(o), "tmp", "rules_in.json")
		if err := writeJSON(rules, f.Restrictions); err != nil {
			return err
		}
		args = append(args, "--rules", rules)
	}
	return sh(ctx, o, pythonBin(o), args...)
}

// --- areas ----------------------------------------------------------------------

func modelsDir(o *opts) string { return filepath.Join(o.build, "gh", o.country, "models") }

// cmdAreas turns military polygons into a GraphHopper custom-model file with
// areas and a priority block (SPEC §4.3, §6.2). Polygons are unioned, buffered
// 15 m outward (conservative) and simplified so the per-edge area test stays cheap.
func cmdAreas(ctx context.Context, o *opts) error {
	in := overridePath(o)
	out := filepath.Join(modelsDir(o), "military.json")
	if !o.force && fresh(out, in) {
		o.log.Info("cached", "file", "military.json")
		return nil
	}
	tmp := filepath.Join(osmDir(o), "tmp")
	_ = os.MkdirAll(tmp, 0o755)
	mil := filepath.Join(tmp, "military_"+o.country+".osm.pbf")
	gj := filepath.Join(tmp, "military_"+o.country+".geojson")
	if err := sh(ctx, o, osmiumBin(o), "tags-filter", "--overwrite", "-o", mil, in, "nwr/landuse=military", "nwr/military"); err != nil {
		return err
	}
	if err := sh(ctx, o, osmiumBin(o), "export", "--overwrite", "-f", "geojson", "--geometry-types=polygon", "-o", gj, mil); err != nil {
		return err
	}
	b, err := os.ReadFile(gj)
	if err != nil {
		return err
	}
	var fc struct {
		Features []struct {
			Geometry json.RawMessage `json:"geometry"`
		} `json:"features"`
	}
	if err := json.Unmarshal(b, &fc); err != nil {
		return err
	}
	var geoms []string
	for _, f := range fc.Features {
		geoms = append(geoms, string(f.Geometry))
	}
	model := map[string]any{"priority": []any{}}
	if len(geoms) > 0 {
		db, err := openDB(ctx, o)
		if err != nil {
			return err
		}
		defer db.Close()
		rows, err := db.Pool.Query(ctx, `
with u as (
  select ST_Union(ST_MakeValid(ST_SetSRID(ST_GeomFromGeoJSON(g), 4326))) g from unnest($1::text[]) g
), b as (
  select (ST_Dump(ST_SimplifyPreserveTopology(ST_Buffer(u.g::geography, 15)::geometry, 0.00005))).geom g from u
)
select ST_AsGeoJSON(ST_MakePolygon(ST_ExteriorRing(g)), 6), ST_Area(g::geography) a
from b where GeometryType(g) = 'POLYGON' order by a desc limit 400`, geoms)
		if err != nil {
			return err
		}
		var feats []map[string]any
		var conds []string
		for rows.Next() {
			var g string
			var area float64
			if err := rows.Scan(&g, &area); err != nil {
				rows.Close()
				return err
			}
			id := fmt.Sprintf("military_%d", len(feats))
			feats = append(feats, map[string]any{"type": "Feature", "id": id, "properties": map[string]any{}, "geometry": json.RawMessage(g)})
			conds = append(conds, "in_"+id)
		}
		rows.Close()
		if len(feats) > 0 {
			model = map[string]any{
				"priority": []map[string]string{{"if": strings.Join(conds, " || "), "multiply_by": "0"}},
				"areas":    map[string]any{"type": "FeatureCollection", "features": feats},
			}
		}
		o.log.Info("military areas", "osm_polygons", len(geoms), "merged_areas", len(feats))
	}
	return writeJSON(out, model)
}

// --- nodes ----------------------------------------------------------------------

func cmdNodes(ctx context.Context, o *opts) error {
	in := overridePath(o)
	tmp := filepath.Join(osmDir(o), "tmp")
	_ = os.MkdirAll(tmp, 0o755)
	nodesCSV := filepath.Join(tmp, "nodes_"+o.country+".csv")
	barCSV := filepath.Join(tmp, "barriers_"+o.country+".csv")
	stamp := filepath.Join(tmp, "nodes_"+o.country+".loaded")
	if !o.force && fresh(stamp, in) {
		o.log.Info("cached", "step", "nodes")
		return nil
	}
	if err := sh(ctx, o, pythonBin(o), filepath.Join(o.root, "infra", "tools", "osm_nodes.py"),
		"--country", o.country, "--in", in, "--nodes", nodesCSV, "--barriers", barCSV); err != nil {
		return err
	}
	db, err := openDB(ctx, o)
	if err != nil {
		return err
	}
	defer db.Close()
	cc := strings.ToUpper(o.country)
	if o.nodesCountry != "" {
		cc = o.nodesCountry
	}
	n, err := loadCSV(ctx, db, nodesCSV, "osm_nodes", cc, 3)
	if err != nil {
		return fmt.Errorf("load nodes: %w", err)
	}
	b, err := loadCSV(ctx, db, barCSV, "osm_barriers", cc, 5)
	if err != nil {
		return fmt.Errorf("load barriers: %w", err)
	}
	o.log.Info("loaded", "nodes", n, "barriers", b)
	return os.WriteFile(stamp, []byte(time.Now().Format(time.RFC3339)), 0o644)
}

func loadCSV(ctx context.Context, db *store.DB, path, table, country string, cols int) (int64, error) {
	f, err := os.Open(path)
	if err != nil {
		return 0, err
	}
	defer f.Close()
	tx, err := db.Pool.Begin(ctx)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback(ctx)
	if _, err := tx.Exec(ctx, `create temp table stage (id bigint, kind text, access text, lon float8, lat float8) on commit drop`); err != nil {
		return 0, err
	}
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1<<20), 1<<20)
	var parseErr error
	src := pgx.CopyFromFunc(func() ([]any, error) {
		for sc.Scan() {
			p := strings.Split(sc.Text(), ",")
			if len(p) != cols {
				continue
			}
			id, err := strconv.ParseInt(p[0], 10, 64)
			if err != nil {
				continue
			}
			if cols == 3 {
				lon, _ := strconv.ParseFloat(p[1], 64)
				lat, _ := strconv.ParseFloat(p[2], 64)
				return []any{id, nil, nil, lon, lat}, nil
			}
			lon, _ := strconv.ParseFloat(p[3], 64)
			lat, _ := strconv.ParseFloat(p[4], 64)
			var access any
			if p[2] != "" {
				access = p[2]
			}
			return []any{id, p[1], access, lon, lat}, nil
		}
		parseErr = sc.Err()
		return nil, nil
	})
	if _, err := tx.CopyFrom(ctx, pgx.Identifier{"stage"}, []string{"id", "kind", "access", "lon", "lat"}, src); err != nil {
		return 0, err
	}
	if parseErr != nil {
		return 0, parseErr
	}
	if _, err := tx.Exec(ctx, `delete from `+table+` where country = $1`, country); err != nil {
		return 0, err
	}
	var q string
	if table == "osm_nodes" {
		q = `insert into osm_nodes (id, country, geom) select distinct on (id) id, $1, ST_SetSRID(ST_MakePoint(lon, lat), 4326) from stage on conflict (id) do nothing`
	} else {
		q = `insert into osm_barriers (id, country, kind, access, geom) select distinct on (id) id, $1, kind, access, ST_SetSRID(ST_MakePoint(lon, lat), 4326) from stage on conflict (id) do nothing`
	}
	ct, err := tx.Exec(ctx, q, country)
	if err != nil {
		return 0, err
	}
	if err := tx.Commit(ctx); err != nil {
		return 0, err
	}
	_, _ = db.Pool.Exec(ctx, `analyze `+table)
	return ct.RowsAffected(), nil
}

// --- overlay --------------------------------------------------------------------

type xmlTag struct {
	K string `xml:"k,attr"`
	V string `xml:"v,attr"`
}
type xmlNode struct {
	XMLName xml.Name `xml:"node"`
	ID      int64    `xml:"id,attr"`
	Version int      `xml:"version,attr"`
	Lat     string   `xml:"lat,attr"`
	Lon     string   `xml:"lon,attr"`
}
type xmlNd struct {
	Ref int64 `xml:"ref,attr"`
}
type xmlWay struct {
	XMLName xml.Name `xml:"way"`
	ID      int64    `xml:"id,attr"`
	Version int      `xml:"version,attr"`
	Nds     []xmlNd  `xml:"nd"`
	Tags    []xmlTag `xml:"tag"`
}

// cmdOverlay bakes trusted community shortcuts into the extract as OSM ways with
// negative ids that reuse the snapped endpoint node ids (SPEC §7.6). Nothing is
// uploaded to OpenStreetMap.
func cmdOverlay(ctx context.Context, o *opts) error {
	in, out := overridePath(o), graphInputPath(o)
	db, err := openDB(ctx, o)
	if err != nil {
		return err
	}
	defer db.Close()
	list, err := db.BakeCandidates(ctx, strings.ToUpper(o.country))
	if err != nil {
		return err
	}
	if len(list) == 0 {
		o.log.Info("overlay: no shortcuts eligible for baking")
		return copyFile(in, out)
	}
	var nodes []xmlNode
	var ways []xmlWay
	nextNode := int64(-1)
	for i, s := range list {
		w := xmlWay{ID: -int64(i + 1), Version: 1}
		w.Nds = append(w.Nds, xmlNd{Ref: s.StartNode})
		for _, p := range s.Line[1 : len(s.Line)-1] {
			nodes = append(nodes, xmlNode{ID: nextNode, Version: 1, Lat: fmt.Sprintf("%.7f", p.Lat), Lon: fmt.Sprintf("%.7f", p.Lng)})
			w.Nds = append(w.Nds, xmlNd{Ref: nextNode})
			nextNode--
		}
		w.Nds = append(w.Nds, xmlNd{Ref: s.EndNode})
		hw := "track"
		if s.Surface == "paved" {
			hw = "unclassified"
		}
		w.Tags = []xmlTag{{"highway", hw}, {"pagdandi:shortcut", "yes"}, {"pagdandi:id", s.ID}}
		if s.Surface == "unpaved" {
			w.Tags = append(w.Tags, xmlTag{"surface", "unpaved"})
		}
		if s.Oneway {
			w.Tags = append(w.Tags, xmlTag{"oneway", "yes"})
		}
		if s.Title != nil {
			w.Tags = append(w.Tags, xmlTag{"name", *s.Title})
		}
		ways = append(ways, w)
	}
	tmp := filepath.Join(osmDir(o), "tmp")
	_ = os.MkdirAll(tmp, 0o755)
	osmFile := filepath.Join(tmp, "overlay_"+o.country+".osm")
	f, err := os.Create(osmFile)
	if err != nil {
		return err
	}
	fmt.Fprint(f, xml.Header+`<osm version="0.6" generator="pagdandi-datactl">`+"\n")
	enc := xml.NewEncoder(f)
	enc.Indent("", " ")
	for _, n := range nodes {
		if err := enc.Encode(n); err != nil {
			return err
		}
	}
	for _, w := range ways {
		if err := enc.Encode(w); err != nil {
			return err
		}
	}
	fmt.Fprint(f, "\n</osm>\n")
	if err := f.Close(); err != nil {
		return err
	}
	sorted := filepath.Join(tmp, "overlay_"+o.country+".sorted.osm.pbf")
	if err := sh(ctx, o, osmiumBin(o), "sort", "--overwrite", "-o", sorted, osmFile); err != nil {
		return err
	}
	o.log.Info("overlay", "shortcuts", len(list))
	return sh(ctx, o, osmiumBin(o), "merge", "--overwrite", "-o", out, in, sorted)
}

func copyFile(src, dst string) error {
	if fresh(dst, src) {
		return nil
	}
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	tmp := dst + ".tmp"
	out, err := os.Create(tmp)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	if err := out.Close(); err != nil {
		return err
	}
	return os.Rename(tmp, dst)
}

var _ = errors.New
var _ = geo.Point{}
