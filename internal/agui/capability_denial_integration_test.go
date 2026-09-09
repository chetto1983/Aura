//go:build db_integration

// capability_denial_integration_test.go proves aura.capability_denials' RLS scoping
// (02-04 Task 1, RBAC-09/RBAC-10) against a LIVE Postgres — the raw pgx write in
// capability_denial_store.go is not exercisable by the unit tier. Task 2 appends the
// audit-feed UNION-leg proofs (TestAuditFeed*) to this same file, reusing the pool +
// helpers set up here.
//
// Run via:
//
//	go test -tags db_integration -race -count=1 -p 1 ./internal/agui -run TestCapabilityDenialsRLSAndScope -v
//
// No-skip-as-green: migratedPool/envOrSkip (server_integration_test.go) t.Fatal under
// $CI when the DSN is unset — a skipped integration test must never pass green.
package agui

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/chetto1983/aura/internal/db"
)

// readCapabilityDenialCauses reads the recorded causes for identityID, RLS-scoped to that
// SAME identity — mirroring onboarding_provision_grants_test.go's liveGrantedCapabilities.
func readCapabilityDenialCauses(t *testing.T, pool *pgxpool.Pool, identityID string) []string {
	t.Helper()
	var got []string
	if err := db.WithIdentityTxRaw(ownerCtx(), pool, identityID, func(tx pgx.Tx) error {
		rows, err := tx.Query(ownerCtx(),
			`SELECT cause FROM aura.capability_denials WHERE identity_id = $1 ORDER BY created_at`, identityID)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var c string
			if err := rows.Scan(&c); err != nil {
				return err
			}
			got = append(got, c)
		}
		return rows.Err()
	}); err != nil {
		t.Fatalf("read capability denials for %s: %v", identityID, err)
	}
	return got
}

// TestCapabilityDenialsRLSAndScope proves identity A's denials are not readable while
// scoped to identity B, even when the query explicitly names A's identity_id in its WHERE
// clause — the RLS policy filters the row away before the WHERE clause is evaluated, not
// merely the application-level query shape.
func TestCapabilityDenialsRLSAndScope(t *testing.T) {
	pool := migratedPool(t)
	store := NewPgCapabilityDenialStore(pool)

	idA := uuid.Must(uuid.NewV7()).String()
	idB := uuid.Must(uuid.NewV7()).String()
	for _, id := range []string{idA, idB} {
		if _, err := pool.Exec(ownerCtx(),
			"INSERT INTO aura.identities (id, name, kind) VALUES ($1, $2, 'user')",
			id, "denial-rls-"+id); err != nil {
			t.Fatalf("seed identity %s: %v", id, err)
		}
	}

	if err := store.RecordDenial(ownerCtx(), idA, "identity.create", "POST /api/onboarding/start", DenialCauseNotHeld); err != nil {
		t.Fatalf("RecordDenial(A): %v", err)
	}
	if err := store.RecordDenial(ownerCtx(), idB, "identity.delete", "DELETE /api/admin/identities/{id}", DenialCauseNotHeld); err != nil {
		t.Fatalf("RecordDenial(B): %v", err)
	}

	if got := readCapabilityDenialCauses(t, pool, idA); len(got) != 1 {
		t.Fatalf("A's own denials = %v, want exactly 1", got)
	}
	if got := readCapabilityDenialCauses(t, pool, idB); len(got) != 1 {
		t.Fatalf("B's own denials = %v, want exactly 1", got)
	}

	// The cross-identity proof: scoped as B, an explicit WHERE identity_id = A must still
	// return zero rows — RLS, not the app-level filter, is what makes A invisible.
	var crossRows []string
	if err := db.WithIdentityTxRaw(ownerCtx(), pool, idB, func(tx pgx.Tx) error {
		rows, err := tx.Query(ownerCtx(), `SELECT cause FROM aura.capability_denials WHERE identity_id = $1`, idA)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var c string
			if err := rows.Scan(&c); err != nil {
				return err
			}
			crossRows = append(crossRows, c)
		}
		return rows.Err()
	}); err != nil {
		t.Fatalf("cross-identity read: %v", err)
	}
	if len(crossRows) != 0 {
		t.Fatalf("identity B's scope read identity A's denial: %v (RLS not enforced)", crossRows)
	}
}

// --- 02-04 Task 2: the admin audit feed's fifth ('capability') UNION leg ---

