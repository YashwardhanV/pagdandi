package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"pagdandi/api/internal/geo"
)

// Ports per country graph (SPEC §13.2): gh-in :8989, gh-se :8990.
var ghPorts = map[string][2]int{"in": {8989, 9989}, "se": {8990, 9990}}

func ghJar(o *opts) string {
	if v := os.Getenv("GH_JAR"); v != "" {
		return v
	}
	return filepath.Join(o.root, "data", "build", "bin", "graphhopper-web-11.1.jar")
}

func slash(p string) string { return filepath.ToSlash(p) }

func renderConfig(o *opts, graphDir string, port, admin int, bind string) (string, error) {
	tpl, err := os.ReadFile(filepath.Join(o.root, "infra", "graphhopper", "config-"+o.country+".yml"))
	if err != nil {
		return "", err
	}
	r := strings.NewReplacer(
		"{{DATAREADER_FILE}}", slash(graphInputPath(o)),
		"{{GRAPH_LOCATION}}", slash(graphDir),
		"{{MODELS_DIR}}", slash(modelsDir(o)),
		"{{PORT}}", fmt.Sprint(port),
		"{{ADMIN_PORT}}", fmt.Sprint(admin),
		"{{BIND}}", bind,
		"{{MIN_NETWORK_SIZE}}", fmt.Sprint(o.minNetwork),
	)
	return r.Replace(string(tpl)), nil
}

func syncModels(o *opts) ([]string, error) {
	src := filepath.Join(o.root, "infra", "graphhopper", "custom_models")
	if err := os.MkdirAll(modelsDir(o), 0o755); err != nil {
		return nil, err
	}
	ents, err := os.ReadDir(src)
	if err != nil {
		return nil, err
	}
	var files []string
	for _, e := range ents {
		if !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		b, err := os.ReadFile(filepath.Join(src, e.Name()))
		if err != nil {
			return nil, err
		}
		dst := filepath.Join(modelsDir(o), e.Name())
		if old, err := os.ReadFile(dst); err != nil || string(old) != string(b) {
			if err := os.WriteFile(dst, b, 0o644); err != nil {
				return nil, err
			}
		}
		files = append(files, dst)
	}
	mil := filepath.Join(modelsDir(o), "military.json")
	if _, err := os.Stat(mil); err != nil {
		if err := os.WriteFile(mil, []byte(`{"priority": []}`+"\n"), 0o644); err != nil {
			return nil, err
		}
	}
	return append(files, mil), nil
}

// cmdGraph imports a new graph into a fresh directory and swaps the pointer
// only after the import (and, with --verify, a live health check) succeeds:
// blue/green, so the serving graph is never half-built (SPEC §5).
func cmdGraph(ctx context.Context, o *opts) error {
	in := graphInputPath(o)
	if _, err := os.Stat(in); err != nil {
		return fmt.Errorf("%s missing: run overlay first", in)
	}
	models, err := syncModels(o)
	if err != nil {
		return err
	}
	base := filepath.Join(o.build, "gh", o.country)
	pointer := filepath.Join(base, "current")
	tpl := filepath.Join(o.root, "infra", "graphhopper", "config-"+o.country+".yml")
	if cur, err := os.ReadFile(pointer); err == nil && !o.force {
		if _, err := os.Stat(filepath.Join(base, strings.TrimSpace(string(cur)), "properties")); err == nil && fresh(pointer, append(models, in, tpl)...) {
			o.log.Info("cached", "graph", strings.TrimSpace(string(cur)))
			return nil
		}
	}
	name := "graph-" + time.Now().Format("20060102-150405")
	dir := filepath.Join(base, name)
	ports := ghPorts[o.country]
	if o.portOverride > 0 {
		ports = [2]int{o.portOverride, o.portOverride + 1000}
	}
	cfg, err := renderConfig(o, dir, ports[0], ports[1], "localhost")
	if err != nil {
		return err
	}
	importCfg := filepath.Join(base, "config-import.yml")
	if err := os.WriteFile(importCfg, []byte(cfg), 0o644); err != nil {
		return err
	}
	logDir := filepath.Join(o.build, "logs")
	_ = os.MkdirAll(logDir, 0o755)
	logf, err := os.Create(filepath.Join(logDir, "gh-import-"+o.country+".log"))
	if err != nil {
		return err
	}
	defer logf.Close()
	o.log.Info("graphhopper import", "country", o.country, "dir", name, "xmx", o.javaXmx, "log", logf.Name())
	java := envOr("JAVA", "java")
	c := exec.CommandContext(ctx, java, "-Xmx"+o.javaXmx, "-Xms512m", "-XX:+UseParallelGC", "-jar", ghJar(o), "import", importCfg)
	c.Stdout, c.Stderr = io.MultiWriter(logf, os.Stdout), io.MultiWriter(logf, os.Stderr)
	if err := c.Run(); err != nil {
		_ = os.RemoveAll(dir)
		return fmt.Errorf("graphhopper import failed (see %s): %w", logf.Name(), err)
	}
	if o.verify {
		if err := verifyGraph(ctx, o, dir); err != nil {
			_ = os.RemoveAll(dir)
			return fmt.Errorf("new graph failed its health check, keeping the old one: %w", err)
		}
	}
	serveCfg, err := renderConfig(o, dir, ports[0], ports[1], envOr("GH_BIND", "localhost"))
	if err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(base, "config.yml"), []byte(serveCfg), 0o644); err != nil {
		return err
	}
	if err := os.WriteFile(pointer, []byte(name+"\n"), 0o644); err != nil {
		return err
	}
	pruneGraphs(o, base, name)
	o.log.Info("graph ready", "country", o.country, "graph", name, "serve", "scripts/gh-serve.sh "+o.country)
	return nil
}

