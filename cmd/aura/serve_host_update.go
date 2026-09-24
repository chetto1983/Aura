package main

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/chetto1983/aura/internal/db"
	"github.com/chetto1983/aura/internal/db/sqlc"
	"github.com/chetto1983/aura/internal/hostupdate"
)

// hostUpdateActivityInterval is how often Aura tells the host updater whether anyone is using
// it. The updater reads an activity file older than three minutes as "Aura is down".
const hostUpdateActivityInterval = time.Minute

// hostUpdateActivity measures use across every channel for the host updater. Tool calls and
// job runs are read system-wide; conversations are owner-only (migration 0089), so each
// identity's newest activity is read inside its own identity transaction.
type hostUpdateActivity struct {
	pool       *pgxpool.Pool
	identities memoryIdentityLister
}

func (a hostUpdateActivity) Activity(ctx context.Context) (time.Time, int, error) {
	work, err := sqlc.New(a.pool).ApplianceWorkInFlight(ctx)
	if err != nil {
		return time.Time{}, 0, fmt.Errorf("host update activity: work in flight: %w", err)
	}
	last := timestamptzTime(work.LastWorkAt)
	rows, err := a.identities.ListIdentities(ctx)
	if err != nil {
		return time.Time{}, 0, fmt.Errorf("host update activity: list identities: %w", err)
	}
	for _, row := range rows {
		if row.Deactivated {
			continue
		}
		var seen pgtype.Timestamptz
		if err := db.WithIdentityTx(ctx, a.pool, row.ID, func(q *sqlc.Queries) error {
			seen, err = q.LatestConversationActivity(ctx)
			return err
		}); err != nil {
			return time.Time{}, 0, fmt.Errorf("host update activity: conversations of %s: %w", row.ID, err)
		}
		if t := timestamptzTime(seen); t.After(last) {
			last = t
		}
	}
	return last, int(work.LiveRuns), nil
}

func timestamptzTime(t pgtype.Timestamptz) time.Time {
	if !t.Valid {
		return time.Time{}
	}
	return t.Time
}

// buildHostUpdateWriter returns nil on a stack the host updater does not manage: the
// directory is bind-mounted only on an appliance, and without it there is no one to tell.
func buildHostUpdateWriter(dir string, pool *pgxpool.Pool, identities memoryIdentityLister) *hostupdate.ActivityWriter {
	if dir == "" || pool == nil {
		return nil
	}
	if info, err := os.Stat(dir); err != nil || !info.IsDir() {
		return nil
	}
	return hostupdate.NewActivityWriter(dir, hostUpdateActivity{pool: pool, identities: identities},
		hostUpdateActivityInterval, time.Now)
}
