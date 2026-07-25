//go:build db_integration

package db

import (
	"context"
	"testing"
	"time"
)

func TestAdaptiveFocalClaimConversationMigrationSecurityContract(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	migrateURL := envOrSkip(t, "AURA_DB_MIGRATE_URL")
	if err := EnsureRoles(
		ctx,
		bootstrapURL(t),
		envOrSkip(t, "POSTGRES_PASSWORD"),
	); err != nil {
		t.Fatal(err)
	}
	if _, err := Migrate(ctx, migrateURL); err != nil {
		t.Fatal(err)
	}
	pool, err := Open(ctx, &Config{URL: migrateURL})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)

	var securityDefiner, appExecute, publicExecute bool
	var config string
	err = pool.QueryRow(ctx, `SELECT p.prosecdef, COALESCE(p.proconfig::text, ''),
		has_function_privilege('aura_app', p.oid, 'EXECUTE'),
		EXISTS (
			SELECT 1
			FROM aclexplode(COALESCE(p.proacl, acldefault('f', p.proowner))) AS acl
			WHERE acl.grantee=0 AND acl.privilege_type='EXECUTE'
		)
		FROM pg_proc AS p
		JOIN pg_namespace AS n ON n.oid=p.pronamespace
		WHERE n.nspname='aura'
		  AND p.proname='enforce_adaptive_focal_claim_conversation'`).Scan(
		&securityDefiner,
		&config,
		&appExecute,
		&publicExecute,
	)
	if err != nil {
		t.Fatal(err)
	}
	if securityDefiner ||
		config != "{search_path=pg_catalog}" ||
		appExecute ||
		publicExecute {
		t.Fatalf(
			"unsafe conversation validator: definer=%t path=%q app=%t public=%t",
			securityDefiner,
			config,
			appExecute,
			publicExecute,
		)
	}

	var enabled string
	err = pool.QueryRow(ctx, `SELECT t.tgenabled::text
		FROM pg_trigger AS t
		JOIN pg_class AS c ON c.oid=t.tgrelid
		JOIN pg_namespace AS n ON n.oid=c.relnamespace
		WHERE n.nspname='aura'
		  AND c.relname='adaptive_focal_cohort_claims'
		  AND t.tgname='adaptive_focal_cohort_claims_conversation'
		  AND NOT t.tgisinternal`).Scan(&enabled)
	if err != nil {
		t.Fatal(err)
	}
	if enabled != "O" {
		t.Fatalf("conversation trigger enabled state = %q, want O", enabled)
	}
}
