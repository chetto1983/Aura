//go:build db_integration

// Live proofs for migration 0118 (amendment #214): the RLS floor on aura.resource_acl, the
// share/revoke round trip, the deprovision cascade, and the always-apply index.
//
// Requires a Postgres with the migrations applied through 0118:
//
//	make db-up && aura db migrate
//	AURA_DB_URL set in the environment (the aura_app DSN — the RLS-subject role)
//
// Run via:
//
//	go test -tags db_integration -race -run TestACL ./internal/skillacl -count=1
//
// No-skip-as-green: aclEnvOrSkip t.Fatals under $CI when the DSN is unset, so a skipped tier
// fails the gate rather than passing it.
package skillacl

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/chetto1983/aura/internal/db"
	"github.com/chetto1983/aura/internal/dbtest"
)

func aclEnvOrSkip(t *testing.T, key string) string {
	t.Helper()
	value := os.Getenv(key)
	if value == "" {
		if os.Getenv("CI") != "" {
			t.Fatalf("skillacl integration requires %s under CI — a skipped tier is never a silent pass", key)
		}
		t.Skipf("skillacl integration requires %s", key)
	}
	return value
}

// aclVerifyPool connects as the role that OWNS aura.skill_catalog and aura.resource_acl
// (aura_migrate), which is the only way to count what really survived a delete.
//
// The obvious choice — the app pool with no app.current_identity — is worse than useless
// here, and CI run 1791 proved it: aura_app is exactly the role the restrictive floor
// governs, so an unscoped SELECT sees NOTHING. Every "count == 0" assertion then passes
// because the rows are hidden, not because they are gone, and the one assertion expecting a
// row to SURVIVE fails no matter how correct the schema is. A table's owner is not subject to
// its row security unless it is FORCEd, so this pool reads the table as it actually is.
func aclVerifyPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	pool, err := db.Open(ctx, &db.Config{
		URL: dbtest.MigrateURL(t, aclEnvOrSkip(t, "AURA_DB_MIGRATE_URL")),
	})
	if err != nil {
		t.Fatalf("db.Open (owner): %v", err)
	}
	t.Cleanup(pool.Close)
	return pool
}

func aclPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	pool, err := db.Open(ctx, &db.Config{URL: aclEnvOrSkip(t, "AURA_DB_URL")})
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	t.Cleanup(pool.Close)
	return pool
}

