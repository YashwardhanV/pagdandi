package store

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"

	"pagdandi/api/internal/geo"
)

// SaveTrace stores a (privacy-trimmed) trace.
func (d *DB) SaveTrace(ctx context.Context, userID, regionID string, line []geo.Point, ts []time.Time, purpose string) (string, error) {
	var id string
	var reg any
	if regionID != "" {
		reg = regionID
	}
	err := d.Pool.QueryRow(ctx, `
insert into traces (user_id, region_id, geom, timestamps, purpose)
values ($1::uuid, $2, ST_SetSRID(ST_GeomFromGeoJSON($3), 4326), $4, $5) returning id::text`,
		userID, reg, LineGeoJSON(line), ts, purpose).Scan(&id)
	return id, err
}

// TraceForMining is a trace not yet mined.
type TraceForMining struct {
	ID       string
	UserID   string
	RegionID string
	Line     []geo.Point
	Times    []time.Time
}

// TracesToMine returns desire-mining traces created since a time.
func (d *DB) TracesToMine(ctx context.Context, since time.Time) ([]TraceForMining, error) {
	rows, err := d.Pool.Query(ctx, `
select t.id::text, t.user_id::text, coalesce(t.region_id, ''), ST_AsGeoJSON(t.geom, 7), t.timestamps
from traces t
where t.purpose = 'desire_mining' and t.created_at >= $1
  and not exists (select 1 from offnet_segments o where o.trace_id = t.id)
order by t.created_at limit 5000`, since)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []TraceForMining
	for rows.Next() {
		var t TraceForMining
		var gj string
		if err := rows.Scan(&t.ID, &t.UserID, &t.RegionID, &gj, &t.Times); err != nil {
			return nil, err
		}
		if t.Line, err = ParseLine(gj); err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

// SaveOffnetSegment stores one off-network stretch with its H3 cells.
func (d *DB) SaveOffnetSegment(ctx context.Context, traceID, userID, regionID string, cells []string, line []geo.Point) error {
	var reg any
	if regionID != "" {
		reg = regionID
	}
	_, err := d.Pool.Exec(ctx, `
insert into offnet_segments (trace_id, user_id, region_id, cells, geom)
values ($1::uuid, $2::uuid, $3, $4, ST_SetSRID(ST_GeomFromGeoJSON($5), 4326))`, traceID, userID, reg, cells, LineGeoJSON(line))
	return err
}

// OffnetSegment is a stored off-network stretch.
type OffnetSegment struct {
	UserID    string
	RegionID  string
	Cells     []string
	Line      []geo.Point
	CreatedAt time.Time
}

// OffnetSince lists off-network segments in the mining window.
func (d *DB) OffnetSince(ctx context.Context, since time.Time) ([]OffnetSegment, error) {
	rows, err := d.Pool.Query(ctx, `select user_id::text, coalesce(region_id, ''), cells, ST_AsGeoJSON(geom, 7), created_at from offnet_segments where created_at >= $1`, since)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []OffnetSegment
	for rows.Next() {
		var s OffnetSegment
		var gj string
		if err := rows.Scan(&s.UserID, &s.RegionID, &s.Cells, &gj, &s.CreatedAt); err != nil {
			return nil, err
		}
		if s.Line, err = ParseLine(gj); err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

// Candidate is a desire-path candidate.
type Candidate struct {
	ID            string      `json:"id"`
	RegionID      string      `json:"region_id"`
	Polyline      string      `json:"polyline"`
	Line          []geo.Point `json:"-"`
	DistinctUsers int         `json:"distinct_users"`
	FirstSeen     time.Time   `json:"first_seen"`
	LastSeen      time.Time   `json:"last_seen"`
	YesCount      int         `json:"yes_count"`
	NoCount       int         `json:"no_count"`
	Status        string      `json:"status"`
}

// UpsertCandidate inserts a candidate unless one already overlaps it (within 15 m), in which case it is refreshed.
func (d *DB) UpsertCandidate(ctx context.Context, regionID string, line []geo.Point, users int, first, last time.Time) error {
	var id string
	err := d.Pool.QueryRow(ctx, `
select id::text from candidate_paths
where status in ('new', 'prompted') and ST_DWithin(geom::geography, ST_SetSRID(ST_GeomFromGeoJSON($1), 4326)::geography, 15)
  and ST_HausdorffDistance(geom, ST_SetSRID(ST_GeomFromGeoJSON($1), 4326)) < 0.0005
limit 1`, LineGeoJSON(line)).Scan(&id)
	if err == nil {
		_, err = d.Pool.Exec(ctx, `update candidate_paths set distinct_users = greatest(distinct_users, $2), last_seen = greatest(last_seen, $3) where id = $1::uuid`, id, users, last)
		return err
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return err
	}
	_, err = d.Pool.Exec(ctx, `
insert into candidate_paths (region_id, geom, distinct_users, first_seen, last_seen)
values ($1, ST_SetSRID(ST_GeomFromGeoJSON($2), 4326), $3, $4, $5)`, regionID, LineGeoJSON(line), users, first, last)
	return err
}

// CandidatesInBBox lists prompts the user has not answered yet.
func (d *DB) CandidatesInBBox(ctx context.Context, b geo.BBox, userID string) ([]Candidate, error) {
	rows, err := d.Pool.Query(ctx, `
select id::text, region_id, ST_AsGeoJSON(geom, 6), distinct_users, first_seen, last_seen, yes_count, no_count, status
from candidate_paths c
where geom && ST_MakeEnvelope($1, $2, $3, $4, 4326) and status in ('new', 'prompted') and distinct_users >= 3
  and not exists (select 1 from candidate_answers a where a.candidate_id = c.id and a.user_id = $5::uuid)
limit 50`, b[0], b[1], b[2], b[3], userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Candidate
	for rows.Next() {
		var c Candidate
		var gj string
		if err := rows.Scan(&c.ID, &c.RegionID, &gj, &c.DistinctUsers, &c.FirstSeen, &c.LastSeen, &c.YesCount, &c.NoCount, &c.Status); err != nil {
			return nil, err
		}
		if c.Line, err = ParseLine(gj); err != nil {
			return nil, err
		}
		c.Polyline = geo.Encode(c.Line)
		out = append(out, c)
	}
	return out, rows.Err()
}

// AnswerCandidate records a yes/no and returns the updated candidate.
func (d *DB) AnswerCandidate(ctx context.Context, candidateID, userID string, yes bool) (*Candidate, error) {
	tx, err := d.Pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)
	if _, err := tx.Exec(ctx, `insert into candidate_answers (candidate_id, user_id, answer) values ($1::uuid, $2::uuid, $3)`, candidateID, userID, yes); err != nil {
		if isUnique(err) {
			return nil, ErrConflict
		}
		return nil, err
	}
	var c Candidate
	var gj string
	err = tx.QueryRow(ctx, `
update candidate_paths set
  yes_count = yes_count + case when $2 then 1 else 0 end,
  no_count = no_count + case when $2 then 0 else 1 end,
  status = case when status = 'new' then 'prompted' else status end
where id = $1::uuid
returning id::text, region_id, ST_AsGeoJSON(geom, 7), distinct_users, first_seen, last_seen, yes_count, no_count, status`,
		candidateID, yes).Scan(&c.ID, &c.RegionID, &gj, &c.DistinctUsers, &c.FirstSeen, &c.LastSeen, &c.YesCount, &c.NoCount, &c.Status)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	if c.Line, err = ParseLine(gj); err != nil {
		return nil, err
	}
	return &c, tx.Commit(ctx)
}

// SetCandidateStatus marks a candidate promoted or rejected.
func (d *DB) SetCandidateStatus(ctx context.Context, id, status string) error {
	_, err := d.Pool.Exec(ctx, `update candidate_paths set status = $2 where id = $1::uuid`, id, status)
	return err
}

// PurgeExpired deletes traces past their 90-day retention.
func (d *DB) PurgeExpired(ctx context.Context) (int64, error) {
	ct, err := d.Pool.Exec(ctx, `delete from traces where expires_at < now()`)
	if err != nil {
		return 0, err
	}
	return ct.RowsAffected(), nil
}

// ReportAgreement is per-user agreement counts with shortcut consensus.
type ReportAgreement struct {
	UserID   string
	Agree    int
	Disagree int
}

// TrustAgreement computes, over the last 180 days, how often each user's
// passable/blocked reports matched the shortcut's current consensus.
func (d *DB) TrustAgreement(ctx context.Context) ([]ReportAgreement, error) {
	rows, err := d.Pool.Query(ctx, `
with c as (
  select id, case when confidence >= 0.75 then 'passable' when confidence <= 0.25 then 'blocked' end as consensus
  from shortcuts where status <> 'removed'
)
select r.user_id::text,
  count(*) filter (where r.verdict = c.consensus)::int,
  count(*) filter (where r.verdict in ('passable', 'blocked') and r.verdict <> c.consensus)::int
from shortcut_reports r join c on c.id = r.shortcut_id
where c.consensus is not null and r.created_at > now() - interval '180 days'
group by r.user_id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ReportAgreement
	for rows.Next() {
		var a ReportAgreement
		if err := rows.Scan(&a.UserID, &a.Agree, &a.Disagree); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}
