package askuser

import (
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
)

// TestPauseExpired_PureComparison pins the D-12/D-13 lazy-expiry predicate — daemon-free
// (no Postgres) so this package's non-db_integration floor covers it. Disabled TTL and a
// NULL created_at both read as "not expired"; otherwise expiry fires at exactly ttlSec,
// never before.
func TestPauseExpired_PureComparison(t *testing.T) {
	now := time.Date(2026, 8, 28, 12, 0, 0, 0, time.UTC)
	valid := func(age time.Duration) pgtype.Timestamptz {
		return pgtype.Timestamptz{Time: now.Add(-age), Valid: true}
	}

	cases := []struct {
		name      string
		createdAt pgtype.Timestamptz
		ttlSec    int
		want      bool
	}{
		{"ttl disabled (zero)", valid(1 * time.Hour), 0, false},
		{"ttl disabled (negative)", valid(1 * time.Hour), -5, false},
		{"created_at NULL, ttl enabled", pgtype.Timestamptz{Valid: false}, 10, false},
		{"fresh row, well under ttl", valid(1 * time.Second), 60, false},
		{"exactly at ttl boundary", valid(60 * time.Second), 60, true},
		{"past ttl", valid(120 * time.Second), 60, true},
		{"one second under ttl boundary", valid(59 * time.Second), 60, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := pauseExpired(tc.createdAt, tc.ttlSec, now); got != tc.want {
				t.Errorf("pauseExpired(%+v, ttlSec=%d) = %v, want %v", tc.createdAt, tc.ttlSec, got, tc.want)
			}
		})
	}
}

// TestNewWithPauseTTL_StoreExposesPauseExpired proves the Store-bound wrapper threads
// the configured TTL and the real clock into the same pure comparison, without a pool
// round-trip (nil pool — pauseExpired never touches it).
func TestNewWithPauseTTL_StoreExposesPauseExpired(t *testing.T) {
	disabled := New(nil)
	stale := pgtype.Timestamptz{Time: time.Now().Add(-24 * time.Hour), Valid: true}
	if disabled.pauseExpired(stale) {
		t.Fatal("New(pool) must leave lazy expiry disabled (ttlSec=0)")
	}

	enabled := NewWithPauseTTL(nil, 1)
	if !enabled.pauseExpired(stale) {
		t.Fatal("NewWithPauseTTL(pool, 1) must treat a 24h-old row as expired")
	}
	fresh := pgtype.Timestamptz{Time: time.Now(), Valid: true}
	if enabled.pauseExpired(fresh) {
		t.Fatal("NewWithPauseTTL(pool, 1) must not expire a just-created row")
	}
}
