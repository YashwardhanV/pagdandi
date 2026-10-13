// Command api serves Pagdandi: trails (Go), discovery (Wander), search, map
// tiles and, in production, the web app itself.
package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"
	_ "time/tzdata"

	"pagdandi/api/internal/gh"
	"pagdandi/api/internal/land"
	"pagdandi/api/internal/plan"
	"pagdandi/api/internal/region"
	"pagdandi/api/internal/search"
	"pagdandi/api/internal/server"
	"pagdandi/api/internal/water"
)

func main() {
	log := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo}))
	if os.Getenv("APP_ENV") == "prod" {
		log = slog.New(slog.NewJSONHandler(os.Stderr, nil))
	}
	root := findRoot()
	dataDir := envOr("DATA_DIR", filepath.Join(root, "data", "build"))
	reg, err := region.Load(envOr("REGION_FILE", filepath.Join(root, "data", "region.yaml")))
	if err != nil {
		fatal(log, err)
	}
	ld, err := land.Load(filepath.Join(dataDir, "features"), log)
	if err != nil {
		fatal(log, err)
	}
	ghc := gh.New(envOr("GH_URL", "http://localhost:8989"))
	planner := &plan.Planner{GH: ghc, Land: ld, Region: reg, Water: water.New(ld.Grid, ld.Waters), Log: log}
	ua := "Pagdandi/2.0"
	if c := os.Getenv("GEOCODER_CONTACT_EMAIL"); c != "" {
		ua += " (" + c + ")"
	}
	srv := &server.Server{
		Region: reg, Land: ld, Planner: planner, GH: ghc, Log: log,
		Search:   &search.Service{Region: reg, Gems: ld.Gems.Items(), PhotonURL: envOr("PHOTON_URL", "https://photon.komoot.io"), UserAgent: ua, HTTP: &http.Client{Timeout: 6 * time.Second}},
		TilesDir: filepath.Join(dataDir, "tiles"),
		WebDir:   os.Getenv("WEB_DIR"),
		Origins:  strings.Split(envOr("CORS_ORIGINS", "http://localhost:5173"), ","),
	}
	addr := envOr("API_ADDR", ":8080")
	hs := &http.Server{Addr: addr, Handler: srv.Handler(), ReadHeaderTimeout: 10 * time.Second, WriteTimeout: 60 * time.Second}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	go func() {
		log.Info("listening", "addr", addr, "region", reg.ID, "graphhopper", ghc.BaseURL)
		if err := hs.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			fatal(log, err)
		}
	}()
	<-ctx.Done()
	sctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_ = hs.Shutdown(sctx)
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

func fatal(log *slog.Logger, err error) {
	log.Error("fatal", "err", err)
	os.Exit(1)
}