// pruneGraphs keeps the current graph and the one before it (for rollback).
func pruneGraphs(o *opts, base, current string) {
	ents, _ := os.ReadDir(base)
	var dirs []string
	for _, e := range ents {
		if e.IsDir() && strings.HasPrefix(e.Name(), "graph-") && e.Name() != current {
			dirs = append(dirs, e.Name())
		}
	}
	sort.Strings(dirs)
	for i := 0; i < len(dirs)-1; i++ {
		if err := os.RemoveAll(filepath.Join(base, dirs[i])); err != nil {
			o.log.Warn("could not remove old graph (still in use?)", "dir", dirs[i], "err", err)
		}
	}
}

// verifyGraph serves the new graph on a spare port and routes centre → first destination.
func verifyGraph(ctx context.Context, o *opts, dir string) error {
	port, admin := 18989, 18990
	cfg, err := renderConfig(o, dir, port, admin, "localhost")
	if err != nil {
		return err
	}
	path := filepath.Join(filepath.Dir(dir), "config-verify.yml")
	if err := os.WriteFile(path, []byte(cfg), 0o644); err != nil {
		return err
	}
	vctx, cancel := context.WithTimeout(ctx, 10*time.Minute)
	defer cancel()
	c := exec.CommandContext(vctx, envOr("JAVA", "java"), "-Xmx"+envOr("GH_SERVE_XMX", "2g"), "-jar", ghJar(o), "server", path)
	if err := c.Start(); err != nil {
		return err
	}
	defer func() { _ = c.Process.Kill(); _ = c.Wait() }()
	url := fmt.Sprintf("http://localhost:%d", port)
	for i := 0; ; i++ {
		if i > 300 {
			return fmt.Errorf("graphhopper did not become healthy")
		}
		if resp, err := http.Get(url + "/health"); err == nil {
			resp.Body.Close()
			if resp.StatusCode == 200 {
				break
			}
		}
		time.Sleep(time.Second)
	}
	sel, err := loadSelection(o)
	if err != nil {
		return err
	}
	for _, id := range sel.Regions {
		reg, _ := o.regions.Get(id)
		if strings.ToLower(reg.Country) != o.country {
			continue
		}
		c := reg.CenterPoint()
		to := geo.Destination(c, 45, 3000)
		profile := "tw_standard"
		if o.country == "se" {
			profile = "bike_standard"
		}
		body := fmt.Sprintf(`{"points":[[%f,%f],[%f,%f]],"profile":"%s","points_encoded":true}`, c.Lng, c.Lat, to.Lng, to.Lat, profile)
		resp, err := http.Post(url+"/route", "application/json", strings.NewReader(body))
		if err != nil {
			return err
		}
		var out struct {
			Paths   []json.RawMessage `json:"paths"`
			Message string            `json:"message"`
		}
		_ = json.NewDecoder(resp.Body).Decode(&out)
		resp.Body.Close()
		if resp.StatusCode != 200 || len(out.Paths) == 0 {
			return fmt.Errorf("sample route in %s failed: %s %s", id, resp.Status, out.Message)
		}
		o.log.Info("verify ok", "region", id)
	}
	return nil
}
