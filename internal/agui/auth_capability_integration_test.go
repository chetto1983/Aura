//go:build db_integration

// Integration test for the WEB-03 capability gate (D-04) against the REAL seeded
// `local` identity over a Postgres-backed identity.Store. It confirms the dormant
// capability_grants scaffolding (migration 0004) is actually exercised end-to-end:
// the seeded `local` identity holds the `*` wildcard, so HasCapability(local,
// "agent.run") is true, and RequireCapability lets the request through to next; an
// identity with no matching grant gets 403.
//
// Requires a running Postgres with the migrations applied:
//
//	make db-up && aura db migrate
//	AURA_DB_URL + AURA_DB_MIGRATE_URL + POSTGRES_PASSWORD set in env
//
// Run via:
//
//	go test -tags db_integration -race -p 1 ./internal/agui -run TestAgentRunCapability -count=1
//
// No-skip-as-green: the shared envOrSkip (server_integration_test.go) t.Fatals under
// $CI when the DSN is unset — a skipped integration test must never pass green.
package agui

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/chetto1983/aura/internal/identity"
)

// storeChecker bridges *identity.Store onto the agui consumer-side identityChecker
// seam (identity.Identity → agui.Identity on GetIdentityByID; HasCapability passes
// through). It mirrors cmd/aura's identityCheckerAdapter so the real store drives the
// gate in this integration test.
type storeChecker struct{ store *identity.Store }

func (c storeChecker) GetIdentityByID(ctx context.Context, id string) (Identity, error) {
	idn, err := c.store.GetIdentityByID(ctx, id)
	if err != nil {
		return Identity{}, err
	}
	return Identity{ID: idn.ID, Name: idn.Name, Kind: idn.Kind}, nil
}

func (c storeChecker) HasCapability(ctx context.Context, id, capability string) (bool, error) {
	return c.store.HasCapability(ctx, id, capability)
}

// TestAgentRunCapability exercises RequireCapability over the REAL seeded local
// identity: the `*` wildcard (migration 0004) makes HasCapability(local, "agent.run")
// true so the gate passes to next; a fresh identity with no grant is 403'd.
func TestAgentRunCapability(t *testing.T) {
	pool := migratedPool(t)
	store := identity.New(pool)
	ctx := ownerCtx()

	// The seeded local identity holds the `*` wildcard: HasCapability is true for ANY
	// capability name, including the agent.run gate name.
	ok, err := store.HasCapability(ctx, localIdentityID, "agent.run")
	if err != nil {
		t.Fatalf("HasCapability(local, agent.run): %v", err)
	}
	if !ok {
		t.Fatal("seeded local identity does not pass HasCapability via the * wildcard (migration 0004 regression)")
	}

	deps := AuthDeps{
		SecretConfigured: true,
		LocalIdentityID:  localIdentityID,
		// agui declares its own narrow Identity projection so it need not import
		// internal/identity at the package boundary; storeChecker is the trivial
		// field-copy bridge (the same shape cmd/aura's identityCheckerAdapter uses at
		// the composition root).
		Identities: storeChecker{store: store},
	}

	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	t.Run("local principal -> next (wildcard grant)", func(t *testing.T) {
		rec := httptest.NewRecorder()
		req := withPrincipal(httptest.NewRequest(http.MethodPost, "/agent/run", nil), localIdentityID)
		RequireCapability(next, deps, "agent.run").ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("authorized POST /agent/run = %d, want 200", rec.Code)
		}
	})

	t.Run("identity without the grant -> 403", func(t *testing.T) {
		// Create a fresh identity with NO capability grants; its principal must be denied.
		other, err := pool.Exec(ctx,
			`INSERT INTO aura.identities (id, name, kind) VALUES (gen_random_uuid(), $1, 'user') RETURNING id`,
			"web03-capgate-"+t.Name())
		_ = other
		if err != nil {
			t.Fatalf("seed ungranted identity: %v", err)
		}
		var ungrantedID string
		if err := pool.QueryRow(ctx,
			`SELECT id::text FROM aura.identities WHERE name = $1`,
			"web03-capgate-"+t.Name()).Scan(&ungrantedID); err != nil {
			t.Fatalf("read ungranted identity id: %v", err)
		}
		t.Cleanup(func() {
			_, _ = pool.Exec(ownerCtx(), `DELETE FROM aura.identities WHERE id = $1`, ungrantedID)
		})

		rec := httptest.NewRecorder()
		req := withPrincipal(httptest.NewRequest(http.MethodPost, "/agent/run", nil), ungrantedID)
		RequireCapability(next, deps, "agent.run").ServeHTTP(rec, req)
		if rec.Code != http.StatusForbidden {
			t.Fatalf("ungranted POST /agent/run = %d, want 403", rec.Code)
		}
	})
}
