package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"math"
	"time"

	"pagdandi/api/internal/geo"
	"pagdandi/api/internal/jobs"
	"pagdandi/api/internal/store"
)

// cmdMigrate applies database migrations.
func cmdMigrate(ctx context.Context, o *opts) error {
	db, err := openDB(ctx, o)
	if err != nil {
		return err
	}
	db.Close()
	o.log.Info("migrations applied")
	return nil
}

// cmdAdmin grants a role: datactl admin --email you@x.com [--role admin|moderator|rider].
func cmdAdmin(ctx context.Context, o *opts, args []string) error {
	fs := flag.NewFlagSet("admin", flag.ContinueOnError)
	email := fs.String("email", "", "user email")
	role := fs.String("role", "admin", "admin | moderator | rider")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *email == "" {
		return errors.New("--email is required")
	}
	if *role != "admin" && *role != "moderator" && *role != "rider" {
		return fmt.Errorf("unknown role %q", *role)
	}
	db, err := openDB(ctx, o)
	if err != nil {
		return err
	}
	defer db.Close()
	if _, err := db.UpsertUserByEmail(ctx, *email, ""); err != nil {
		return err
	}
	if err := db.SetRole(ctx, *email, *role); err != nil {
		return err
	}
	o.log.Info("role set", "email", *email, "role", *role)
	return nil
}

// demoShortcut is a demo shortcut between two points near Bilawali; both ends
// snap to the nearest real OSM road node. Titles say "Demo" so nobody mistakes
// them for rider-verified paths.
type demoShortcut struct {
	title   string
	a, b    geo.Point
	surface string
	flags   []string
	reports []string // verdicts from demo riders, oldest first
}

// cmdSeedDev loads demo shortcuts and reports around Bilawali, Indore (SPEC §13.2).
func cmdSeedDev(ctx context.Context, o *opts) error {
	db, err := openDB(ctx, o)
	if err != nil {
		return err
	}
	defer db.Close()
	if _, ok, err := db.RegionAt(ctx, geo.Point{Lat: 22.6893, Lng: 75.8529}); err != nil || !ok {
		return errors.New("region indore is not seeded: run `make data REGION=indore` first")
	}
	var nodes int
	_ = db.Pool.QueryRow(ctx, `select count(*) from osm_nodes where country = 'IN'`).Scan(&nodes)
	if nodes == 0 {
		return errors.New("no OSM nodes loaded: run `make data REGION=indore` first")
	}
	// Demo riders. Their emails use the reserved .invalid TLD so nothing is ever sent.
	var riders []string
	for i := 1; i <= 5; i++ {
		u, err := db.UpsertUserByEmail(ctx, fmt.Sprintf("demo-rider-%d@pagdandi.invalid", i), fmt.Sprintf("Demo rider %d", i))
		if err != nil {
			return err
		}
		riders = append(riders, u.ID)
	}
	// The founding example ends at Bilawali Lake: anchor the demo track at the
	// road node nearest the seeded destination so it actually reaches the lake.
	bilawali := geo.Point{Lat: 22.6631, Lng: 75.8652}
	_ = db.Pool.QueryRow(ctx, `select ST_Y(geom::geometry), ST_X(geom::geometry) from destinations where region_id = 'indore' and name = 'Bilawali Lake' and verified`).Scan(&bilawali.Lat, &bilawali.Lng)
	if _, err := db.Pool.Exec(ctx, `delete from shortcuts where title like 'Demo:%'`); err != nil {
		return err
	}
	demos := []demoShortcut{
		{"Demo: Choithram to Bilawali farm track", geo.Point{Lat: 22.6828, Lng: 75.8551}, bilawali, "unpaved",
			[]string{"floods_in_rain"}, []string{"passable", "passable", "passable", "partly", "passable", "passable"}},
		{"Demo: Lake-side kaccha lane", geo.Point{Lat: 22.6612, Lng: 75.8603}, geo.Point{Lat: 22.6560, Lng: 75.8664}, "track",
			[]string{"gate", "no_lights"}, []string{"passable", "passable"}},
		{"Demo: Colony cut-through", geo.Point{Lat: 22.6905, Lng: 75.8610}, geo.Point{Lat: 22.6850, Lng: 75.8660}, "paved",
			nil, []string{"passable", "blocked", "passable"}},
	}
	j := &jobs.Jobs{DB: db, Regions: o.regions, Log: o.log}
	now := time.Now()
	for _, d := range demos {
		a, err := snapDemo(ctx, db, d.a)
		if err != nil {
			o.log.Warn("demo shortcut skipped", "title", d.title, "err", err)
			continue
		}
		b, err := snapDemo(ctx, db, d.b)
		if err != nil || a.id == b.id {
			o.log.Warn("demo shortcut skipped", "title", d.title, "err", err)
			continue
		}
		mid := geo.Point{Lat: (a.p.Lat+b.p.Lat)/2 + 0.0008, Lng: (a.p.Lng+b.p.Lng)/2 - 0.0006}
		line := []geo.Point{a.p, mid, b.p}
		title := d.title
		flags := d.flags
		if flags == nil {
			flags = []string{}
		}
		sc, err := db.CreateShortcut(ctx, store.NewShortcut{
			RegionID: "indore", Title: &title, Line: line, LengthM: int(math.Round(geo.Length(line))),
			StartNode: a.id, EndNode: b.id, Surface: d.surface, Vehicles: []string{"scooter", "motorcycle"},
			Flags: flags, CreatedBy: riders[0],
		})
		if err != nil {
			return err
		}
		for i, v := range d.reports {
			at := now.Add(-time.Duration(len(d.reports)-i) * 36 * time.Hour)
			var reason any
			if v == "blocked" {
				reason = "flooded"
			}
			if _, err := db.Pool.Exec(ctx, `insert into shortcut_reports (shortcut_id, user_id, verdict, reason, created_at) values ($1::uuid, $2::uuid, $3, $4, $5)`,
				sc.ID, riders[(i+1)%len(riders)], v, reason, at); err != nil {
				return err
			}
		}
		res, err := j.RecomputeOne(ctx, sc.ID)
		if err != nil {
			return err
		}
		o.log.Info("demo shortcut", "title", d.title, "status", res.Status, "confidence", math.Round(res.Confidence*100)/100)
	}
	return nil
}

type snapped struct {
	id int64
	p  geo.Point
}

func snapDemo(ctx context.Context, db *store.DB, p geo.Point) (snapped, error) {
	id, at, _, err := db.NearestNode(ctx, p, 2000)
	if err != nil {
		return snapped{}, fmt.Errorf("no road node near %.5f,%.5f", p.Lat, p.Lng)
	}
	return snapped{id: id, p: at}, nil
}
