// Command api serves the Pagdandi HTTP API (SPEC §8).
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"
	_ "time/tzdata"

	"pagdandi/api/internal/auth"
	"pagdandi/api/internal/cache"
	"pagdandi/api/internal/config"
	"pagdandi/api/internal/geocode"
	"pagdandi/api/internal/handlers"
	"pagdandi/api/internal/jobs"
	"pagdandi/api/internal/regions"
	"pagdandi/api/internal/routing"
	"pagdandi/api/internal/store"
)

func main() {
	log := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	slog.SetDefault(log)
	if err := run(log); err != nil {
		log.Error("fatal", "err", err)
		os.Exit(1)
	}
}

// seededSet tracks which regions have data in Postgres, refreshed periodically.
type seededSet struct {
	mu  sync.RWMutex
	ids map[string]bool
}

func (s *seededSet) has(id string) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.ids) == 0 || s.ids[id]
}

func (s *seededSet) refresh(ctx context.Context, db *store.DB) {
	rows, err := db.Regions(ctx)
	if err != nil {
		return
	}
	m := map[string]bool{}
	for _, r := range rows {
		m[r.ID] = true
	}
	s.mu.Lock()
	s.ids = m
	s.mu.Unlock()
}

func run(log *slog.Logger) error {
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("config: %w", err)
	}
	set, err := regions.Load(cfg.RegionsFile)
	if err != nil {
		return fmt.Errorf("regions: %w", err)
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	db, err := store.Open(ctx, cfg.DatabaseURL)
	if err != nil {
		return fmt.Errorf("postgres: %w", err)
	}
	defer db.Close()
	if err := db.Migrate(ctx); err != nil {
		return fmt.Errorf("migrate: %w", err)
	}

	var c cache.Cache
	if cfg.RedisURL == "" {
		log.Warn("REDIS_URL empty: using in-process cache and rate limits")
		c = cache.NewMemory()
	} else {
		rc, err := cache.NewRedis(cfg.RedisURL)
		if err != nil {
			return fmt.Errorf("redis: %w", err)
		}
		if err := rc.Ping(ctx); err != nil {
			log.Warn("redis not reachable yet", "err", err)
		}
		c = rc
	}

	metrics := handlers.NewMetrics()
	graphs := map[string]*routing.GH{"IN": routing.NewGH(cfg.GHInURL), "SE": routing.NewGH(cfg.GHSeURL)}
	for _, g := range graphs {
		g.Observe = metrics.ObserveGH
	}
	seeded := &seededSet{}
	seeded.refresh(ctx, db)
	router := &routing.Router{Regions: set, Lookup: db, Graphs: graphs, Store: db, Cache: c, Log: log, Seeded: seeded.has}
	geo := &geocode.Service{DB: db, Lookup: db, Cache: c, PhotonURL: cfg.PhotonURL,
		UserAgent: "Pagdandi/0.1 (" + orDefault(cfg.GeocoderContactEmail, "contact not configured") + ")"}
	srv := &handlers.Server{
		Cfg: cfg, DB: db, Cache: c, Regions: set, Router: router, Geo: geo,
		Auth: auth.New(cfg, db, log), Jobs: &jobs.Jobs{DB: db, Regions: set, Log: log},
		Graphs: graphs, Log: log, Metrics: metrics,
	}

	go func() {
		t := time.NewTicker(time.Minute)
		defer t.Stop()
		for {
			srv.RefreshShortcutGauge(ctx)
			seeded.refresh(ctx, db)
			select {
			case <-ctx.Done():
				return
			case <-t.C:
			}
		}
	}()

	hs := &http.Server{
		Addr:              cfg.Addr,
		Handler:           srv.Handler(),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       120 * time.Second,
		MaxHeaderBytes:    32 << 10,
	}
	errc := make(chan error, 1)
	go func() {
		log.Info("api listening", "addr", cfg.Addr, "env", cfg.Env, "regions", len(set.Regions))
		errc <- hs.ListenAndServe()
	}()
	select {
	case err := <-errc:
		if !errors.Is(err, http.ErrServerClosed) {
			return err
		}
	case <-ctx.Done():
		log.Info("shutting down")
		sctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		return hs.Shutdown(sctx)
	}
	return nil
}

func orDefault(s, d string) string {
	if s == "" {
		return d
	}
	return s
}
