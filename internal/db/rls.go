package db

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
)

// ErrRLSBypass is returned by VerifyRLSEnforced when the pool's role is exempt from
// row-level security. Callers match on it rather than on the message text.
var ErrRLSBypass = errors.New("db: application role bypasses row-level security")

// VerifyRLSEnforced fails when the role behind pool can ignore every RLS policy in the
// database — because it is a superuser, because it carries BYPASSRLS, or because it OWNS
// the tables the policies are on.
//
// This closes the one hole the policies themselves cannot. Migration 0087 makes the
// identity-scoped tables fail closed, but a policy is only ever as good as the role it is
// evaluated against: PostgreSQL skips row security entirely for a superuser or a
// BYPASSRLS role ("Superusers and roles with the BYPASSRLS attribute always bypass the
// row security system when accessing a table" — PostgreSQL manual, Row Security
// Policies). Against such a role the whole of 0032/0041/0087 is inert and nothing
// anywhere reports it: reads simply return every tenant's rows, exactly as they did
// before any policy existed.
//
// That is not hypothetical here. config.loadBase composes the runtime DSN from
// AURA_DB_APP_ROLE, which defaults to aura_app — but the same function defaults
// POSTGRES_USER to "aura", which IS a superuser with rolbypassrls, and a deployment that
// points AURA_DB_URL or AURA_DB_APP_ROLE at it gets a daemon with no tenant isolation and
// no symptom. Silence is the dangerous part, so this turns it into a refusal to boot.
//
// The owner term was missing until 2026-09-06 and is the one that mattered most, because
// the owner is the role a deployment is most likely to reach for by mistake. Migration 0087
// states the rule in its own words — "aura_migrate OWNS these tables and a table owner
// bypasses RLS by default, which is exactly what golang-migrate needs to keep running DDL
// and backfills" — and concludes that aura_app is safe because it is "a non-owner,
// non-superuser, non-BYPASSRLS role". Three conditions; this function checked two. Nothing
// in the tree uses FORCE ROW LEVEL SECURITY (verified across every migration), which is a
// deliberate choice 0087 explains and not an oversight to correct here, so ownership really
// does mean bypass.
//
// The gap was reachable: aura_migrate is created with a bare CREATE ROLE ... WITH LOGIN
// PASSWORD (migrate.go), so it carries neither rolsuper nor rolbypassrls. An AURA_DB_URL
// pointed at it — the obvious mistake, since it is the other DSN the deployment already
// holds — passed this gate and served every tenant's rows to every tenant, silently. That
// is precisely the symptomless failure the rest of this comment exists to prevent.
//
// The check reads current_user rather than trusting the DSN string: the effective role
// after connecting is what Postgres actually evaluates policies against, and a DSN can be
// overridden by PGUSER, a service file, or a peer-auth mapping. Ownership is likewise read
// from the catalog, per table, rather than assumed from the role's name.
func VerifyRLSEnforced(ctx context.Context, pool *pgxpool.Pool) error {
	if pool == nil {
		return fmt.Errorf("%w: no pool", ErrRLSBypass)
	}
	var role string
	var superuser, bypassRLS bool
	const q = `SELECT rolname, rolsuper, rolbypassrls FROM pg_roles WHERE rolname = current_user`
	if err := pool.QueryRow(ctx, q).Scan(&role, &superuser, &bypassRLS); err != nil {
		return fmt.Errorf("verify rls enforcement: %w", err)
	}
	if superuser || bypassRLS {
		return fmt.Errorf(
			"%w: connected as %q (rolsuper=%t, rolbypassrls=%t), so every row-level-security "+
				"policy is skipped and tenants are not isolated; point AURA_DB_URL at the "+
				"unprivileged application role (AURA_DB_APP_ROLE, default aura_app) instead of a "+
				"superuser",
			ErrRLSBypass, role, superuser, bypassRLS)
	}
	return verifyNotPolicyOwner(ctx, pool, role)
}

// verifyNotPolicyOwner refuses a role that owns any RLS-enabled table in the aura schema.
//
// It counts rather than name-matches: which role owns the tables is a property of the
// database, not of a constant this package could drift away from. A deployment that
// migrates as some other role is covered for free.
//
// A zero count is a pass, and deliberately so — that is a database whose migrations have
// not run yet, where there are no policies to bypass. The daemon checks the migration head
// separately (db.CheckMigrationHead), so an unmigrated database is already refused for its
// own reason; making this function guess at that too would give one fault two messages.
func verifyNotPolicyOwner(ctx context.Context, pool *pgxpool.Pool, role string) error {
	const q = `
		SELECT count(*)
		FROM pg_class c
		JOIN pg_namespace n ON n.oid = c.relnamespace
		JOIN pg_roles r ON r.oid = c.relowner
		WHERE n.nspname = 'aura' AND c.relkind = 'r' AND c.relrowsecurity AND r.rolname = current_user`
	var owned int
	if err := pool.QueryRow(ctx, q).Scan(&owned); err != nil {
		return fmt.Errorf("verify rls table ownership: %w", err)
	}
	if owned == 0 {
		return nil
	}
	return fmt.Errorf(
		"%w: connected as %q, which OWNS %d row-level-security table(s) in schema aura; a "+
			"table owner bypasses its own policies unless they are FORCEd (they are not, by "+
			"design — see migration 0087), so tenants are not isolated even though every "+
			"policy is present and rolsuper/rolbypassrls are both false. Point AURA_DB_URL at "+
			"the unprivileged application role (AURA_DB_APP_ROLE, default aura_app), not at "+
			"the migration role",
		ErrRLSBypass, role, owned)
}
