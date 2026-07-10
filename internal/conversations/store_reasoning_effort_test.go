//go:build db_integration

// Integration proofs for the Phase-37E per-conversation reasoning-effort persistence
// (WEBMODEL-01 / D-06 — Claude parity: persisted + restored on reopen). Requires a running
// Postgres with the migrations applied through 0032 (owner RLS):
//
//	make db-up && aura db migrate           # or the WSL equivalent
//	AURA_DB_URL + AURA_DB_MIGRATE_URL + POSTGRES_PASSWORD set in env
//
// Run via (against a THROWAWAY DB — db_integration mutations are NOT isolated per-run, so
// NEVER the live `aura` DB):
//
//	go test -tags db_integration -race -run TestReasoningEffort ./internal/conversations -count=1
//
// Reuses the store_test.go harness (envOrSkip / migratedPool / newStore / newConversation /
// localID) + the package goleak TestMain (main_test.go). No-skip-as-green: envOrSkip t.Fatals
// under $CI when the DSN is unset, so a skipped tier can never pass green.
package conversations

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

// seedIdentity inserts a user identity on the pool (aura.identities carries no RLS) and
// schedules its deletion (cascading its conversations via FK). Mirrors the db-package
// seedRLSIdentity so a SECOND, real owner exists for the cross-identity deny proof.
func seedIdentity(t *testing.T, pool *pgxpool.Pool, id, name string) {
	t.Helper()
	if _, err := pool.Exec(context.Background(),
		"INSERT INTO aura.identities (id, name, kind) VALUES ($1, $2, 'user') ON CONFLICT DO NOTHING",
		id, name); err != nil {
		t.Fatalf("seed identity %s: %v", name, err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), "DELETE FROM aura.identities WHERE id = $1", id)
	})
}

// TestReasoningEffortRoundTrip proves the D-06 persistence contract end-to-end at the store
// layer: the owner-scoped write lands in the metadata jsonb (no migration) and the read
// projection restores it on reopen, across the full symbol lifecycle
// "high" -> "auto" (switch-back is remembered, OQ-3) -> "" (cleared -> hydrates as auto).
func TestReasoningEffortRoundTrip(t *testing.T) {
	pool := migratedPool(t)
	s := newStore(t, pool)
	convID := newConversation(t, s) // owned by localID
	ctx := context.Background()

	// A fresh conversation has no persisted effort -> "" (the D-07 zero-regression default).
	if c, err := s.GetForIdentity(ctx, convID, localID); err != nil {
		t.Fatalf("GetForIdentity (initial): %v", err)
	} else if c.ReasoningEffort != "" {
		t.Errorf("new conversation effort = %q, want \"\" (auto default, D-07)", c.ReasoningEffort)
	}

	for _, want := range []string{"high", "auto", ""} {
		affected, err := s.UpdateReasoningEffortForIdentity(ctx, convID, localID, want)
		if err != nil {
			t.Fatalf("UpdateReasoningEffortForIdentity(%q): %v", want, err)
		}
		if affected != 1 {
			t.Fatalf("UpdateReasoningEffortForIdentity(%q) affected = %d, want 1 (owner writes its own row)", want, affected)
		}
		c, err := s.GetForIdentity(ctx, convID, localID)
		if err != nil {
			t.Fatalf("GetForIdentity after set %q: %v", want, err)
		}
		if c.ReasoningEffort != want {
			t.Errorf("effort round-trip: set %q, read back %q", want, c.ReasoningEffort)
		}
	}
}

// TestReasoningEffortForeignIdentityDenied proves T-37E-03-ISO: a DIFFERENT identity cannot
// write the owner's effort — the update affects 0 rows (the identity_id owner predicate plus
// the migration-0032 RLS backstop) and the owner's stored value is untouched. Mirrors the
// RenameForIdentity cross-identity isolation.
func TestReasoningEffortForeignIdentityDenied(t *testing.T) {
	pool := migratedPool(t)
	s := newStore(t, pool)
	convID := newConversation(t, s) // owned by localID
	ctx := context.Background()

	// The owner sets a known value first.
	if affected, err := s.UpdateReasoningEffortForIdentity(ctx, convID, localID, "high"); err != nil || affected != 1 {
		t.Fatalf("owner set effort: affected=%d err=%v (want 1, nil)", affected, err)
	}

	// A real second identity attempts to overwrite the owner's conversation.
	foreignID := uuid.Must(uuid.NewV7()).String()
	seedIdentity(t, pool, foreignID, "effort-foreign-"+foreignID[:8])

	affected, err := s.UpdateReasoningEffortForIdentity(ctx, convID, foreignID, "low")
	if err != nil {
		t.Fatalf("foreign UpdateReasoningEffortForIdentity: unexpected error %v (0 rows is not an error)", err)
	}
	if affected != 0 {
		t.Fatalf("foreign effort write affected = %d, want 0 (not owned — isolation broken)", affected)
	}

	// The owner's stored value is unchanged — the foreign write touched nothing.
	c, err := s.GetForIdentity(ctx, convID, localID)
	if err != nil {
		t.Fatalf("owner GetForIdentity: %v", err)
	}
	if c.ReasoningEffort != "high" {
		t.Errorf("foreign write mutated the owner's effort to %q, want \"high\" (unchanged)", c.ReasoningEffort)
	}
}
