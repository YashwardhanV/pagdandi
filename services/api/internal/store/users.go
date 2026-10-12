package store

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"pagdandi/api/internal/geo"
)

// User is an account.
type User struct {
	ID              string     `json:"id"`
	Email           *string    `json:"email,omitempty"`
	DisplayName     string     `json:"display_name"`
	HomeRegion      *string    `json:"home_region"`
	Trust           float64    `json:"trust"`
	Role            string     `json:"role"`
	TracesConsentAt *time.Time `json:"traces_consent_at"`
	CreatedAt       time.Time  `json:"created_at"`
}

const userCols = `id::text, email::text, display_name, home_region, trust, role, traces_consent_at, created_at`

func scanUser(row pgx.Row) (*User, error) {
	var u User
	var t float32
	err := row.Scan(&u.ID, &u.Email, &u.DisplayName, &u.HomeRegion, &t, &u.Role, &u.TracesConsentAt, &u.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	u.Trust = float64(t)
	return &u, nil
}

// UpsertUserByEmail returns the user with this email, creating it if needed.
func (d *DB) UpsertUserByEmail(ctx context.Context, email, displayName string) (*User, error) {
	if displayName == "" {
		displayName = strings.Split(email, "@")[0]
	}
	return scanUser(d.Pool.QueryRow(ctx, `
insert into users (email, display_name) values ($1, $2)
on conflict (email) do update set email = excluded.email
returning `+userCols, email, displayName))
}

// GetUser loads a user.
func (d *DB) GetUser(ctx context.Context, id string) (*User, error) {
	return scanUser(d.Pool.QueryRow(ctx, `select `+userCols+` from users where id = $1::uuid`, id))
}

// SetRole sets a user's role by email.
func (d *DB) SetRole(ctx context.Context, email, role string) error {
	ct, err := d.Pool.Exec(ctx, `update users set role = $2 where email = $1`, email, role)
	if err != nil {
		return err
	}
	if ct.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// UpdateUser edits profile settings (nil = unchanged). consent: nil unchanged, true sets now, false clears.
func (d *DB) UpdateUser(ctx context.Context, id string, displayName, homeRegion *string, consent *bool) (*User, error) {
	var setConsent, clearConsent bool
	if consent != nil {
		setConsent, clearConsent = *consent, !*consent
	}
	return scanUser(d.Pool.QueryRow(ctx, `
update users set
  display_name = coalesce($2, display_name),
  home_region = coalesce($3, home_region),
  traces_consent_at = case when $4 then coalesce(traces_consent_at, now()) when $5 then null else traces_consent_at end
where id = $1::uuid returning `+userCols, id, displayName, homeRegion, setConsent, clearConsent))
}

// DeleteUser removes a user and every personal record (DPDP/GDPR erasure).
// Shortcuts they created stay (community data) but lose the creator link.
func (d *DB) DeleteUser(ctx context.Context, id string) error {
	ct, err := d.Pool.Exec(ctx, `delete from users where id = $1::uuid`, id)
	if err != nil {
		return err
	}
	if ct.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// SetTrust stores recomputed trust.
func (d *DB) SetTrust(ctx context.Context, id string, trust float64) error {
	_, err := d.Pool.Exec(ctx, `update users set trust = $2 where id = $1::uuid`, id, trust)
	return err
}

// --- sessions -------------------------------------------------------------------

// CreateSession stores a session token hash.
func (d *DB) CreateSession(ctx context.Context, userID string, tokenHash []byte, expires time.Time) error {
	_, err := d.Pool.Exec(ctx, `insert into sessions (user_id, token_hash, expires_at) values ($1::uuid, $2, $3)`, userID, tokenHash, expires)
	return err
}

// UserBySession resolves a live session.
func (d *DB) UserBySession(ctx context.Context, tokenHash []byte) (*User, error) {
	return scanUser(d.Pool.QueryRow(ctx, `
select `+prefixUser("u.")+` from sessions s join users u on u.id = s.user_id
where s.token_hash = $1 and s.expires_at > now()`, tokenHash))
}

func prefixUser(p string) string {
	return p + "id::text, " + p + "email::text, " + p + "display_name, " + p + "home_region, " + p + "trust, " + p + "role, " + p + "traces_consent_at, " + p + "created_at"
}

// DeleteSession revokes a session.
func (d *DB) DeleteSession(ctx context.Context, tokenHash []byte) error {
	_, err := d.Pool.Exec(ctx, `delete from sessions where token_hash = $1`, tokenHash)
	return err
}

// --- magic links ------------------------------------------------------------------

// CreateMagicLink stores a sign-in token hash.
func (d *DB) CreateMagicLink(ctx context.Context, email string, tokenHash []byte, expires time.Time) error {
	_, err := d.Pool.Exec(ctx, `insert into magic_links (email, token_hash, expires_at) values ($1, $2, $3)`, email, tokenHash, expires)
	return err
}

// CountMagicLinksRecent supports per-email throttling.
func (d *DB) CountMagicLinksRecent(ctx context.Context, email string, since time.Duration) (int, error) {
	var n int
	err := d.Pool.QueryRow(ctx, `select count(*) from magic_links where email = $1 and created_at > now() - $2::interval`, email, since.String()).Scan(&n)
	return n, err
}

// ConsumeMagicLink marks a token used and returns its email.
func (d *DB) ConsumeMagicLink(ctx context.Context, tokenHash []byte) (string, error) {
	var email string
	err := d.Pool.QueryRow(ctx, `
update magic_links set used_at = now()
where token_hash = $1 and used_at is null and expires_at > now()
returning email::text`, tokenHash).Scan(&email)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", ErrNotFound
	}
	return email, err
}

// CleanupAuth removes expired sessions and links.
func (d *DB) CleanupAuth(ctx context.Context) error {
	_, err := d.Pool.Exec(ctx, `delete from sessions where expires_at < now(); `)
	if err != nil {
		return err
	}
	_, err = d.Pool.Exec(ctx, `delete from magic_links where expires_at < now() - interval '1 day'`)
	return err
}

// --- places -----------------------------------------------------------------------

// Place is a saved Home or Work location.
type Place struct {
	Kind string  `json:"kind"`
	Lat  float64 `json:"lat"`
	Lng  float64 `json:"lng"`
}

// SetPlace saves or (with nil point) deletes a Home/Work place.
func (d *DB) SetPlace(ctx context.Context, userID, kind string, p *geo.Point) error {
	if p == nil {
		_, err := d.Pool.Exec(ctx, `delete from user_places where user_id = $1::uuid and kind = $2`, userID, kind)
		return err
	}
	_, err := d.Pool.Exec(ctx, `
insert into user_places (user_id, kind, geom) values ($1::uuid, $2, ST_SetSRID(ST_MakePoint($3, $4), 4326)::geography)
on conflict (user_id, kind) do update set geom = excluded.geom`, userID, kind, p.Lng, p.Lat)
	return err
}

// Places lists a user's saved places.
func (d *DB) Places(ctx context.Context, userID string) ([]Place, error) {
	rows, err := d.Pool.Query(ctx, `select kind, ST_Y(geom::geometry), ST_X(geom::geometry) from user_places where user_id = $1::uuid`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Place
	for rows.Next() {
		var p Place
		if err := rows.Scan(&p.Kind, &p.Lat, &p.Lng); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}
