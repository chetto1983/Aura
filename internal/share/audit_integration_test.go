//go:build db_integration

// audit_integration_test.go proves AuditWriter.Append against a real Postgres
// aura.share_audit (migration 0040). Split from store_integration_test.go (single concern
// per file, CLAUDE.md file-size discipline); shares that file's migratedPool/seedIdentity/
// seedConversation helpers (same package, same build tag).
package share

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
)

// TestShareAuditWriterAppend proves Append inserts a row readable back with the exact
// action/tier/detail supplied, and that two appends for the same link correlate by
// share_link_id (the create -> revoke lifecycle a real caller emits).
func TestShareAuditWriterAppend(t *testing.T) {
	pool := migratedPool(t)
	writer := NewAuditWriter(pool)
	ctx := context.Background()

	identityID := seedIdentity(t, pool)
	conv := seedConversation(t, pool, identityID)
	shareLinkID := uuid.Must(uuid.NewV7())
	convUUID := uuid.MustParse(conv)

	if err := writer.Append(ctx, identityID, shareLinkID, convUUID, AuditCreate, "internal", ""); err != nil {
		t.Fatalf("Append(create): %v", err)
	}
	if err := writer.Append(ctx, identityID, shareLinkID, convUUID, AuditRevoke, "internal", ""); err != nil {
		t.Fatalf("Append(revoke): %v", err)
	}

	rows, err := pool.Query(ctx,
		`SELECT action, tier, detail, created_at FROM aura.share_audit WHERE share_link_id = $1 ORDER BY created_at ASC`,
		shareLinkID)
	if err != nil {
		t.Fatalf("query share_audit: %v", err)
	}
	defer rows.Close()
	var actions []string
	for rows.Next() {
		var action, tier, detail string
		var createdAt time.Time
		if err := rows.Scan(&action, &tier, &detail, &createdAt); err != nil {
			t.Fatalf("scan: %v", err)
		}
		if tier != "internal" {
			t.Fatalf("tier = %q, want %q", tier, "internal")
		}
		if detail != "" {
			t.Fatalf("detail = %q, want empty (no PII)", detail)
		}
		if createdAt.IsZero() {
			t.Fatal("created_at is zero")
		}
		actions = append(actions, action)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate: %v", err)
	}
	if len(actions) != 2 || actions[0] != string(AuditCreate) || actions[1] != string(AuditRevoke) {
		t.Fatalf("actions = %v, want [create revoke] in insertion order", actions)
	}
}
