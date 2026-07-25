//go:build db_integration

package db

import (
	"context"
	"testing"
	"time"
)

func TestAdaptiveFocalCohortMigrationSecurityContract(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	migrateURL := envOrSkip(t, "AURA_DB_MIGRATE_URL")
	if err := EnsureRoles(ctx, bootstrapURL(t), envOrSkip(t, "POSTGRES_PASSWORD")); err != nil {
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

	for _, table := range []string{
		"adaptive_focal_cohorts", "adaptive_focal_cohort_claims",
	} {
		var rls, selectOK, insertOK, updateOK, deleteOK, truncateOK, referencesOK, triggerOK bool
		err := pool.QueryRow(ctx, `SELECT c.relrowsecurity,
			has_table_privilege('aura_app', c.oid, 'SELECT'),
			has_table_privilege('aura_app', c.oid, 'INSERT'),
			has_table_privilege('aura_app', c.oid, 'UPDATE'),
			has_table_privilege('aura_app', c.oid, 'DELETE'),
			has_table_privilege('aura_app', c.oid, 'TRUNCATE'),
			has_table_privilege('aura_app', c.oid, 'REFERENCES'),
			has_table_privilege('aura_app', c.oid, 'TRIGGER')
			FROM pg_class AS c JOIN pg_namespace AS n ON n.oid=c.relnamespace
			WHERE n.nspname='aura' AND c.relname=$1`, table).Scan(
			&rls, &selectOK, &insertOK, &updateOK, &deleteOK, &truncateOK,
			&referencesOK, &triggerOK,
		)
		if err != nil {
			t.Fatal(err)
		}
		if !rls || !selectOK || !insertOK || updateOK || deleteOK || truncateOK || referencesOK || triggerOK {
			t.Fatalf("unsafe %s ACL: rls=%t select=%t insert=%t update=%t delete=%t truncate=%t references=%t trigger=%t", table, rls, selectOK, insertOK, updateOK, deleteOK, truncateOK, referencesOK, triggerOK)
		}
	}

	for _, function := range []string{
		"adaptive_focal_cohort_artifact_valid",
		"adaptive_focal_cohort_catalogs_valid",
		"adaptive_focal_cohort_metrics_valid",
		"adaptive_focal_cohort_id_valid",
	} {
		var securityDefiner, appExecute, publicExecute bool
		var config string
		err = pool.QueryRow(ctx, `SELECT p.prosecdef, COALESCE(p.proconfig::text, ''),
			has_function_privilege('aura_app', p.oid, 'EXECUTE'),
			EXISTS (SELECT 1 FROM aclexplode(COALESCE(p.proacl, acldefault('f', p.proowner))) AS acl WHERE acl.grantee=0 AND acl.privilege_type='EXECUTE')
			FROM pg_proc AS p JOIN pg_namespace AS n ON n.oid=p.pronamespace
			WHERE n.nspname='aura' AND p.proname=$1`, function).Scan(
			&securityDefiner, &config, &appExecute, &publicExecute,
		)
		if err != nil {
			t.Fatal(err)
		}
		if securityDefiner || config != "{search_path=pg_catalog}" || !appExecute || publicExecute {
			t.Fatalf("unsafe %s: definer=%t path=%q app=%t public=%t", function, securityDefiner, config, appExecute, publicExecute)
		}
	}
}
