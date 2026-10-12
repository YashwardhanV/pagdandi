// Package store is the Postgres/PostGIS persistence layer. All SQL is
// parameterised; geometry crosses the boundary as GeoJSON.
package store

import (
	"context"
	"database/sql"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"

	"pagdandi/api/internal/geo"
	"pagdandi/api/internal/routing"
)

//go:embed migrations/*.sql
var migrations embed.FS

// ErrNotFound is returned when a row does not exist.
var ErrNotFound = errors.New("not found")

// ErrConflict is returned on unique-constraint violations.
var ErrConflict = errors.New("conflict")

// DB wraps a pgx pool.
type DB struct {
	Pool *pgxpool.Pool
}

// Open connects and pings.
func Open(ctx context.Context, url string) (*DB, error) {
	cfg, err := pgxpool.ParseConfig(url)
	if err != nil {
		return nil, err
	}
	cfg.MaxConns = 20
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, err
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, err
	}
	return &DB{Pool: pool}, nil
}

// Close closes the pool.
func (d *DB) Close() { d.Pool.Close() }

// Migrate applies all pending migrations.
func (d *DB) Migrate(ctx context.Context) error {
	db := stdlib.OpenDBFromPool(d.Pool)
	defer db.Close()
	return migrateSQL(ctx, db)
}

func migrateSQL(ctx context.Context, db *sql.DB) error {
	goose.SetBaseFS(migrations)
	goose.SetLogger(goose.NopLogger())
	if err := goose.SetDialect("postgres"); err != nil {
		return err
	}
	return goose.UpContext(ctx, db, "migrations")
}

// Ping checks connectivity.
func (d *DB) Ping(ctx context.Context) error { return d.Pool.Ping(ctx) }

func isUnique(err error) bool {
	var pg *pgconn.PgError
	return errors.As(err, &pg) && pg.Code == "23505"
}

// --- geometry helpers -------------------------------------------------------

// LineGeoJSON renders a LineString.
func LineGeoJSON(line []geo.Point) string {
	coords := make([][2]float64, len(line))
	for i, p := range line {
		coords[i] = [2]float64{p.Lng, p.Lat}
	}
	b, _ := json.Marshal(map[string]any{"type": "LineString", "coordinates": coords})
	return string(b)
}

// ParseLine parses a GeoJSON LineString.
func ParseLine(s string) ([]geo.Point, error) {
	var g struct {
		Type        string       `json:"type"`
		Coordinates [][2]float64 `json:"coordinates"`
	}
	if err := json.Unmarshal([]byte(s), &g); err != nil {
		return nil, err
	}
	out := make([]geo.Point, len(g.Coordinates))
	for i, c := range g.Coordinates {
		out[i] = geo.Point{Lat: c[1], Lng: c[0]}
	}
	return out, nil
}

// --- regions ------------------------------------------------------------------

// RegionRow is what the API serves about a region.
type RegionRow struct {
	ID       string          `json:"id"`
	Name     string          `json:"name"`
	Country  string          `json:"country"`
	Timezone string          `json:"timezone"`
	Center   [2]float64      `json:"center"` // [lat, lng]
	RadiusKm int             `json:"radius_km"`
	BBox     [4]float64      `json:"bbox"` // [minLng, minLat, maxLng, maxLat]
	Config   json.RawMessage `json:"config"`
}

// UpsertRegion stores a region with its coverage (GeoJSON Polygon or MultiPolygon).
func (d *DB) UpsertRegion(ctx context.Context, r RegionRow, coverageGeoJSON string) error {
	_, err := d.Pool.Exec(ctx, `
insert into regions (id, name, country, timezone, center, radius_km, coverage, config)
values ($1, $2, $3, $4, ST_SetSRID(ST_MakePoint($5, $6), 4326)::geography, $7,
        ST_Multi(ST_CollectionExtract(ST_MakeValid(ST_SetSRID(ST_GeomFromGeoJSON($8), 4326)), 3)), $9)
on conflict (id) do update set name = excluded.name, country = excluded.country, timezone = excluded.timezone,
  center = excluded.center, radius_km = excluded.radius_km, coverage = excluded.coverage, config = excluded.config`,
		r.ID, r.Name, r.Country, r.Timezone, r.Center[1], r.Center[0], r.RadiusKm, coverageGeoJSON, r.Config)
	return err
}