// seedIdentity inserts a fresh identity row and returns its uuid — the same shape
// audit_store_integration_test.go's TestPgAuditStoreListActivityForIdentity uses.
func seedIdentity(t *testing.T, pool *pgxpool.Pool, label string) string {
	t.Helper()
	id := uuid.Must(uuid.NewV7()).String()
	if _, err := pool.Exec(ownerCtx(),
		"INSERT INTO aura.identities (id, name, kind) VALUES ($1, $2, 'user')",
		id, label+"-"+id); err != nil {
		t.Fatalf("seed identity %s: %v", label, err)
	}
	return id
}

// TestAuditFeedIncludesCapabilityDenials proves the fifth UNION leg: after two denials
// for identity B, ListActivityForIdentity(B) surfaces them with Source == "capability",
// Action naming the cause and Target naming the capability — and the pre-existing mcp leg
// still returns its own row, unchanged, in the same call.
func TestAuditFeedIncludesCapabilityDenials(t *testing.T) {
	pool := migratedPool(t)
	ctx := ownerCtx()
	idB := seedIdentity(t, pool, "audit-feed-capdenial")

	if _, err := pool.Exec(ctx,
		"INSERT INTO aura.mcp_audit (actor_identity_id, action, server_name, created_at) VALUES ($1,'install','srv-audit-feed',$2)",
		idB, time.Now().UTC().Add(-5*time.Minute)); err != nil {
		t.Fatalf("seed mcp_audit: %v", err)
	}

	denialStore := NewPgCapabilityDenialStore(pool)
	if err := denialStore.RecordDenial(ctx, idB, "identity.create", "POST /api/onboarding/start", DenialCauseNotHeld); err != nil {
		t.Fatalf("RecordDenial 1: %v", err)
	}
	if err := denialStore.RecordDenial(ctx, idB, "identity.delete", "DELETE /api/admin/identities/{id}", DenialCauseNotHeld); err != nil {
		t.Fatalf("RecordDenial 2: %v", err)
	}

	auditStore := NewPgAuditStore(pool)
	events, err := auditStore.ListActivityForIdentity(ctx, idB, 50, 0)
	if err != nil {
		t.Fatalf("ListActivityForIdentity: %v", err)
	}

	var capEvents, mcpEvents int
	for _, e := range events {
		switch e.Source {
		case "capability":
			capEvents++
			if e.Action != DenialCauseNotHeld {
				t.Fatalf("capability event Action = %q, want the cause %q", e.Action, DenialCauseNotHeld)
			}
			if e.Target != "identity.create" && e.Target != "identity.delete" {
				t.Fatalf("capability event Target = %q, want a capability name", e.Target)
			}
		case "mcp":
			mcpEvents++
		}
	}
	if capEvents != 2 {
		t.Fatalf("capability events = %d, want 2", capEvents)
	}
	if mcpEvents != 1 {
		t.Fatalf("pre-existing mcp leg events = %d, want 1 (unchanged by the new leg)", mcpEvents)
	}
}

// TestAuditFeedDenialsForOtherIdentityNotVisible proves cross-identity deny on the new
// leg: identity A's feed contains none of B's denials.
func TestAuditFeedDenialsForOtherIdentityNotVisible(t *testing.T) {
	pool := migratedPool(t)
	ctx := ownerCtx()
	idA := seedIdentity(t, pool, "audit-feed-cross-a")
	idB := seedIdentity(t, pool, "audit-feed-cross-b")

	denialStore := NewPgCapabilityDenialStore(pool)
	if err := denialStore.RecordDenial(ctx, idB, "governance.write", "POST /api/admin/identities", DenialCauseNotHeld); err != nil {
		t.Fatalf("RecordDenial(B): %v", err)
	}

	auditStore := NewPgAuditStore(pool)
	events, err := auditStore.ListActivityForIdentity(ctx, idA, 50, 0)
	if err != nil {
		t.Fatalf("ListActivityForIdentity(A): %v", err)
	}
	for _, e := range events {
		if e.Source == "capability" {
			t.Fatalf("identity A's feed leaked identity B's capability denial: %+v", e)
		}
	}
}

