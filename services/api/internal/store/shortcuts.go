package store

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"pagdandi/api/internal/geo"
	"pagdandi/api/internal/shortcuts"
)

// Shortcut is a stored community shortcut.
type Shortcut struct {
	ID              string      `json:"id"`
	RegionID        string      `json:"region_id"`
	Title           *string     `json:"title"`
	Line            []geo.Point `json:"-"`
	Polyline        string      `json:"polyline"`
	LengthM         int         `json:"length_m"`
	StartNode       int64       `json:"start_osm_node"`
	EndNode         int64       `json:"end_osm_node"`
	Oneway          bool        `json:"oneway"`
	Surface         string      `json:"surface"`
	Vehicles        []string    `json:"vehicles"`
	Flags           []string    `json:"flags"`
	Status          string      `json:"status"`
	Alpha           float64     `json:"-"`
	Beta            float64     `json:"-"`
	Confidence      float64     `json:"confidence"`
	Band            string      `json:"confidence_band"`
	LastConfirmedAt *time.Time  `json:"last_confirmed_at"`
	CreatedBy       *string     `json:"-"`
	CreatedAt       time.Time   `json:"created_at"`
	UpdatedAt       time.Time   `json:"updated_at"`
}

// NewShortcut is the insert payload.
type NewShortcut struct {
	RegionID  string
	Title     *string
	Line      []geo.Point
	LengthM   int
	StartNode int64
	EndNode   int64
	Oneway    bool
	Surface   string
	Vehicles  []string
	Flags     []string
	Status    string
	CreatedBy string
}

const shortcutColsTmpl = `{p}id::text, {p}region_id, {p}title, ST_AsGeoJSON({p}geom, 7), {p}length_m, {p}start_osm_node, {p}end_osm_node, {p}oneway,
  {p}surface, {p}vehicles, {p}flags, {p}status, {p}alpha, {p}beta, {p}confidence, {p}last_confirmed_at, {p}created_by::text, {p}created_at, {p}updated_at`

// shortcutColsAs returns the shortcut column list qualified with a table alias.
func shortcutColsAs(alias string) string {
	p := ""
	if alias != "" {
		p = alias + "."
	}
	return strings.ReplaceAll(shortcutColsTmpl, "{p}", p)
}

var shortcutCols = shortcutColsAs("")

func scanShortcut(row pgx.Row) (*Shortcut, error) {
	var s Shortcut
	var gj string
	var a, b, c float32
	if err := row.Scan(&s.ID, &s.RegionID, &s.Title, &gj, &s.LengthM, &s.StartNode, &s.EndNode, &s.Oneway, &s.Surface,
		&s.Vehicles, &s.Flags, &s.Status, &a, &b, &c, &s.LastConfirmedAt, &s.CreatedBy, &s.CreatedAt, &s.UpdatedAt); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	s.Alpha, s.Beta, s.Confidence = float64(a), float64(b), float64(c)
	s.Band = shortcuts.Band(s.Confidence)
	line, err := ParseLine(gj)
	if err != nil {
		return nil, err
	}
	s.Line = line
	s.Polyline = geo.Encode(line)
	return &s, nil
}

// CreateShortcut inserts a shortcut.
func (d *DB) CreateShortcut(ctx context.Context, n NewShortcut) (*Shortcut, error) {
	status := n.Status
	if status == "" {
		status = shortcuts.StatusPending
	}
	row := d.Pool.QueryRow(ctx, `
insert into shortcuts (region_id, title, geom, length_m, start_osm_node, end_osm_node, oneway, surface, vehicles, flags, status, created_by)
values ($1, $2, ST_SetSRID(ST_GeomFromGeoJSON($3), 4326), $4, $5, $6, $7, $8, $9, $10, $11, $12::uuid)
returning `+shortcutCols,
		n.RegionID, n.Title, LineGeoJSON(n.Line), n.LengthM, n.StartNode, n.EndNode, n.Oneway, n.Surface, n.Vehicles, n.Flags, status, n.CreatedBy)
	return scanShortcut(row)
}

