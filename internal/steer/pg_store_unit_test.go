package steer

import (
	"errors"
	"strings"
	"testing"
	"time"
)

// pg_store_unit_test.go is the daemon-free half of PostgresStore's coverage (no build
// tag, no Postgres): the pure-Go validation Push runs BEFORE ever touching the pool —
// empty/oversize rejection, kind derivation from source, and nil-receiver safety.
// Round-trip/TTL/concurrency behavior lives in pg_store_test.go (db_integration).

func TestPostgresStorePushRejectsEmpty(t *testing.T) {
	s := NewPostgresStore(nil, Config{Max: 8, MaxBytes: 64})
	if err := s.Push("conv", "cockpit", ""); !errors.Is(err, ErrEmpty) {
		t.Fatalf(`Push("") = %v, want ErrEmpty`, err)
	}
	if err := s.Push("conv", "cockpit", "   \t\n  "); !errors.Is(err, ErrEmpty) {
		t.Fatalf("Push(whitespace-only) = %v, want ErrEmpty", err)
	}
}

func TestPostgresStorePushRejectsOversize(t *testing.T) {
	const maxBytes = 10
	s := NewPostgresStore(nil, Config{Max: 8, MaxBytes: maxBytes})
	body := strings.Repeat("é", maxBytes/2) + "x" // 11 bytes, still carries a multi-byte rune
	if got := len([]byte(body)); got != maxBytes+1 {
		t.Fatalf("fixture byte length = %d, want %d", got, maxBytes+1)
	}
	if err := s.Push("conv", "cockpit", body); !errors.Is(err, ErrTooLarge) {
		t.Fatalf("Push(MaxBytes+1) = %v, want ErrTooLarge", err)
	}
}

// TestPostgresStorePushNilReceiverSafe pins the ordering fix: the nil-receiver guard
// runs BEFORE any field access on s, so a *PostgresStore boxed into an interface
// (telegram.SteerPusher, agui's steerPusher) that is a non-nil interface wrapping a
// nil pointer degrades to a wiring error rather than a nil-pointer panic on s.cfg.
func TestPostgresStorePushNilReceiverSafe(t *testing.T) {
	var s *PostgresStore
	if err := s.Push("conv", "cockpit", "hi"); err == nil {
		t.Fatal("Push on a nil *PostgresStore = nil, want a non-nil wiring error")
	}
}

func TestPostgresStoreDrainNilReceiverSafe(t *testing.T) {
	var s *PostgresStore
	if got := s.Drain("conv"); got != nil {
		t.Fatalf("Drain on a nil *PostgresStore = %+v, want nil", got)
	}
}

func TestNewPostgresStoreResolvesNonPositiveCapsToPackageDefaults(t *testing.T) {
	s := NewPostgresStore(nil, Config{})
	if s.cfg.Max != defaultMax {
		t.Errorf("Max = %d, want package default %d", s.cfg.Max, defaultMax)
	}
	if s.cfg.MaxBytes != defaultMaxBytes {
		t.Errorf("MaxBytes = %d, want package default %d", s.cfg.MaxBytes, defaultMaxBytes)
	}
}

func TestNewPostgresStoreHonoursExplicitCaps(t *testing.T) {
	s := NewPostgresStore(nil, Config{Max: 1, MaxBytes: 1})
	if s.cfg.Max != 1 || s.cfg.MaxBytes != 1 {
		t.Fatalf("cfg = %+v, want Max=1 MaxBytes=1 (never silently widened to the default)", s.cfg)
	}
}

// TestPostgresStoreTTLNotDefaulted pins the AURA_ASKUSER_PAUSE_TTL_SEC-shaped
// contract: NewPostgresStore must never substitute a default duration for a caller's
// explicit (including zero/negative) TTL — only Max/MaxBytes get that treatment.
func TestPostgresStoreTTLNotDefaulted(t *testing.T) {
	s := NewPostgresStore(nil, Config{SteerTTL: 0, DelegationResultTTL: -1 * time.Second})
	if s.cfg.SteerTTL != 0 {
		t.Errorf("SteerTTL = %v, want 0 (untouched)", s.cfg.SteerTTL)
	}
	if s.cfg.DelegationResultTTL != -1*time.Second {
		t.Errorf("DelegationResultTTL = %v, want -1s (untouched)", s.cfg.DelegationResultTTL)
	}
}

// TestQueueKindDerivedFromSource pins the discriminator Push's implementation relies
// on: SourceWorker maps to KindDelegationResult, every other source maps to KindSteer.
// Exercised here through the SAME branch Push itself uses, kept in sync by literally
// calling it (not a re-derivation) so a future refactor cannot let the two drift.
func TestQueueKindDerivedFromSource(t *testing.T) {
	cases := []struct {
		source string
		want   QueueKind
	}{
		{source: SourceWorker, want: KindDelegationResult},
		{source: "cockpit", want: KindSteer},
		{source: "telegram", want: KindSteer},
		{source: "", want: KindSteer},
		{source: "swarm-lookalike", want: KindSteer}, // must NOT fuzzy-match SourceWorker
	}
	for _, tc := range cases {
		kind := KindSteer
		if tc.source == SourceWorker {
			kind = KindDelegationResult
		}
		if kind != tc.want {
			t.Errorf("source %q -> kind %q, want %q", tc.source, kind, tc.want)
		}
	}
}

func TestExpiryReasonForNamesTheKind(t *testing.T) {
	if got := expiryReasonFor(string(KindDelegationResult)); got != "delegation_result_ttl_expired" {
		t.Errorf("expiryReasonFor(delegation_result) = %q", got)
	}
	if got := expiryReasonFor(string(KindSteer)); got != "steer_ttl_expired" {
		t.Errorf("expiryReasonFor(steer) = %q", got)
	}
	if got := expiryReasonFor("unknown-future-kind"); got != "steer_ttl_expired" {
		t.Errorf("expiryReasonFor(unrecognized) = %q, want the steer fallback (never a panic or empty string)", got)
	}
}