// TestAuditFeedEmptyDenialsIsEmptyList proves the RBAC-10 "empty" edge: an identity with
// no denials (and no other activity) returns a zero-length slice and a nil error, the
// same shape ListActivityForIdentity already returns for an identity with no activity.
func TestAuditFeedEmptyDenialsIsEmptyList(t *testing.T) {
	pool := migratedPool(t)
	ctx := ownerCtx()
	idEmpty := seedIdentity(t, pool, "audit-feed-empty")

	auditStore := NewPgAuditStore(pool)
	events, err := auditStore.ListActivityForIdentity(ctx, idEmpty, 50, 0)
	if err != nil {
		t.Fatalf("ListActivityForIdentity: %v", err)
	}
	if events == nil {
		t.Fatal("events is nil, want a non-nil zero-length slice")
	}
	if len(events) != 0 {
		t.Fatalf("events = %d, want 0 for an identity with no denials", len(events))
	}
}

// TestAuditFeedDenialsAreDistinctRows proves the RBAC-10 "adjacency" edge: two denials
// differing only in capability (identity.create vs identity.delete) read back as two
// distinct rows — the feed does not collapse them.
func TestAuditFeedDenialsAreDistinctRows(t *testing.T) {
	pool := migratedPool(t)
	ctx := ownerCtx()
	id := seedIdentity(t, pool, "audit-feed-adjacency")

	denialStore := NewPgCapabilityDenialStore(pool)
	if err := denialStore.RecordDenial(ctx, id, "identity.create", "POST /api/onboarding/start", DenialCauseNotHeld); err != nil {
		t.Fatalf("RecordDenial(create): %v", err)
	}
	if err := denialStore.RecordDenial(ctx, id, "identity.delete", "POST /api/onboarding/start", DenialCauseNotHeld); err != nil {
		t.Fatalf("RecordDenial(delete): %v", err)
	}

	auditStore := NewPgAuditStore(pool)
	events, err := auditStore.ListActivityForIdentity(ctx, id, 50, 0)
	if err != nil {
		t.Fatalf("ListActivityForIdentity: %v", err)
	}
	seen := map[string]bool{}
	for _, e := range events {
		if e.Source == "capability" {
			seen[e.Target] = true
		}
	}
	if !seen["identity.create"] || !seen["identity.delete"] {
		t.Fatalf("expected two distinct capability rows (create+delete), got: %v", seen)
	}
	if len(seen) != 2 {
		t.Fatalf("capability rows collapsed: %v", seen)
	}
}

// TestAuditFeedOrderIsDeterministicAtEqualTimestamps proves the RBAC-10 "ordering" edge:
// two rows written inside the same timestamp tick come back in a STABLE order across
// repeated reads — a refresh must not shuffle the feed. Inserted directly (not through
// RecordDenial, which always stamps now()) so both rows share one explicit created_at.
func TestAuditFeedOrderIsDeterministicAtEqualTimestamps(t *testing.T) {
	pool := migratedPool(t)
	ctx := ownerCtx()
	id := seedIdentity(t, pool, "audit-feed-order")
	tie := time.Now().UTC().Truncate(time.Second)

	for _, cap := range []string{"identity.create", "identity.delete"} {
		seedAsOwner(t, pool, id,
			"INSERT INTO aura.capability_denials (identity_id, capability, route, cause, created_at) VALUES ($1,$2,'POST /agent/run','not_held',$3)",
			id, cap, tie)
	}

	auditStore := NewPgAuditStore(pool)
	first, err := auditStore.ListActivityForIdentity(ctx, id, 50, 0)
	if err != nil {
		t.Fatalf("ListActivityForIdentity (first read): %v", err)
	}
	second, err := auditStore.ListActivityForIdentity(ctx, id, 50, 0)
	if err != nil {
		t.Fatalf("ListActivityForIdentity (second read): %v", err)
	}
	firstOrder := capabilityTargetsInOrder(first)
	secondOrder := capabilityTargetsInOrder(second)
	if len(firstOrder) != 2 {
		t.Fatalf("first read capability rows = %v, want 2", firstOrder)
	}
	if !slicesEqual(firstOrder, secondOrder) {
		t.Fatalf("order shuffled between reads: first=%v second=%v", firstOrder, secondOrder)
	}
}

func capabilityTargetsInOrder(events []AuditEvent) []string {
	var out []string
	for _, e := range events {
		if e.Source == "capability" {
			out = append(out, e.Target)
		}
	}
	return out
}

func slicesEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
