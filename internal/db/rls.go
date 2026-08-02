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
// database — because it is a superuser, or because it carries BYPASSRLS.
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
// The check reads current_user rather than trusting the DSN string: the effective role
// after connecting is what Postgres actually evaluates policies against, and a DSN can be
// overridden by PGUSER, a service file, or a peer-auth mapping.
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
	if !superuser && !bypassRLS {
		return nil
	}
	return fmt.Errorf(
		"%w: connected as %q (rolsuper=%t, rolbypassrls=%t), so every row-level-security "+
			"policy is skipped and tenants are not isolated; point AURA_DB_URL at the "+
			"unprivileged application role (AURA_DB_APP_ROLE, default aura_app) instead of a "+
			"superuser",
		ErrRLSBypass, role, superuser, bypassRLS)
}