// GetShortcut loads one shortcut.
func (d *DB) GetShortcut(ctx context.Context, id string) (*Shortcut, error) {
	return scanShortcut(d.Pool.QueryRow(ctx, `select `+shortcutCols+` from shortcuts where id = $1::uuid`, id))
}

// ShortcutsInBBox lists visible shortcuts, simplified by tolerance (degrees).
func (d *DB) ShortcutsInBBox(ctx context.Context, b geo.BBox, simplifyDeg float64, includeLow bool, limit int) ([]Shortcut, error) {
	rows, err := d.Pool.Query(ctx, `
select id::text, region_id, title, ST_AsGeoJSON(ST_Simplify(geom, $5), 6), length_m, start_osm_node, end_osm_node, oneway,
  surface, vehicles, flags, status, alpha, beta, confidence, last_confirmed_at, created_by::text, created_at, updated_at
from shortcuts
where geom && ST_MakeEnvelope($1, $2, $3, $4, 4326)
  and status in ('pending', 'active', 'stale')
  and ($6 or confidence >= 0.5 or status = 'pending')
order by confidence desc
limit $7`, b[0], b[1], b[2], b[3], simplifyDeg, includeLow, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Shortcut
	for rows.Next() {
		s, err := scanShortcut(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *s)
	}
	return out, rows.Err()
}

// CountShortcutsNear counts active shortcuts within r metres of p.
func (d *DB) CountShortcutsNear(ctx context.Context, p geo.Point, r float64) (int, error) {
	var n int
	err := d.Pool.QueryRow(ctx, `
select count(*) from shortcuts
where status = 'active' and ST_DWithin(geom::geography, ST_SetSRID(ST_MakePoint($1, $2), 4326)::geography, $3)`,
		p.Lng, p.Lat, r).Scan(&n)
	return n, err
}

// ShortcutCountsByStatus is used for metrics.
func (d *DB) ShortcutCountsByStatus(ctx context.Context) (map[string]int, error) {
	rows, err := d.Pool.Query(ctx, `select status, count(*) from shortcuts group by status`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]int{}
	for rows.Next() {
		var s string
		var n int
		if err := rows.Scan(&s, &n); err != nil {
			return nil, err
		}
		out[s] = n
	}
	return out, rows.Err()
}

// CountUserShortcutsToday supports the 10/day creation limit.
func (d *DB) CountUserShortcutsToday(ctx context.Context, userID string) (int, error) {
	var n int
	err := d.Pool.QueryRow(ctx, `select count(*) from shortcuts where created_by = $1::uuid and created_at > now() - interval '1 day'`, userID).Scan(&n)
	return n, err
}

// UpdateShortcutMeta edits title/flags/surface/vehicles (nil = unchanged).
func (d *DB) UpdateShortcutMeta(ctx context.Context, id string, title *string, flags, vehicles []string, surface *string) (*Shortcut, error) {
	return scanShortcut(d.Pool.QueryRow(ctx, `
update shortcuts set
  title = coalesce($2, title),
  flags = coalesce($3, flags),
  vehicles = coalesce($4, vehicles),
  surface = coalesce($5, surface),
  updated_at = now()
where id = $1::uuid
returning `+shortcutCols, id, title, flags, vehicles, surface))
}

// SetShortcutStatus sets status directly (moderation).
func (d *DB) SetShortcutStatus(ctx context.Context, id, status string) error {
	ct, err := d.Pool.Exec(ctx, `update shortcuts set status = $2, updated_at = now() where id = $1::uuid`, id, status)
	if err != nil {
		return err
	}
	if ct.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// SaveShortcutState stores recomputed confidence and status.
func (d *DB) SaveShortcutState(ctx context.Context, id string, alpha, beta, conf float64, status string, lastConfirmed *time.Time) error {
	_, err := d.Pool.Exec(ctx, `
update shortcuts set alpha = $2, beta = $3, confidence = $4, status = $5,
  last_confirmed_at = coalesce($6, last_confirmed_at),
  updated_at = case when status <> $5 then now() else updated_at end
where id = $1::uuid`, id, alpha, beta, conf, status, lastConfirmed)
	return err
}

// --- reports --------------------------------------------------------------------

// NewReport is a verification.
type NewReport struct {
	ShortcutID string
	UserID     string
	Verdict    string
	Reason     *string
	Note       *string
}

// AddReport stores a report; ErrConflict if the user already reported today.
func (d *DB) AddReport(ctx context.Context, r NewReport) error {
	_, err := d.Pool.Exec(ctx, `insert into shortcut_reports (shortcut_id, user_id, verdict, reason, note) values ($1::uuid, $2::uuid, $3, $4, $5)`,
		r.ShortcutID, r.UserID, r.Verdict, r.Reason, r.Note)
	if isUnique(err) {
		return ErrConflict
	}
	return err
}

// CountUserReportsToday supports the 100/day limit.
func (d *DB) CountUserReportsToday(ctx context.Context, userID string) (int, error) {
	var n int
	err := d.Pool.QueryRow(ctx, `select count(*) from shortcut_reports where user_id = $1::uuid and created_at > now() - interval '1 day'`, userID).Scan(&n)
	return n, err
}

// ReportsFor returns all reports for a shortcut with each reporter's current trust.
func (d *DB) ReportsFor(ctx context.Context, shortcutID string) ([]shortcuts.Report, error) {
	rows, err := d.Pool.Query(ctx, `
select r.user_id::text, r.verdict, coalesce(r.reason, ''), r.created_at, u.trust
from shortcut_reports r join users u on u.id = r.user_id
where r.shortcut_id = $1::uuid order by r.created_at`, shortcutID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []shortcuts.Report
	for rows.Next() {
		var r shortcuts.Report
		var t float32
		if err := rows.Scan(&r.UserID, &r.Verdict, &r.Reason, &r.CreatedAt, &t); err != nil {
			return nil, err
		}
		r.Trust = float64(t)
		out = append(out, r)
	}
	return out, rows.Err()
}

// UserTrust returns a user's trust.
func (d *DB) UserTrust(ctx context.Context, userID string) (float64, error) {
	var t float32
	err := d.Pool.QueryRow(ctx, `select trust from users where id = $1::uuid`, userID).Scan(&t)
	if errors.Is(err, pgx.ErrNoRows) {
		return 1, nil
	}
	return float64(t), err
}

// LifecycleRow is a shortcut plus what the lifecycle job needs.
type LifecycleRow struct {
	ID           string
	RegionID     string
	Status       string
	CreatedBy    string
	CreatorTrust float64
	CreatedAt    time.Time
}

// ShortcutsForLifecycle lists every non-removed shortcut.
func (d *DB) ShortcutsForLifecycle(ctx context.Context) ([]LifecycleRow, error) {
	rows, err := d.Pool.Query(ctx, `
select s.id::text, s.region_id, s.status, coalesce(s.created_by::text, ''), coalesce(u.trust, 1), s.created_at
from shortcuts s left join users u on u.id = s.created_by
where s.status <> 'removed'`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []LifecycleRow
	for rows.Next() {
		var r LifecycleRow
		var t float32
		if err := rows.Scan(&r.ID, &r.RegionID, &r.Status, &r.CreatedBy, &t, &r.CreatedAt); err != nil {
			return nil, err
		}
		r.CreatorTrust = float64(t)
		out = append(out, r)
	}
	return out, rows.Err()
}

// ReportSummary is the anonymous report list for the detail view.
type ReportSummary struct {
	Day     string `json:"day"`
	Verdict string `json:"verdict"`
	Reason  string `json:"reason,omitempty"`
	Count   int    `json:"count"`
}

// RecentReportSummary groups the last 90 days of reports by day and verdict (no identities).
func (d *DB) RecentReportSummary(ctx context.Context, shortcutID string) ([]ReportSummary, error) {
	rows, err := d.Pool.Query(ctx, `
select report_day::text, verdict, coalesce(reason, ''), count(*)::int from shortcut_reports
where shortcut_id = $1::uuid and created_at > now() - interval '90 days'
group by 1, 2, 3 order by 1 desc limit 60`, shortcutID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ReportSummary
	for rows.Next() {
		var r ReportSummary
		if err := rows.Scan(&r.Day, &r.Verdict, &r.Reason, &r.Count); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// UserContributions lists a user's shortcuts and report count.
func (d *DB) UserContributions(ctx context.Context, userID string) ([]Shortcut, int, error) {
	rows, err := d.Pool.Query(ctx, `select `+shortcutCols+` from shortcuts where created_by = $1::uuid order by created_at desc limit 200`, userID)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	var out []Shortcut
	for rows.Next() {
		s, err := scanShortcut(rows)
		if err != nil {
			return nil, 0, err
		}
		out = append(out, *s)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, err
	}
	var n int
	err = d.Pool.QueryRow(ctx, `select count(*) from shortcut_reports where user_id = $1::uuid`, userID).Scan(&n)
	return out, n, err
}

// AdminQueueItem is a moderation item.
type AdminQueueItem struct {
	Kind      string    `json:"kind"` // new_shortcut | report
	Shortcut  Shortcut  `json:"shortcut"`
	Reason    string    `json:"reason,omitempty"`
	Note      string    `json:"note,omitempty"`
	ReportsN  int       `json:"reports"`
	CreatedAt time.Time `json:"created_at"`
}

// AdminQueue returns pending shortcuts and private/unsafe reports awaiting review.
func (d *DB) AdminQueue(ctx context.Context, limit int) ([]AdminQueueItem, error) {
	var out []AdminQueueItem
	rows, err := d.Pool.Query(ctx, `select `+shortcutCols+` from shortcuts where status = 'pending' order by created_at limit $1`, limit)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		s, err := scanShortcut(rows)
		if err != nil {
			rows.Close()
			return nil, err
		}
		out = append(out, AdminQueueItem{Kind: "new_shortcut", Shortcut: *s, CreatedAt: s.CreatedAt})
	}
	rows.Close()
	rows, err = d.Pool.Query(ctx, `
select `+shortcutColsAs("s")+`, r.reason, coalesce(r.note, ''), r.created_at,
  (select count(*) from shortcut_reports x where x.shortcut_id = s.id and x.reason in ('private', 'unsafe'))::int
from shortcut_reports r join shortcuts s on s.id = r.shortcut_id
where r.reason in ('private', 'unsafe') and s.status <> 'removed'
  and not exists (select 1 from moderation_actions m where m.shortcut_id = s.id and m.created_at > r.created_at)
order by r.created_at limit $1`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var it AdminQueueItem
		var gj string
		var a, b, c float32
		s := &it.Shortcut
		if err := rows.Scan(&s.ID, &s.RegionID, &s.Title, &gj, &s.LengthM, &s.StartNode, &s.EndNode, &s.Oneway, &s.Surface,
			&s.Vehicles, &s.Flags, &s.Status, &a, &b, &c, &s.LastConfirmedAt, &s.CreatedBy, &s.CreatedAt, &s.UpdatedAt,
			&it.Reason, &it.Note, &it.CreatedAt, &it.ReportsN); err != nil {
			return nil, err
		}
		s.Confidence = float64(c)
		s.Band = shortcuts.Band(s.Confidence)
		if s.Line, err = ParseLine(gj); err == nil {
			s.Polyline = geo.Encode(s.Line)
		}
		it.Kind = "report"
		out = append(out, it)
	}
	return out, rows.Err()
}

// LogModeration records a moderator action.
func (d *DB) LogModeration(ctx context.Context, shortcutID, moderatorID, action, note string) error {
	_, err := d.Pool.Exec(ctx, `insert into moderation_actions (shortcut_id, moderator_id, action, note) values ($1::uuid, $2::uuid, $3, $4)`,
		shortcutID, moderatorID, action, note)
	return err
}

// BakeCandidates returns shortcuts eligible for baking into the graph (SPEC §7.6).
func (d *DB) BakeCandidates(ctx context.Context, country string) ([]Shortcut, error) {
	rows, err := d.Pool.Query(ctx, `
select `+shortcutColsAs("s")+` from shortcuts s join regions g on g.id = s.region_id
where s.status = 'active' and s.confidence >= 0.75 and g.country = $1
  and (select count(*) from shortcut_reports r where r.shortcut_id = s.id and r.verdict = 'passable') >= 5`, country)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Shortcut
	for rows.Next() {
		s, err := scanShortcut(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *s)
	}
	return out, rows.Err()
}
