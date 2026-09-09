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
