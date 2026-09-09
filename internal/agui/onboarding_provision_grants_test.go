//go:build db_integration

// onboarding_provision_grants_test.go proves the Task 2c uniform-grant behavior
// (D-01/RBAC-03) against a REAL migrated Postgres, reusing the live saga harness in
// onboarding_provision_integration_harness_test.go: every identity the onboarding saga
// provisions ends with EXACTLY identity.UserSet() — regardless of what the request asked
// for — a request naming an administrative capability is refused rather than silently
// narrowed, and the underlying grant step is idempotent (ON CONFLICT DO NOTHING at the SQL
// layer, internal/db/queries/capability_grants.sql).
//
// Run via (stack up, password from the container/.env):
//
//	go test -tags db_integration ./internal/agui -run 'TestProvisionGrantsUniformCapabilitySet|TestProvisionRefusesAdministrativeRequest|TestProvisionGrantStepIsIdempotent' -count=1 -p 1 -race -v
//
// Requires POSTGRES_PASSWORD plus optional PGHOST/PGPORT. No-skip-as-green: envOrSkip
// t.Fatals under $CI when required env is unset (see disposableLiveSagaPool).

package agui

import (
	"errors"
	"slices"
	"testing"

	"github.com/jackc/pgx/v5"

	"github.com/chetto1983/aura/internal/db"
	"github.com/chetto1983/aura/internal/identity"
)

// liveGrantedCapabilities reads the actual capability_grants rows for the identity
// provisioned under email, RLS-scoped (aura.capability_grants is fail-closed as of
// migration 0087, matching auraOrphans's own scoping in the harness file), sorted for a
// deterministic comparison against identity.UserSet().
func liveGrantedCapabilities(t *testing.T, env *liveSagaEnv, identityID string) []string {
	t.Helper()
	var got []string
	if err := db.WithIdentityTxRaw(ownerCtx(), env.pool, identityID, func(tx pgx.Tx) error {
		rows, err := tx.Query(ownerCtx(), `SELECT capability FROM aura.capability_grants WHERE identity_id=$1::uuid`, identityID)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var cap string
			if err := rows.Scan(&cap); err != nil {
				return err
			}
			got = append(got, cap)
		}
		return rows.Err()
	}); err != nil {
		t.Fatalf("read live capability_grants: %v", err)
	}
	slices.Sort(got)
	return got
}

// TestProvisionGrantsUniformCapabilitySet proves every provisioned identity ends with
// exactly identity.UserSet() — four rows, no administrative row, no wildcard — regardless
// of what the request's Capabilities field asked for. Two requests, one carrying an
// EMPTY list (RBAC-03 "empty" truth: an empty request still lands the full set) and one
// carrying an unrelated well-formed name, both must converge on the identical granted set.
func TestProvisionGrantsUniformCapabilitySet(t *testing.T) {
	wantSet := identity.UserSet()
	slices.Sort(wantSet)

	cases := []struct {
		name      string
		requested []string
	}{
		{"empty request list", nil},
		{"unrelated well-formed name", []string{"agent.run", "share.public"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			env := newLiveSagaEnv(t)
			au := newStatefulAuthula()
			svc, tok, email := env.service(t, au, env.auraLeg, env.telegram)
			t.Cleanup(func() { env.cleanupProvisioned(email) })

			req := liveProvReq(email, "temp-pw-123")
			req.Capabilities = tc.requested
			resp, err := svc.Provision(ownerCtx(), env.creator, tok, req)
			if err != nil {
				t.Fatalf("Provision(requested=%v): %v", tc.requested, err)
			}

			got := liveGrantedCapabilities(t, env, resp.IdentityID)
			if !slices.Equal(got, wantSet) {
				t.Fatalf("granted = %v, want exactly identity.UserSet() = %v (requested=%v)", got, wantSet, tc.requested)
			}
			for _, admin := range identity.Administrative() {
				if slices.Contains(got, admin) {
					t.Fatalf("granted set contains administrative capability %q", admin)
				}
			}
			if slices.Contains(got, identity.Wildcard) {
				t.Fatal("granted set contains the retired '*' wildcard")
			}
		})
	}
}

// TestProvisionRefusesAdministrativeRequest proves a provisioning request naming an
// administrative capability is refused with a named error (ErrOnboardingEscalation) rather
// than silently narrowed, and leaves zero rows — the audit trail records a refusal, never a
// success that was not one (T-02-15).
func TestProvisionRefusesAdministrativeRequest(t *testing.T) {
	for _, admin := range identity.Administrative() {
		t.Run(admin, func(t *testing.T) {
			env := newLiveSagaEnv(t)
			au := newStatefulAuthula()
			svc, tok, email := env.service(t, au, env.auraLeg, env.telegram)
			t.Cleanup(func() { env.cleanupProvisioned(email) })

			req := liveProvReq(email, "temp-pw-123")
			req.Capabilities = []string{admin}
			if _, err := svc.Provision(ownerCtx(), env.creator, tok, req); !errors.Is(err, ErrOnboardingEscalation) {
				t.Fatalf("Provision(requested=%q) err = %v, want ErrOnboardingEscalation", admin, err)
			}

			ids, grants, links, tokens, recovery, audit := env.auraOrphans(t, email)
			if ids != 0 || grants != 0 || links != 0 || tokens != 0 || recovery != 0 || audit != 0 || au.liveUsers() != 0 {
				t.Fatalf("refused request left rows: identities=%d grants=%d links=%d tokens=%d recovery=%d audit=%d authula=%d",
					ids, grants, links, tokens, recovery, audit, au.liveUsers())
			}
		})
	}
}

// TestProvisionGrantStepIsIdempotent proves the grant step the saga drives
// (identity.Store.GrantCapability, ON CONFLICT DO NOTHING at the SQL layer) is idempotent:
// re-running it for an identity that already holds the user set adds no duplicate rows and
// returns nil, so a journal-resumed retry after a partial saga failure can never double-grant.
func TestProvisionGrantStepIsIdempotent(t *testing.T) {
	env := newLiveSagaEnv(t)
	au := newStatefulAuthula()
	svc, tok, email := env.service(t, au, env.auraLeg, env.telegram)
	t.Cleanup(func() { env.cleanupProvisioned(email) })

	resp, err := svc.Provision(ownerCtx(), env.creator, tok, liveProvReq(email, "temp-pw-123"))
	if err != nil {
		t.Fatalf("Provision: %v", err)
	}

	store := identity.New(env.pool)
	for _, cap := range identity.UserSet() {
		if err := store.GrantCapability(ownerCtx(), resp.IdentityID, cap); err != nil {
			t.Fatalf("re-grant %q: %v, want nil (idempotent no-op)", cap, err)
		}
	}

	got := liveGrantedCapabilities(t, env, resp.IdentityID)
	want := identity.UserSet()
	slices.Sort(want)
	if !slices.Equal(got, want) {
		t.Fatalf("after re-running the grant step: granted = %v, want exactly identity.UserSet() = %v (no duplicates)", got, want)
	}
}
