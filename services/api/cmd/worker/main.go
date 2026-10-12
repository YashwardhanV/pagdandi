// Command worker runs Pagdandi's background jobs on an internal schedule
// (SPEC §3): hourly confidence decay and lifecycle, nightly desire-path mining,
// trust and retention, weekly shortcut baking (graph refresh is driven by
// `make refresh-graph`, which calls datactl overlay + graph).
//
//	worker            run forever
//	worker once JOB   run one job now (lifecycle|mining|trust|retention)
package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"
	_ "time/tzdata"

	"pagdandi/api/internal/config"
	"pagdandi/api/internal/jobs"
	"pagdandi/api/internal/mining"
	"pagdandi/api/internal/regions"
	"pagdandi/api/internal/routing"
	"pagdandi/api/internal/store"
)

type job struct {
	name  string
	every time.Duration
	at    int // for daily jobs: hour (UTC) to run at; -1 = interval only
	run   func(context.Context) error
}

func main() {
	log := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	slog.SetDefault(log)
	if err := run(log); err != nil {
		log.Error("fatal", "err", err)
		os.Exit(1)
	}
}

func run(log *slog.Logger) error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	set, err := regions.Load(cfg.RegionsFile)
	if err != nil {
		return err
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	db, err := store.Open(ctx, cfg.DatabaseURL)
	if err != nil {
		return err
	}
	defer db.Close()
	if err := db.Migrate(ctx); err != nil {
		return err
	}
	j := &jobs.Jobs{DB: db, Regions: set, Log: log}
	m := &mining.Miner{
		DB:     db,
		Graphs: map[string]*routing.GH{"IN": routing.NewGH(cfg.GHInURL), "SE": routing.NewGH(cfg.GHSeURL)},
		Country: func(id string) string {
			if r, ok := set.Get(id); ok {
				return r.Country
			}
			return "IN"
		},
		Log: log,
	}
	all := []job{
		{"lifecycle", time.Hour, -1, j.Lifecycle},
		{"mining", 24 * time.Hour, 21, func(ctx context.Context) error { // 02:30 IST
			if err := m.ExtractNew(ctx); err != nil {
				return err
			}
			return m.Aggregate(ctx)
		}},
		{"trust", 24 * time.Hour, 22, j.Trust},
		{"retention", 24 * time.Hour, 23, j.Retention},
	}

	if len(os.Args) == 3 && os.Args[1] == "once" {
		for _, jb := range all {
			if jb.name == os.Args[2] {
				return jb.run(ctx)
			}
		}
		return fmt.Errorf("unknown job %q", os.Args[2])
	}

	log.Info("worker started", "jobs", len(all))
	// Lifecycle runs immediately so a restarted worker catches up.
	if err := j.Lifecycle(ctx); err != nil {
		log.Error("job failed", "job", "lifecycle", "err", err)
	}
	next := map[string]time.Time{}
	now := time.Now().UTC()
	for _, jb := range all {
		if jb.at < 0 {
			next[jb.name] = now.Add(jb.every)
			continue
		}
		t := time.Date(now.Year(), now.Month(), now.Day(), jb.at, 0, 0, 0, time.UTC)
		if !t.After(now) {
			t = t.Add(24 * time.Hour)
		}
		next[jb.name] = t
	}
	tick := time.NewTicker(30 * time.Second)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			log.Info("worker stopping")
			return nil
		case t := <-tick.C:
			for _, jb := range all {
				if t.Before(next[jb.name]) {
					continue
				}
				next[jb.name] = next[jb.name].Add(jb.every)
				start := time.Now()
				jctx, cancel := context.WithTimeout(ctx, 30*time.Minute)
				err := jb.run(jctx)
				cancel()
				if err != nil {
					log.Error("job failed", "job", jb.name, "err", err)
				} else {
					log.Info("job ok", "job", jb.name, "took", time.Since(start).Round(time.Millisecond))
				}
			}
		}
	}
}
