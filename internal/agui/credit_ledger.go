package agui

// credit_ledger.go is the per-identity period-spend read behind CRED-06 and D-08's
// "display and refuse" half. It sums Aura's own in-band ledger
// (aura.cache_metrics.cost_usd, joined to aura.conversations for the identity scope)
// rather than reading OpenRouter's own GET /api/v1/key counter, which lags a spend by
// 30-40s (M-07) and is reconciliation, never the live balance (D-08).
//
// The window start is derived from the identity's own stored reset interval
// (daily/weekly/monthly, D-10) rather than assumed monthly, so a weekly- or
// daily-reset identity reads the right window (CRED-06 "Ordering" probe).
//
// Nothing here rounds. The aggregate is read at the full stored scale (numeric(24,12)
// as of migration 0124); the display rounding rule belongs to credit_api.go, the
// caller closer to the wire (CRED-06 "Precision" probe) -- rounding twice is how a
// displayed total stops matching the sum of its parts.

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/chetto1983/aura/internal/db"
	"github.com/chetto1983/aura/internal/db/sqlc"
	"github.com/chetto1983/aura/internal/pgnumeric"
)

// PgSpendReader reads the period spend from aura.cache_metrics over a live pool.
type PgSpendReader struct {
	pool *pgxpool.Pool
}

// NewPgSpendReader builds a PgSpendReader over an open pool.
func NewPgSpendReader(pool *pgxpool.Pool) *PgSpendReader {
	return &PgSpendReader{pool: pool}
}

// PeriodSpend sums aura.cache_metrics.cost_usd for identityID since the start of its
// CURRENT reset window (derived from limitReset). Scoped to the SUBJECT of the read
// (identityctx.WithIdentityID), never the caller -- migration 0032's
// conversations_owner_isolation RLS policy filters the aura.conversations join to
// app.current_identity, and scoping to an ADMIN caller inspecting someone else would
// silently lose every row belonging to the identity being inspected. Mirrors
// internal/agui/audit_store.go's ListActivityForIdentity, which states the identical
// trap and the identical fix. Returns an exact float64 for an identity with no rows
// (0, nil error), never an error for the empty case.
func (r *PgSpendReader) PeriodSpend(ctx context.Context, identityID, limitReset string) (float64, error) {
	if r == nil || r.pool == nil {
		return 0, fmt.Errorf("credit_ledger: nil spend reader")
	}
	identityID = strings.TrimSpace(identityID)
	if identityID == "" {
		return 0, fmt.Errorf("credit_ledger: empty identity id")
	}
	since := windowStart(time.Now().UTC(), limitReset)
	idParam, err := parseIdentityUUID(identityID)
	if err != nil {
		return 0, err
	}
	// WithIdentityTx takes identityID as an explicit argument and sets
	// app.current_identity from IT, not from ctx (internal/db/tx.go) -- so scoping to
	// the SUBJECT of this read is just passing identityID here, the same identityID
	// this function was called with, never the caller's own principal.
	var raw any
	err = db.WithIdentityTx(ctx, r.pool, identityID, func(q *sqlc.Queries) error {
		var qerr error
		raw, qerr = q.SumIdentitySpendSince(ctx, sqlc.SumIdentitySpendSinceParams{
			IdentityID: idParam,
			Since:      pgtype.Timestamptz{Time: since, Valid: true},
		})
		return qerr
	})
	if err != nil {
		return 0, fmt.Errorf("credit_ledger: period spend for %s: %w", identityID, err)
	}
	spend, err := anyCostFloat(raw)
	if err != nil {
		return 0, fmt.Errorf("credit_ledger: decode period spend for %s: %w", identityID, err)
	}
	return spend, nil
}

// windowStart returns the UTC instant marking the beginning of the CURRENT reset
// window for limitReset ("daily"/"weekly"/"monthly"), evaluated at now. This is a
// fixed calendar boundary (UTC day/ISO-week(Monday)/month start), never a rolling
// now-minus-duration window: OpenRouter's own limit_reset semantics reset at a
// calendar boundary (02-OPENROUTER-API.md), and a rolling window would show a
// different "period spend" than the provider's own cap resets against. An
// unrecognized or empty value falls back to monthly, matching D-10's own default and
// identitykey.Store.Save's "monthly" fallback for the same field.
func windowStart(now time.Time, limitReset string) time.Time {
	now = now.UTC()
	dayStart := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
	switch strings.ToLower(strings.TrimSpace(limitReset)) {
	case "daily":
		return dayStart
	case "weekly":
		// ISO week starts Monday; time.Weekday's Sunday=0 needs remapping so Monday
		// maps to offset 0.
		offset := (int(dayStart.Weekday()) + 6) % 7
		return dayStart.AddDate(0, 0, -offset)
	default: // "monthly" and any unrecognized value.
		return time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC)
	}
}

// anyCostFloat coerces a sqlc `coalesce(sum(numeric),0)` result to float64, mirroring
// internal/cachemetrics/store_helpers.go's anyNumericFloat (a different package, same
// discipline: an unparseable text shape or an unmodeled driver type is an error, never
// a silently-fabricated 0 -- a "$0.00 spend" that is actually an unrecognized decode
// shape reads as "this identity is free", which is the exact false-green this ledger
// exists to prevent, WR-02).
func anyCostFloat(v any) (float64, error) {
	switch n := v.(type) {
	case pgtype.Numeric:
		return pgnumeric.FloatFromNumeric(n), nil
	case float64:
		return n, nil
	case int64:
		return float64(n), nil
	case string:
		f, err := strconv.ParseFloat(n, 64)
		if err != nil {
			return 0, fmt.Errorf("decode float aggregate from string %q: %w", n, err)
		}
		return f, nil
	case []byte:
		f, err := strconv.ParseFloat(string(n), 64)
		if err != nil {
			return 0, fmt.Errorf("decode float aggregate from bytes %q: %w", n, err)
		}
		return f, nil
	default:
		return 0, fmt.Errorf("unmodeled float aggregate shape %T", v)
	}
}

// parseIdentityUUID parses identityID into the pgtype.UUID SumIdentitySpendSince's
// $1::uuid cast needs, wrapping the error with which identity failed to parse (the
// same convention internal/identitykey.parseUUID uses).
func parseIdentityUUID(identityID string) (pgtype.UUID, error) {
	var out pgtype.UUID
	if err := out.Scan(identityID); err != nil {
		return pgtype.UUID{}, fmt.Errorf("credit_ledger: identity %q is not a uuid: %w", identityID, err)
	}
	return out, nil
}