// Regions lists all regions.
func (d *DB) Regions(ctx context.Context) ([]RegionRow, error) {
	rows, err := d.Pool.Query(ctx, `
select id, name, country, timezone, ST_Y(center::geometry), ST_X(center::geometry), radius_km,
       ST_XMin(coverage), ST_YMin(coverage), ST_XMax(coverage), ST_YMax(coverage), config
from regions order by name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []RegionRow
	for rows.Next() {
		var r RegionRow
		if err := rows.Scan(&r.ID, &r.Name, &r.Country, &r.Timezone, &r.Center[0], &r.Center[1], &r.RadiusKm,
			&r.BBox[0], &r.BBox[1], &r.BBox[2], &r.BBox[3], &r.Config); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// RegionAt implements routing.RegionLookup: the covering region with the nearest centre.
func (d *DB) RegionAt(ctx context.Context, p geo.Point) (string, bool, error) {
	var id string
	err := d.Pool.QueryRow(ctx, `
select id from regions
where ST_Contains(coverage, ST_SetSRID(ST_MakePoint($1, $2), 4326))
order by center <-> ST_SetSRID(ST_MakePoint($1, $2), 4326)::geography
limit 1`, p.Lng, p.Lat).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	return id, true, nil
}

// CoverageGeoJSON returns the union of coverage for a country (or all) as GeoJSON.
func (d *DB) CoverageGeoJSON(ctx context.Context, country string) (string, error) {
	var s *string
	err := d.Pool.QueryRow(ctx, `select ST_AsGeoJSON(ST_Union(coverage), 6) from regions where ($1 = '' or country = $1)`, country).Scan(&s)
	if err != nil || s == nil {
		return "", err
	}
	return *s, nil
}

// --- destinations --------------------------------------------------------------

// Destination is a curated place.
type Destination struct {
	ID        string   `json:"id"`
	RegionID  string   `json:"region_id"`
	Name      string   `json:"name"`
	NameLocal *string  `json:"name_local,omitempty"`
	Category  string   `json:"category"`
	Lat       *float64 `json:"lat"`
	Lng       *float64 `json:"lng"`
	ApproxKm  *int     `json:"approx_km,omitempty"`
	Verified  bool     `json:"verified"`
	// NearbyShortcuts counts active shortcuts within 10 km.
	NearbyShortcuts int `json:"nearby_shortcuts"`
}

// ReplaceDestinations rewrites the curated list for one region.
func (d *DB) ReplaceDestinations(ctx context.Context, regionID string, ds []Destination) error {
	tx, err := d.Pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if _, err := tx.Exec(ctx, `delete from destinations where region_id = $1`, regionID); err != nil {
		return err
	}
	for _, x := range ds {
		var lng, lat any
		if x.Lat != nil && x.Lng != nil {
			lat, lng = *x.Lat, *x.Lng
		}
		if _, err := tx.Exec(ctx, `
insert into destinations (region_id, name, name_local, category, geom, approx_km, verified)
values ($1, $2, $3, $4, case when $5::float8 is null then null else ST_SetSRID(ST_MakePoint($5, $6), 4326)::geography end, $7, $8)`,
			regionID, x.Name, x.NameLocal, x.Category, lng, lat, x.ApproxKm, x.Verified); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}

const destCols = `id::text, region_id, name, name_local, category,
  ST_Y(geom::geometry), ST_X(geom::geometry), approx_km, verified,
  (select count(*) from shortcuts s where s.status = 'active' and geom is not null
     and ST_DWithin(s.geom::geography, destinations.geom, 10000))::int`

func scanDest(row pgx.Row) (Destination, error) {
	var x Destination
	err := row.Scan(&x.ID, &x.RegionID, &x.Name, &x.NameLocal, &x.Category, &x.Lat, &x.Lng, &x.ApproxKm, &x.Verified, &x.NearbyShortcuts)
	return x, err
}

// Destinations lists a region's curated places (verified first, then by distance).
func (d *DB) Destinations(ctx context.Context, regionID string) ([]Destination, error) {
	rows, err := d.Pool.Query(ctx, `select `+destCols+` from destinations where region_id = $1 order by verified desc, approx_km nulls last, name`, regionID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Destination
	for rows.Next() {
		x, err := scanDest(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, x)
	}
	return out, rows.Err()
}

// SearchDestinations does trigram search over verified destinations, nearest first on ties.
func (d *DB) SearchDestinations(ctx context.Context, q string, near *geo.Point, limit int) ([]Destination, error) {
	var lng, lat any
	if near != nil {
		lng, lat = near.Lng, near.Lat
	}
	rows, err := d.Pool.Query(ctx, `
select `+destCols+` from destinations
where verified and geom is not null and (name % $1 or name ilike '%' || $1 || '%')
order by similarity(name, $1) desc,
  case when $2::float8 is null then 0 else ST_Distance(geom, ST_SetSRID(ST_MakePoint($2, $3), 4326)::geography) end
limit $4`, q, lng, lat, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Destination
	for rows.Next() {
		x, err := scanDest(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, x)
	}
	return out, rows.Err()
}

// --- OSM support tables --------------------------------------------------------

// NearestNode finds the closest routable OSM node within maxM metres.
func (d *DB) NearestNode(ctx context.Context, p geo.Point, maxM float64) (id int64, at geo.Point, dist float64, err error) {
	err = d.Pool.QueryRow(ctx, `
select id, ST_Y(geom), ST_X(geom), ST_DistanceSphere(geom, ST_SetSRID(ST_MakePoint($1, $2), 4326))
from osm_nodes
order by geom <-> ST_SetSRID(ST_MakePoint($1, $2), 4326)
limit 1`, p.Lng, p.Lat).Scan(&id, &at.Lat, &at.Lng, &dist)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, geo.Point{}, 0, ErrNotFound
	}
	if err != nil {
		return 0, geo.Point{}, 0, err
	}
	if dist > maxM {
		return id, at, dist, ErrNotFound
	}
	return id, at, dist, nil
}

// BarriersNear implements routing.Store.
func (d *DB) BarriersNear(ctx context.Context, line []geo.Point, distM float64) ([]routing.Barrier, error) {
	if len(line) < 2 {
		return nil, nil
	}
	deg := distM / 111320 * 2
	rows, err := d.Pool.Query(ctx, `
with l as (select ST_SetSRID(ST_GeomFromGeoJSON($1), 4326) g)
select b.id, b.kind, coalesce(b.access, ''), ST_Y(b.geom), ST_X(b.geom)
from osm_barriers b, l
where b.geom && ST_Expand(l.g, $3) and ST_DWithin(b.geom::geography, l.g::geography, $2)
  and b.kind in ('gate', 'lift_gate', 'swing_gate', 'sliding_gate', 'chain', 'bar')
limit 50`, LineGeoJSON(line), distM, deg)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []routing.Barrier
	for rows.Next() {
		var b routing.Barrier
		if err := rows.Scan(&b.ID, &b.Kind, &b.Access, &b.Point.Lat, &b.Point.Lng); err != nil {
			return nil, err
		}
		out = append(out, b)
	}
	return out, rows.Err()
}

// --- route stats ---------------------------------------------------------------

// RecordRouteStat stores an anonymised route choice.
func (d *DB) RecordRouteStat(ctx context.Context, region, h3o, h3d, chosen string, savedM, savedS int) error {
	_, err := d.Pool.Exec(ctx, `insert into route_stats (region_id, h3_origin_r7, h3_dest_r7, chosen, saved_m, saved_s) values ($1,$2,$3,$4,$5,$6)`,
		region, h3o, h3d, chosen, savedM, savedS)
	return err
}

// ellipseBox returns a bbox (degrees) containing the ellipse with foci a, b and the given major axis.
func ellipseBox(a, b geo.Point, major float64) (minLng, minLat, maxLng, maxLat float64) {
	mid := geo.Point{Lat: (a.Lat + b.Lat) / 2, Lng: (a.Lng + b.Lng) / 2}
	r := major / 2
	dLat := r / 111320
	dLng := r / (111320 * math.Max(0.1, math.Cos(mid.Lat*math.Pi/180)))
	return mid.Lng - dLng, mid.Lat - dLat, mid.Lng + dLng, mid.Lat + dLat
}

// Candidates implements routing.Store (SPEC §6.3 step 4 ellipse filter).
func (d *DB) Candidates(ctx context.Context, q routing.CandidateQuery) ([]routing.Candidate, error) {
	x0, y0, x1, y1 := ellipseBox(q.From, q.To, q.MajorAxisM)
	rows, err := d.Pool.Query(ctx, `
with o as (select ST_SetSRID(ST_MakePoint($1, $2), 4326) g), dd as (select ST_SetSRID(ST_MakePoint($3, $4), 4326) g)
select s.id::text, coalesce(s.title, ''), ST_AsGeoJSON(s.geom, 7), s.length_m, s.oneway, s.surface, s.flags,
       s.confidence, s.last_confirmed_at,
       (select count(*) from shortcut_reports r where r.shortcut_id = s.id and r.verdict = 'passable'
          and r.created_at > now() - interval '30 days')::int
from shortcuts s, o, dd
where s.status = 'active' and s.confidence >= $5 and $6 = any(s.vehicles)
  and s.geom && ST_MakeEnvelope($7, $8, $9, $10, 4326)
  and ST_DistanceSphere(ST_StartPoint(s.geom), o.g) + ST_DistanceSphere(ST_StartPoint(s.geom), dd.g) <= $11
  and ST_DistanceSphere(ST_EndPoint(s.geom), o.g) + ST_DistanceSphere(ST_EndPoint(s.geom), dd.g) <= $11
limit 200`, q.From.Lng, q.From.Lat, q.To.Lng, q.To.Lat, q.MinConfidence, q.Vehicle, x0, y0, x1, y1, q.MajorAxisM)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []routing.Candidate
	for rows.Next() {
		var c routing.Candidate
		var gj string
		var length int
		var conf float32
		if err := rows.Scan(&c.ID, &c.Title, &gj, &length, &c.Oneway, &c.Surface, &c.Flags, &conf, &c.LastConfirmedAt, &c.Confirmations30d); err != nil {
			return nil, err
		}
		c.LengthM = float64(length)
		c.Confidence = math.Round(float64(conf)*1000) / 1000
		if c.Line, err = ParseLine(gj); err != nil {
			return nil, fmt.Errorf("shortcut %s geometry: %w", c.ID, err)
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// now is overridable in tests.
var now = time.Now
