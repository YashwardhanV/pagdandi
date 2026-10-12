// Package jobs holds background work shared by the worker and the API:
// confidence recomputation and the shortcut lifecycle, trust updates and
// retention purges.
package jobs

import (
	"context"
	"log/slog"
	"time"

	"pagdandi/api/internal/regions"
	"pagdandi/api/internal/shortcuts"
	"pagdandi/api/internal/store"
)

// Jobs bundles dependencies.
type Jobs struct {
	DB      *store.DB
	Regions *regions.Set
	Log     *slog.Logger
	Now     func() time.Time
}

func (j *Jobs) now() time.Time {
	if j.Now != nil {
		return j.Now()
	}
	return time.Now()
}

// RecomputeResult is the outcome for one shortcut.
type RecomputeResult struct {
	Confidence float64
	Status     string
	Changed    bool
}

// Recompute refreshes one shortcut's confidence (with the region's current
// season half-life) and applies the lifecycle rules.
func (j *Jobs) Recompute(ctx context.Context, row store.LifecycleRow) (RecomputeResult, error) {
	reports, err := j.DB.ReportsFor(ctx, row.ID)
	if err != nil {
		return RecomputeResult{}, err
	}
	now := j.now()
	half := shortcuts.DefaultHalfLifeDays
	if reg, ok := j.Regions.Get(row.RegionID); ok {
		half = reg.HalfLifeDays(now)
	}
	a, b, conf := shortcuts.Confidence(reports, now, half)
	next := shortcuts.NextStatus(shortcuts.LifecycleInput{
		Status: row.Status, CreatedBy: row.CreatedBy, CreatorTrust: row.CreatorTrust,
		CreatedAt: row.CreatedAt, Reports: reports, Confidence: conf, Now: now,
	})
	var lastConfirmed *time.Time
	for i := range reports {
		if reports[i].Verdict == shortcuts.VerdictPassable && (lastConfirmed == nil || reports[i].CreatedAt.After(*lastConfirmed)) {
			t := reports[i].CreatedAt
			lastConfirmed = &t
		}
	}
	if err := j.DB.SaveShortcutState(ctx, row.ID, a, b, conf, next, lastConfirmed); err != nil {
		return RecomputeResult{}, err
	}
	return RecomputeResult{Confidence: conf, Status: next, Changed: next != row.Status}, nil
}

// RecomputeOne recomputes a shortcut by id.
func (j *Jobs) RecomputeOne(ctx context.Context, id string) (RecomputeResult, error) {
	s, err := j.DB.GetShortcut(ctx, id)
	if err != nil {
		return RecomputeResult{}, err
	}
	creator := ""
	trust := 1.0
	if s.CreatedBy != nil {
		creator = *s.CreatedBy
		trust, _ = j.DB.UserTrust(ctx, creator)
	}
	return j.Recompute(ctx, store.LifecycleRow{ID: s.ID, RegionID: s.RegionID, Status: s.Status, CreatedBy: creator, CreatorTrust: trust, CreatedAt: s.CreatedAt})
}

// Lifecycle runs the hourly confidence decay and status pass (SPEC §7.4).
func (j *Jobs) Lifecycle(ctx context.Context) error {
	rows, err := j.DB.ShortcutsForLifecycle(ctx)
	if err != nil {
		return err
	}
	changed := 0
	for _, r := range rows {
		res, err := j.Recompute(ctx, r)
		if err != nil {
			j.Log.Warn("recompute failed", "shortcut", r.ID, "err", err)
			continue
		}
		if res.Changed {
			changed++
			j.Log.Info("shortcut status changed", "shortcut", r.ID, "from", r.Status, "to", res.Status, "confidence", res.Confidence)
		}
	}
	j.Log.Info("lifecycle pass", "shortcuts", len(rows), "changed", changed)
	return nil
}

// Trust recomputes every reporter's trust from agreement with consensus (SPEC §7.5).
func (j *Jobs) Trust(ctx context.Context) error {
	ags, err := j.DB.TrustAgreement(ctx)
	if err != nil {
		return err
	}
	for _, a := range ags {
		if err := j.DB.SetTrust(ctx, a.UserID, shortcuts.TrustFromHistory(a.Agree, a.Disagree)); err != nil {
			return err
		}
	}
	j.Log.Info("trust pass", "users", len(ags))
	return nil
}

// Retention deletes expired traces and auth rows.
func (j *Jobs) Retention(ctx context.Context) error {
	n, err := j.DB.PurgeExpired(ctx)
	if err != nil {
		return err
	}
	if err := j.DB.CleanupAuth(ctx); err != nil {
		return err
	}
	j.Log.Info("retention pass", "traces_deleted", n)
	return nil
}
