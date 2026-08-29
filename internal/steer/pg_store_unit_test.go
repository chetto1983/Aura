package steer

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
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

// TestPushDelegationResultSharesPushValidation (51-11 Task 3) pins that
// PushDelegationResult runs through the SAME unexported push helper Push
// itself calls -- empty/oversize rejection happens identically for both
// entry points, never re-implemented a second time.
func TestPushDelegationResultSharesPushValidation(t *testing.T) {
	s := NewPostgresStore(nil, Config{Max: 8, MaxBytes: 64})
	if err := s.PushDelegationResult("conv", SourceWorker, "", "f-key"); !errors.Is(err, ErrEmpty) {
		t.Fatalf(`PushDelegationResult(text="") = %v, want ErrEmpty`, err)
	}
	body := strings.Repeat("x", 65)
	if err := s.PushDelegationResult("conv", SourceWorker, body, "f-key"); !errors.Is(err, ErrTooLarge) {
		t.Fatalf("PushDelegationResult(oversize) = %v, want ErrTooLarge", err)
	}
}

func TestPushDelegationResultRequiresFanoutKey(t *testing.T) {
	s := NewPostgresStore(nil, Config{Max: 8, MaxBytes: 64})
	for _, key := range []string{"", "   "} {
		if err := s.PushDelegationResult("conv", SourceWorker, "report", key); err == nil || !strings.Contains(err.Error(), "fan-out key is required") {
			t.Fatalf("PushDelegationResult(key=%q) = %v, want explicit key error", key, err)
		}
	}
}

// TestPushDelegationResultNilReceiverSafe mirrors Push's own nil-receiver
// guard ordering (a *PostgresStore boxed into swarm.SteerPublisher can be a
// non-nil interface wrapping a nil pointer).
func TestPushDelegationResultNilReceiverSafe(t *testing.T) {
	var s *PostgresStore
	if err := s.PushDelegationResult("conv", SourceWorker, "hi", "f-key"); err == nil {
		t.Fatal("PushDelegationResult on a nil *PostgresStore = nil, want a non-nil wiring error")
	}
}

// TestMarkFanoutNudgedUnconfiguredPool pins that a nil pool names itself
// unconfigured rather than panicking
// on the transaction.
func TestMarkFanoutNudgedUnconfiguredPool(t *testing.T) {
	s := NewPostgresStore(nil, Config{Max: 8, MaxBytes: 64})
	_, err := s.MarkFanoutNudged(context.Background(), "00000000-0000-0000-0000-000000000001", "f-key")
	if err == nil {
		t.Fatal("MarkFanoutNudged with a nil pool = nil, want a configuration error")
	}
	if !strings.Contains(err.Error(), "not configured") {
		t.Fatalf("MarkFanoutNudged with a nil pool = %v, want it to say the store is not configured", err)
	}
}

// TestMarkFanoutNudgedRejectsMalformedIdentity pins that a malformed identityID is named
// before ever reaching the transaction. Needs a non-nil pool to prove the
// guard runs before WithTx (pgxpool.New opens no connection); a nil pool
// would instead surface the earlier "not configured" branch.
func TestMarkFanoutNudgedRejectsMalformedIdentity(t *testing.T) {
	pool, err := pgxpool.New(context.Background(), "postgres://unused:unused@127.0.0.1:1/unused")
	if err != nil {
		t.Fatalf("pgxpool.New: %v", err)
	}
	defer pool.Close()

	s := NewPostgresStore(pool, Config{Max: 8, MaxBytes: 64})
	if _, err := s.MarkFanoutNudged(context.Background(), "not-a-uuid", "f-key"); err == nil {
		t.Fatal("MarkFanoutNudged(malformed identity) = nil, want a parse error")
	}
}

// TestPostgresStorePushUnconfiguredPool covers the wiring guard between the pure
// validation and the first pool access: a store built with no pool must name itself
// unconfigured rather than panic on the transaction. The body is deliberately valid, so
// the earlier ErrEmpty/ErrTooLarge returns cannot mask this branch.
func TestPostgresStorePushUnconfiguredPool(t *testing.T) {
	s := NewPostgresStore(nil, Config{Max: 8, MaxBytes: 64})
	err := s.Push("conv", "cockpit", "a real body")
	if err == nil {
		t.Fatal("Push with a nil pool = nil, want a configuration error")
	}
	if !strings.Contains(err.Error(), "not configured") {
		t.Fatalf("Push with a nil pool = %v, want it to say the store is not configured", err)
	}
}

// TestDiagnosePushRefusalNamesMalformedConversationID pins the ordering that makes the
// refusal diagnosis readable: a non-uuid conv is named here, before the owner lookup,
// so a caller never sees a server-side cast error standing in for a client mistake. The
// nil pool proves no query is reached.
func TestDiagnosePushRefusalNamesMalformedConversationID(t *testing.T) {
	s := NewPostgresStore(nil, Config{Max: 8, MaxBytes: 64})
	err := s.diagnosePushRefusal(context.Background(), "not-a-uuid")
	if err == nil {
		t.Fatal("diagnosePushRefusal(malformed) = nil, want a parse error")
	}
	if errors.Is(err, ErrQueueFull) {
		t.Fatal("diagnosePushRefusal(malformed) reported ErrQueueFull, want the parse error")
	}
	if !strings.Contains(err.Error(), "not a valid uuid") {
		t.Fatalf("diagnosePushRefusal error = %v, want it to name the invalid uuid", err)
	}
}

// TestUUIDStringOnInvalidValue pins the empty string for a NULL/unscanned uuid: Drain
// maps every row through uuidString, and a zero uuid rendered as the all-zero string
// would look like a real message id to a consumer.
func TestUUIDStringOnInvalidValue(t *testing.T) {
	if got := uuidString(pgtype.UUID{}); got != "" {
		t.Fatalf("uuidString(invalid) = %q, want the empty string", got)
	}
	id := pgtype.UUID{Bytes: [16]byte{0x01, 0x02, 0x03}, Valid: true}
	if got := uuidString(id); got == "" {
		t.Fatal("uuidString(valid) = empty, want the rendered uuid")
	}
}

// TestDrainRejectsMalformedConversationIDWithoutQuerying covers Drain's own uuid guard,
// which sits between the nil-pool check and the transaction. It needs a non-nil pool to
// be reached at all; pgxpool.New opens no connection (MinConns defaults to 0), so no
// database is involved and the guard returning before WithTx is what the empty result
// proves. Drain's contract is best-effort — a malformed conv degrades to "nothing to
// deliver", never to an error the agent has no channel to report.
func TestDrainRejectsMalformedConversationIDWithoutQuerying(t *testing.T) {
	pool, err := pgxpool.New(context.Background(), "postgres://unused:unused@127.0.0.1:1/unused")
	if err != nil {
		t.Fatalf("pgxpool.New: %v", err)
	}
	defer pool.Close() // stops the background health-check goroutine

	s := NewPostgresStore(pool, Config{Max: 8, MaxBytes: 64})
	if got := s.Drain("not-a-uuid"); len(got) != 0 {
		t.Fatalf("Drain(malformed) = %v, want no messages", got)
	}
}