// seedIdentity inserts a throwaway identity and schedules its deletion. The deletion is also
// the cascade probe: aura.identities has no RLS, so this runs on the bare pool.
func seedIdentity(t *testing.T, pool *pgxpool.Pool, label string) string {
	t.Helper()
	id := uuid.NewString()
	name := "skillacl-" + label + "-" + id
	if _, err := pool.Exec(context.Background(),
		`INSERT INTO aura.identities (id, name, kind) VALUES ($1, $2, 'user')`, id, name); err != nil {
		t.Fatalf("seed identity %s: %v", label, err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM aura.identities WHERE id = $1`, id)
	})
	return id
}

// seedSkill inserts one catalog row owned by owner and returns its id. It goes through the
// identity transaction because aura.skill_catalog is fail-closed: an insert with no
// app.current_identity is refused, which this helper would surface immediately.
func seedSkill(t *testing.T, pool *pgxpool.Pool, owner, name string, always bool) string {
	t.Helper()
	var id string
	err := db.WithIdentityTxRaw(context.Background(), pool, owner, func(tx pgx.Tx) error {
		return tx.QueryRow(context.Background(),
			`INSERT INTO aura.skill_catalog (owner_identity_id, name, always_apply, content_hash)
			 VALUES ($1, $2, $3, 'hash') RETURNING id`, owner, name, always).Scan(&id)
	})
	if err != nil {
		t.Fatalf("seed skill %q: %v", name, err)
	}
	return id
}

// TestACLRequiresAnIdentityToReadAnything is acceptance criterion 8: an aura_app connection
// with no app.current_identity set reads NOTHING from the ACL table. The permissive policy
// alone would not give this — a caller with no identity would match the public rows — so this
// is the RESTRICTIVE floor of migration 0087, asserted rather than assumed.
func TestACLRequiresAnIdentityToReadAnything(t *testing.T) {
	pool := aclPool(t)
	ctx := context.Background()
	owner := seedIdentity(t, pool, "owner")
	skillID := seedSkill(t, pool, owner, "acl-floor-skill", false)

	store, err := NewStore(pool)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	// A PUBLIC grant is the hardest case for the floor: its permissive predicate is true for
	// everyone, so only the restrictive layer can keep it from an anonymous connection.
	if err := store.GrantPublic(ctx, owner, ResourceSkill, skillID, PermView); err != nil {
		t.Fatalf("GrantPublic: %v", err)
	}

	var visible int
	if err := pool.QueryRow(ctx,
		`SELECT count(*) FROM aura.resource_acl WHERE resource_id = $1`, skillID).Scan(&visible); err != nil {
		t.Fatalf("count without an identity: %v", err)
	}
	if visible != 0 {
		t.Fatalf("an aura_app connection with no app.current_identity read %d ACL rows, want 0", visible)
	}

	// The same query INSIDE an identity transaction sees the grant, so the zero above is the
	// floor at work and not an insert that quietly failed.
	var scoped int
	if err := db.WithIdentityTxRaw(ctx, pool, owner, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, `SELECT count(*) FROM aura.resource_acl WHERE resource_id = $1`, skillID).Scan(&scoped)
	}); err != nil {
		t.Fatalf("count as the owner: %v", err)
	}
	if scoped != 1 {
		t.Fatalf("the owner sees %d grants on their own skill, want 1", scoped)
	}
}

// TestACLShareAndRevoke walks the share lifecycle a person actually performs: B cannot see
// A's skill, a grant makes it visible, and a revoke takes it away again.
func TestACLShareAndRevoke(t *testing.T) {
	pool := aclPool(t)
	ctx := context.Background()
	alice := seedIdentity(t, pool, "alice")
	bob := seedIdentity(t, pool, "bob")
	skillID := seedSkill(t, pool, alice, "acl-share-skill", false)

	store, err := NewStore(pool)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}

	ids, err := store.AccessibleResourceIDs(ctx, bob, ResourceSkill, PermView)
	if err != nil {
		t.Fatalf("accessible before the grant: %v", err)
	}
	if containsID(ids, skillID) {
		t.Fatal("bob can reach alice's skill with no grant — the boundary is not there")
	}

	if err := store.GrantToIdentity(ctx, alice, ResourceSkill, skillID, bob, PermView); err != nil {
		t.Fatalf("GrantToIdentity: %v", err)
	}
	ids, err = store.AccessibleResourceIDs(ctx, bob, ResourceSkill, PermView)
	if err != nil {
		t.Fatalf("accessible after the grant: %v", err)
	}
	if !containsID(ids, skillID) {
		t.Fatal("bob cannot see the skill alice shared with him")
	}
	// A view grant does not answer for edit: the bitmask test is "at least these bits".
	edits, err := store.AccessibleResourceIDs(ctx, bob, ResourceSkill, PermEdit)
	if err != nil {
		t.Fatalf("accessible for edit: %v", err)
	}
	if containsID(edits, skillID) {
		t.Fatal("a view grant answered an edit question")
	}

	removed, err := store.RevokeFromIdentity(ctx, alice, ResourceSkill, skillID, bob)
	if err != nil {
		t.Fatalf("RevokeFromIdentity: %v", err)
	}
	if !removed {
		t.Fatal("revoke reported nothing removed for a grant that existed")
	}
	ids, err = store.AccessibleResourceIDs(ctx, bob, ResourceSkill, PermView)
	if err != nil {
		t.Fatalf("accessible after the revoke: %v", err)
	}
	if containsID(ids, skillID) {
		t.Fatal("bob still reaches the skill after the revoke")
	}
	// A second revoke removes nothing and says so, so a caller can tell a real revocation
	// from a no-op.
	if removed, err = store.RevokeFromIdentity(ctx, alice, ResourceSkill, skillID, bob); err != nil || removed {
		t.Fatalf("second revoke = (%v, %v), want (false, nil)", removed, err)
	}
}

// TestACLPublicGrantReachesEveryone covers the other principal: a public grant opens the skill
// to an identity nobody named, and revoking it closes the skill again. It also walks
// ListGrants, the read an operator runs before deciding to revoke.
func TestACLPublicGrantReachesEveryone(t *testing.T) {
	pool := aclPool(t)
	ctx := context.Background()
	alice := seedIdentity(t, pool, "alice-public")
	carol := seedIdentity(t, pool, "carol-stranger")
	skillID := seedSkill(t, pool, alice, "acl-public-skill", false)

	store, err := NewStore(pool)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	if err := store.GrantPublic(ctx, alice, ResourceSkill, skillID, PermView); err != nil {
		t.Fatalf("GrantPublic: %v", err)
	}

	ids, err := store.AccessibleResourceIDs(ctx, carol, ResourceSkill, PermView)
	if err != nil {
		t.Fatalf("accessible for a stranger: %v", err)
	}
	if !containsID(ids, skillID) {
		t.Fatal("a public grant did not reach an identity nobody named")
	}

	grants, err := store.ListGrants(ctx, alice, ResourceSkill, skillID)
	if err != nil {
		t.Fatalf("ListGrants: %v", err)
	}
	if len(grants) != 1 || grants[0].PrincipalType != "public" || grants[0].PrincipalID != "" || grants[0].Perm != PermView {
		t.Fatalf("ListGrants = %+v, want one public view grant with no principal id", grants)
	}
	if grants[0].GrantedBy != alice {
		t.Fatalf("granted_by = %q, want the granter %q", grants[0].GrantedBy, alice)
	}

	removed, err := store.RevokePublic(ctx, alice, ResourceSkill, skillID)
	if err != nil || !removed {
		t.Fatalf("RevokePublic = (%v, %v), want (true, nil)", removed, err)
	}
	if ids, err = store.AccessibleResourceIDs(ctx, carol, ResourceSkill, PermView); err != nil {
		t.Fatalf("accessible after the revoke: %v", err)
	}
	if containsID(ids, skillID) {
		t.Fatal("the skill is still public after the revoke")
	}
}

// TestACLDeprovisionLeavesNoOrphan is acceptance criterion 6: deleting an identity takes its
// catalog rows AND every grant that mentions it — both the grants ON its skills and the
// grants addressed TO it.
func TestACLDeprovisionLeavesNoOrphan(t *testing.T) {
	pool := aclPool(t)
	ctx := context.Background()
	alice := seedIdentity(t, pool, "alice-doomed")
	bob := seedIdentity(t, pool, "bob-survivor")
	aliceSkill := seedSkill(t, pool, alice, "acl-cascade-alice", false)
	bobSkill := seedSkill(t, pool, bob, "acl-cascade-bob", false)

	store, err := NewStore(pool)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	// Two grants that must both disappear with alice: one ON her skill, one addressed TO her.
	if err := store.GrantToIdentity(ctx, alice, ResourceSkill, aliceSkill, bob, PermView); err != nil {
		t.Fatalf("grant on alice's skill: %v", err)
	}
	if err := store.GrantToIdentity(ctx, bob, ResourceSkill, bobSkill, alice, PermView); err != nil {
		t.Fatalf("grant to alice: %v", err)
	}

	// Deleted on the BARE pool, with no app.current_identity — which is how deprovisioning
	// really runs (internal/identity.Store.DeleteIdentity). It is also the condition the
	// cascade triggers have to survive: an INVOKER trigger function would have its DELETE on
	// aura.resource_acl filtered by that table's restrictive floor, remove nothing, and
	// report success. This is the caller that proves they are SECURITY DEFINER.
	if _, err := pool.Exec(ctx, `DELETE FROM aura.identities WHERE id = $1`, alice); err != nil {
		t.Fatalf("deprovision alice: %v", err)
	}

	// Counted as the table OWNER, never on the app pool: aura_app is the role the restrictive
	// floor governs, so an unscoped count there returns 0 for everything and every assertion
	// below would pass by blindness rather than by cascade (CI run 1791).
	verify := aclVerifyPool(t)
	var rows int
	if err := verify.QueryRow(ctx,
		`SELECT count(*) FROM aura.skill_catalog WHERE owner_identity_id = $1`, alice).Scan(&rows); err != nil {
		t.Fatalf("count alice's catalog rows: %v", err)
	}
	if rows != 0 {
		t.Fatalf("alice's catalog rows survived her deletion: %d", rows)
	}
	if err := verify.QueryRow(ctx,
		`SELECT count(*) FROM aura.resource_acl WHERE resource_id = $1 OR principal_id = $1 OR granted_by = $1`, alice).Scan(&rows); err != nil {
		t.Fatalf("count orphan grants: %v", err)
	}
	if rows != 0 {
		t.Fatalf("%d ACL rows still mention the deleted identity", rows)
	}
	// Bob's own skill is untouched: the cascade is scoped to the identity that left.
	if err := verify.QueryRow(ctx,
		`SELECT count(*) FROM aura.skill_catalog WHERE id = $1`, bobSkill).Scan(&rows); err != nil {
		t.Fatalf("count bob's catalog row: %v", err)
	}
	if rows != 1 {
		t.Fatalf("bob's skill count = %d, want 1 — the cascade reached past alice", rows)
	}
}

// TestACLRefusesAGrantOverSomebodyElsesSkill is the write half of D-214-5, and it is the
// case a visibility-only policy silently allows: every disjunct of "may I SEE this grant" is
// satisfiable by the attacker's own row, so without a separate write check any identity could
// insert a grant naming ITSELF on any resource id it can name — or publish somebody else's
// skill to the whole deployment — and skill_catalog_shared_read would then honour it.
func TestACLRefusesAGrantOverSomebodyElsesSkill(t *testing.T) {
	pool := aclPool(t)
	ctx := context.Background()
	alice := seedIdentity(t, pool, "grant-owner")
	mallory := seedIdentity(t, pool, "grant-forger")
	aliceSkill := seedSkill(t, pool, alice, "acl-not-yours", false)

	store, err := NewStore(pool)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}

	// Mallory grants herself view on Alice's skill. The id is not a secret — it travels in
	// listings and logs — so knowing it must not be the same as being allowed to use it.
	if err := store.GrantToIdentity(ctx, mallory, ResourceSkill, aliceSkill, mallory, PermView); err == nil {
		t.Fatal("an identity granted ITSELF access to a skill it does not own")
	}
	// And publishes it to everyone, the disjunct that is true for every caller.
	if err := store.GrantPublic(ctx, mallory, ResourceSkill, aliceSkill, PermView); err == nil {
		t.Fatal("an identity published somebody else's skill to the whole deployment")
	}
	if ids, err := store.AccessibleResourceIDs(ctx, mallory, ResourceSkill, PermView); err != nil {
		t.Fatalf("AccessibleResourceIDs: %v", err)
	} else if containsID(ids, aliceSkill) {
		t.Fatalf("the forged grant reached the table: %v", ids)
	}

	// The owner's own grant still works — the check narrows the write, it does not close it.
	if err := store.GrantToIdentity(ctx, alice, ResourceSkill, aliceSkill, mallory, PermView); err != nil {
		t.Fatalf("the owner cannot share their own skill: %v", err)
	}
	// Having been shared WITH is not permission to share ONWARD: the ownership test in the
	// write policy reads owner_identity_id, not mere visibility.
	if err := store.GrantPublic(ctx, mallory, ResourceSkill, aliceSkill, PermView); err == nil {
		t.Fatal("a grantee re-shared a skill that was only shared with them")
	}
	// Nor may a grantee revoke the grant somebody else made on somebody else's skill — but
	// they may decline their own share.
	bob := seedIdentity(t, pool, "grant-bystander")
	if removed, err := store.RevokeFromIdentity(ctx, bob, ResourceSkill, aliceSkill, mallory); err != nil {
		t.Fatalf("bystander revoke: %v", err)
	} else if removed {
		t.Fatal("a bystander revoked a grant between two other identities")
	}
	if removed, err := store.RevokeFromIdentity(ctx, mallory, ResourceSkill, aliceSkill, mallory); err != nil {
		t.Fatalf("declining a share: %v", err)
	} else if !removed {
		t.Fatal("the named principal could not decline the share addressed to them")
	}
}

// TestAlwaysApplyLookupUsesTheIndex is acceptance criterion 7: the always-on lookup is an
// index lookup, not a scan of the catalog. It is rendered at the top of every turn.
//
// The seeding is what makes the plan meaningful. On a table of five rows Postgres reads the
// heap because that IS cheaper, and an EXPLAIN there would assert nothing about the query; so
// this seeds enough rows for the planner to have a real choice, ANALYZEs, and only then asks.
func TestAlwaysApplyLookupUsesTheIndex(t *testing.T) {
	pool := aclPool(t)
	ctx := context.Background()
	owner := seedIdentity(t, pool, "always")

	if err := db.WithIdentityTxRaw(ctx, pool, owner, func(tx pgx.Tx) error {
		for i := range 2000 {
			name := "always-seed-" + uuid.NewString()[:8] + "-" + string(rune('a'+i%26))
			if _, err := tx.Exec(ctx,
				`INSERT INTO aura.skill_catalog (owner_identity_id, name, always_apply, content_hash)
				 VALUES ($1, $2, $3, 'hash')`, owner, name, i%500 == 0); err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		t.Fatalf("seed catalog rows: %v", err)
	}
	// ANALYZE as the OWNER. Postgres does not fail an ANALYZE a role may not run — it emits
	// "skipping ... only table or database owner can analyze it" as a WARNING and moves on, so
	// running it on the app pool returns nil while doing nothing, and the EXPLAIN below would
	// then be read over default statistics. That is the difference between proving the index
	// is chosen and proving nothing at all.
	if _, err := aclVerifyPool(t).Exec(ctx, `ANALYZE aura.skill_catalog`); err != nil {
		t.Fatalf("ANALYZE: %v", err)
	}

	var plan strings.Builder
	if err := db.WithIdentityTxRaw(ctx, pool, owner, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx,
			`EXPLAIN SELECT id, name FROM aura.skill_catalog WHERE owner_identity_id = $1 AND always_apply ORDER BY name`,
			owner)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var line string
			if err := rows.Scan(&line); err != nil {
				return err
			}
			plan.WriteString(line)
			plan.WriteString("\n")
		}
		return rows.Err()
	}); err != nil {
		t.Fatalf("EXPLAIN: %v", err)
	}
	if strings.Contains(plan.String(), "Seq Scan on skill_catalog") {
		t.Fatalf("the always-apply lookup scans the catalog — it runs on every turn:\n%s", plan.String())
	}
}

func containsID(ids []string, want string) bool {
	for _, id := range ids {
		if id == want {
			return true
		}
	}
	return false
}
