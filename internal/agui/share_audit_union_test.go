//go:build db_integration

// share_audit_union_test.go proves the Phase 37F D-14 4th union leg: a share_audit row for a
// provisioned identity surfaces in the SAME admin audit feed (ListActivityForIdentity) as the
// existing mcp/skill/tool ledgers, with Source == "share".
//
// No-skip-as-green: migratedPool/envOrSkip (server_integration_test.go) t.Fatal under $CI when
// the DSN is unset — a skipped integration test must never pass green. share_audit is
// append-only (grant, not trigger — see internal/db/migrations/0040), so cleanup deletes the
// seeded rows directly rather than relying on a fresh-identity-per-run convention.

package agui

import (
	"testing"
	"time"

	"github.com/google/uuid"
)

// TestAuditUnionIncludesShare proves the D-14 "surfaces in the existing admin audit UI"
// property: a share_audit row keyed on a provisioned identity comes back through
// PgAuditStore.ListActivityForIdentity with Source == "share" and the expected action/target.
func TestAuditUnionIncludesShare(t *testing.T) {
	pool := migratedPool(t)
	ctx := ownerCtx()

	identityID := uuid.Must(uuid.NewV7()).String()
	if _, err := pool.Exec(ctx,
		"INSERT INTO aura.identities (id, name, kind) VALUES ($1, $2, 'user')",
		identityID, "share-audit-union-"+identityID); err != nil {
		t.Fatalf("seed identity: %v", err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(ownerCtx(), "DELETE FROM aura.identities WHERE id = $1", identityID)
	})

	convID := uuid.Must(uuid.NewV7()).String()
	seedAsOwner(t, pool, identityID,
		"INSERT INTO aura.conversations (id, identity_id, model) VALUES ($1, $2, 'test-model')",
		convID, identityID)

	shareLinkID := uuid.Must(uuid.NewV7()).String()
	createdAt := time.Now().UTC().Truncate(time.Second)
	if _, err := pool.Exec(ctx,
		`INSERT INTO aura.share_audit (identity_id, share_link_id, conversation_id, action, tier, detail, created_at)
		 VALUES ($1, $2, $3, 'create', 'internal', '', $4)`,
		identityID, shareLinkID, convID, createdAt); err != nil {
		t.Fatalf("seed share_audit: %v", err)
	}

	store := NewPgAuditStore(pool)
	events, err := store.ListActivityForIdentity(ctx, identityID, 50, 0)
	if err != nil {
		t.Fatalf("ListActivityForIdentity: %v", err)
	}
	if len(events) != 1 {
		t.Fatalf("events = %d, want 1 (the seeded share_audit row)", len(events))
	}
	got := events[0]
	if got.Source != "share" {
		t.Fatalf("Source = %q, want %q", got.Source, "share")
	}
	if got.Action != "create" {
		t.Fatalf("Action = %q, want %q", got.Action, "create")
	}
	if got.Target != convID {
		t.Fatalf("Target = %q, want conversation id %q", got.Target, convID)
	}
	if got.Detail != "internal" {
		t.Fatalf("Detail = %q, want tier %q", got.Detail, "internal")
	}
}
