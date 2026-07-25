//go:build db_integration

package adaptive

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/chetto1983/aura/internal/db"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestRandomizationMigration0075DownUpAndPrivileges(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 120*time.Second)
	defer cancel()
	_, migrateURL := schema2MigrationDatabase(
		t, ctx, "aura_randomization_migration", 75,
	)
	migratePool, err := db.Open(ctx, &db.Config{URL: migrateURL})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(migratePool.Close)
	assertRandomizationMigrationContract(t, ctx, migratePool)

	if err := db.MigrateSteps(ctx, migrateURL, -1); err != nil {
		t.Fatalf("migrate 0075 down: %v", err)
	}
	assertSchema2MigrationVersion(t, ctx, migrateURL, 74)
	var tableExists, claimHashesExist, randomizedValidatorExists bool
	var cohortPlanColumnExists, cohortV2ValidatorExists bool
	if err := migratePool.QueryRow(ctx, `
SELECT to_regclass('aura.adaptive_randomization_receipts') IS NOT NULL,
       EXISTS (
           SELECT 1 FROM information_schema.columns
           WHERE table_schema='aura'
             AND table_name='adaptive_focal_cohort_claims'
             AND column_name='analysis_stratum_id'
       ),
       to_regprocedure(
           'aura.adaptive_schema2_randomized_assignment_payload_valid(jsonb,uuid,text,uuid)'
       ) IS NOT NULL,
       EXISTS (
           SELECT 1 FROM information_schema.columns
           WHERE table_schema='aura'
             AND table_name='adaptive_focal_cohorts'
             AND column_name='randomization_plan_artifact'
       ),
       to_regprocedure(
           'aura.adaptive_focal_cohort_v2_artifact_valid(bytea,jsonb,bytea,jsonb,uuid,text,text,bigint,text,uuid,bytea,text,text,text,bigint,bytea,text,timestamptz)'
       ) IS NOT NULL`).Scan(
		&tableExists, &claimHashesExist, &randomizedValidatorExists,
		&cohortPlanColumnExists, &cohortV2ValidatorExists,
	); err != nil {
		t.Fatal(err)
	}
	if tableExists || claimHashesExist || randomizedValidatorExists ||
		cohortPlanColumnExists || cohortV2ValidatorExists {
		t.Fatalf(
			"0075 down retained receipt/claim-validator/cohort-v2 state = %t/%t/%t/%t/%t",
			tableExists, claimHashesExist, randomizedValidatorExists,
			cohortPlanColumnExists, cohortV2ValidatorExists,
		)
	}

	if err := db.MigrateSteps(ctx, migrateURL, 1); err != nil {
		t.Fatalf("migrate 0075 re-up: %v", err)
	}
	assertSchema2MigrationVersion(t, ctx, migrateURL, 75)
	assertRandomizationMigrationContract(t, ctx, migratePool)
}

func assertRandomizationMigrationContract(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
) {
	t.Helper()
	var (
		owner         string
		rls           bool
		appSelect     bool
		appInsert     bool
		appUpdate     bool
		appDelete     bool
		appTruncate   bool
		appReferences bool
		appTrigger    bool
	)
	if err := pool.QueryRow(ctx, `
SELECT pg_get_userbyid(c.relowner), c.relrowsecurity,
       has_table_privilege('aura_app', c.oid, 'SELECT'),
       has_table_privilege('aura_app', c.oid, 'INSERT'),
       has_table_privilege('aura_app', c.oid, 'UPDATE'),
       has_table_privilege('aura_app', c.oid, 'DELETE'),
       has_table_privilege('aura_app', c.oid, 'TRUNCATE'),
       has_table_privilege('aura_app', c.oid, 'REFERENCES'),
       has_table_privilege('aura_app', c.oid, 'TRIGGER')
FROM pg_class AS c
JOIN pg_namespace AS n ON n.oid=c.relnamespace
WHERE n.nspname='aura'
  AND c.relname='adaptive_randomization_receipts'`).Scan(
		&owner, &rls, &appSelect, &appInsert, &appUpdate,
		&appDelete, &appTruncate, &appReferences, &appTrigger,
	); err != nil {
		t.Fatal(err)
	}
	if owner != "aura_migrate" || !rls || !appSelect || !appInsert ||
		appUpdate || appDelete || appTruncate || appReferences || appTrigger {
		t.Fatalf(
			"unsafe receipt table owner=%q rls=%t acl=(%t,%t,%t,%t,%t,%t,%t)",
			owner, rls, appSelect, appInsert, appUpdate, appDelete,
			appTruncate, appReferences, appTrigger,
		)
	}

	functions := []struct {
		name       string
		definer    bool
		appExecute bool
	}{
		{"adaptive_schema2_randomized_assignment_payload_valid", false, true},
		{"adaptive_child_artifact_ref_valid", false, true},
		{"adaptive_focal_cohort_v2_artifact_valid", false, true},
		{"adaptive_randomization_receipt_artifact_valid", false, true},
		{"enforce_adaptive_randomization_receipt_binding", true, false},
		{"reject_adaptive_randomization_receipt_mutation", false, false},
	}
	for _, function := range functions {
		var definer, appExecute, publicExecute bool
		var searchPath string
		if err := pool.QueryRow(ctx, `
SELECT p.prosecdef, COALESCE(p.proconfig::text, ''),
       has_function_privilege('aura_app', p.oid, 'EXECUTE'),
       EXISTS (
           SELECT 1
           FROM aclexplode(COALESCE(p.proacl, acldefault('f', p.proowner))) AS acl
           WHERE acl.grantee=0 AND acl.privilege_type='EXECUTE'
       )
FROM pg_proc AS p
JOIN pg_namespace AS n ON n.oid=p.pronamespace
WHERE n.nspname='aura' AND p.proname=$1`, function.name).Scan(
			&definer, &searchPath, &appExecute, &publicExecute,
		); err != nil {
			t.Fatal(err)
		}
		if definer != function.definer ||
			searchPath != "{search_path=pg_catalog}" ||
			appExecute != function.appExecute || publicExecute {
			t.Fatalf(
				"unsafe %s definer=%t path=%q app=%t public=%t",
				function.name, definer, searchPath, appExecute, publicExecute,
			)
		}
	}

	var policy string
	if err := pool.QueryRow(ctx, `
SELECT pg_get_expr(polqual, polrelid)
FROM pg_policy
WHERE polname='adaptive_randomization_receipts_owner_isolation'`).Scan(
		&policy,
	); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(policy, "app.current_identity") ||
		!strings.Contains(policy, "owner_id") {
		t.Fatalf("receipt RLS policy = %q, want owner identity scope", policy)
	}
}
