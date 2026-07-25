//go:build db_integration

package adaptive

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/chetto1983/aura/internal/db"
	"github.com/chetto1983/aura/internal/db/sqlc"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestSnapshotMigrationUpgradeFrom0067DownAndUp(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 120*time.Second)
	defer cancel()
	pool, migrateURL := schema2MigrationDatabase(
		t,
		ctx,
		"aura_snapshot_upgrade",
		67,
	)
	before := readOutcomeChainFunctionContract(t, ctx, pool)

	if err := db.MigrateSteps(ctx, migrateURL, 1); err != nil {
		t.Fatalf("upgrade 0067 to 0068: %v", err)
	}
	assertSchema2MigrationVersion(t, ctx, migrateURL, 68)
	if err := db.CheckMigrationHead(ctx, migrateURL); err != nil {
		t.Fatalf("CheckMigrationHead() at 0068: %v", err)
	}
	assertOutcomeChainFunctionContractPreserved(t, ctx, pool, before)
	assertSnapshotMigrationContract(t, ctx, pool)

	owner := seedTypedLedgerOwner(t, pool)
	snapshot := snapshotForOwner(t, owner)
	store := NewSnapshotStore(pool)
	if err := store.Save(ctx, snapshot); err != nil {
		t.Fatalf("Save() after upgrade: %v", err)
	}
	var row sqlc.AuraAdaptivePolicySnapshots
	if err := db.WithIdentityTx(
		ctx,
		pool,
		owner.String(),
		func(q *sqlc.Queries) error {
			row = snapshotRowForOwner(t, ctx, q, owner, snapshot.ID())
			return nil
		},
	); err != nil {
		t.Fatal(err)
	}
	assertSnapshotRowMatches(t, row, snapshot)
	assertSnapshotValidatorRejectsPrivateCatalogAlias(
		t,
		ctx,
		pool,
		snapshot,
	)

	if err := db.MigrateSteps(ctx, migrateURL, -1); err != nil {
		t.Fatalf("downgrade 0068 to 0067: %v", err)
	}
	assertSchema2MigrationVersion(t, ctx, migrateURL, 67)
	assertOutcomeChainFunctionContractPreserved(t, ctx, pool, before)
	var table *string
	if err := pool.QueryRow(
		ctx,
		`SELECT to_regclass('aura.adaptive_policy_snapshots')::text`,
	).Scan(&table); err != nil {
		t.Fatal(err)
	}
	if table != nil {
		t.Fatalf("0068 down retained snapshot table %q", *table)
	}

	if err := db.MigrateSteps(ctx, migrateURL, 1); err != nil {
		t.Fatalf("re-upgrade 0067 to 0068: %v", err)
	}
	assertSchema2MigrationVersion(t, ctx, migrateURL, 68)
	assertOutcomeChainFunctionContractPreserved(t, ctx, pool, before)
	assertSnapshotMigrationContract(t, ctx, pool)
}

func assertSnapshotValidatorRejectsPrivateCatalogAlias(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
	snapshot *PolicySnapshot,
) {
	t.Helper()
	artifact, err := snapshot.canonicalArtifact()
	if err != nil {
		t.Fatal(err)
	}
	featureSchema, err := json.Marshal(snapshot.FeatureSchema())
	if err != nil {
		t.Fatal(err)
	}
	actionCatalog, err := json.Marshal(snapshot.Actions())
	if err != nil {
		t.Fatal(err)
	}
	privateArtifact := bytes.Replace(
		artifact,
		[]byte(`"action_id":"deep"`),
		[]byte(`"action_id":"digest"`),
		1,
	)
	privateCatalog := bytes.Replace(
		actionCatalog,
		[]byte(`"action_id":"deep"`),
		[]byte(`"action_id":"digest"`),
		1,
	)
	scope := snapshot.Scope()
	var valid bool
	if err := pool.QueryRow(
		ctx,
		`SELECT aura.adaptive_policy_snapshot_artifact_valid(
		    $1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11
		 )`,
		privateArtifact,
		scope.OwnerID,
		scope.Domain,
		scope.Point,
		scope.ProviderID,
		scope.ModelID,
		scope.PolicyVersion,
		scope.TrainingCutoff,
		featureSchema,
		privateCatalog,
		snapshot.ChampionActionID(),
	).Scan(&valid); err != nil {
		t.Fatal(err)
	}
	if valid {
		t.Fatal("snapshot validator accepted private action catalog alias")
	}
}

func TestSnapshotMigrationEnforcesLeastPrivilegeAndCanonicalRows(t *testing.T) {
	pool := adaptiveIntegrationPool(t)
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
	defer cancel()
	owner := seedTypedLedgerOwner(t, pool)
	snapshot := snapshotForOwner(t, owner)
	store := NewSnapshotStore(pool)
	if err := store.Save(ctx, snapshot); err != nil {
		t.Fatal(err)
	}

	for name, statement := range map[string]string{
		"update": `UPDATE aura.adaptive_policy_snapshots
			SET policy_version='tampered' WHERE id=$1`,
		"delete":   `DELETE FROM aura.adaptive_policy_snapshots WHERE id=$1`,
		"truncate": `TRUNCATE aura.adaptive_policy_snapshots`,
	} {
		t.Run(name, func(t *testing.T) {
			var arguments []any
			if name != "truncate" {
				arguments = []any{snapshot.ID()}
			}
			_, err := pool.Exec(ctx, statement, arguments...)
			var pgErr *pgconn.PgError
			if !errors.As(err, &pgErr) || pgErr.Code != "42501" {
				t.Fatalf("raw %s error = %v, want SQLSTATE 42501", name, err)
			}
		})
	}

	artifact, err := snapshot.canonicalArtifact()
	if err != nil {
		t.Fatal(err)
	}
	err = db.WithIdentityTxRaw(ctx, pool, owner.String(), func(tx pgx.Tx) error {
		_, insertErr := tx.Exec(
			ctx,
			`INSERT INTO aura.adaptive_policy_snapshots (
			    id, owner_id, sha256, schema_version, domain, decision_point,
			    provider_id, model_id, policy_version, training_cutoff,
			    feature_schema, action_catalog, champion_action_id, artifact
			 ) VALUES (
			    gen_random_uuid(), $1, sha256($2), '1.0', 'reasoning', 'reasoning',
			    'openrouter', 'production-model-v1', 'reasoning-policy-v1',
			    '2026-07-24T12:00:00Z',
			    '[]'::jsonb, '[]'::jsonb, 'static', $2
			 )`,
			owner,
			artifact,
		)
		return insertErr
	})
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) || pgErr.Code != "23514" {
		t.Fatalf("malformed raw insert error = %v, want SQLSTATE 23514", err)
	}
}

func assertSnapshotMigrationContract(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
) {
	t.Helper()
	var (
		owner             string
		rlsEnabled        bool
		appSelect         bool
		appInsert         bool
		appUpdate         bool
		appDelete         bool
		appTruncate       bool
		snapshotFunctions int
	)
	if err := pool.QueryRow(
		ctx,
		`SELECT pg_get_userbyid(c.relowner),
		        c.relrowsecurity,
		        has_table_privilege('aura_app', c.oid, 'SELECT'),
		        has_table_privilege('aura_app', c.oid, 'INSERT'),
		        has_table_privilege('aura_app', c.oid, 'UPDATE'),
		        has_table_privilege('aura_app', c.oid, 'DELETE'),
		        has_table_privilege('aura_app', c.oid, 'TRUNCATE'),
		        (
		            SELECT count(*)
		            FROM pg_proc AS p
		            JOIN pg_namespace AS n ON n.oid=p.pronamespace
		            WHERE n.nspname='aura'
		              AND p.proname LIKE 'adaptive%policy%snapshot%'
		        )
		 FROM pg_class AS c
		 JOIN pg_namespace AS n ON n.oid=c.relnamespace
		 WHERE n.nspname='aura' AND c.relname='adaptive_policy_snapshots'`,
	).Scan(
		&owner,
		&rlsEnabled,
		&appSelect,
		&appInsert,
		&appUpdate,
		&appDelete,
		&appTruncate,
		&snapshotFunctions,
	); err != nil {
		t.Fatal(err)
	}
	if owner != "aura_migrate" ||
		!rlsEnabled ||
		!appSelect ||
		!appInsert ||
		appUpdate ||
		appDelete ||
		appTruncate ||
		snapshotFunctions != 1 {
		t.Fatalf(
			"unsafe snapshot migration contract: owner=%q rls=%t acl=(%t,%t,%t,%t,%t) functions=%d",
			owner,
			rlsEnabled,
			appSelect,
			appInsert,
			appUpdate,
			appDelete,
			appTruncate,
			snapshotFunctions,
		)
	}
	var (
		functionOwner    string
		securityDefiner  bool
		searchPath       string
		appCanExecute    bool
		publicCanExecute bool
	)
	if err := pool.QueryRow(
		ctx,
		`SELECT pg_get_userbyid(p.proowner),
		        p.prosecdef,
		        COALESCE(p.proconfig::text, ''),
		        has_function_privilege('aura_app', p.oid, 'EXECUTE'),
		        EXISTS (
		            SELECT 1
		            FROM aclexplode(
		                COALESCE(p.proacl, acldefault('f', p.proowner))
		            ) AS acl
		            WHERE acl.grantee=0 AND acl.privilege_type='EXECUTE'
		        )
		 FROM pg_proc AS p
		 JOIN pg_namespace AS n ON n.oid=p.pronamespace
		 WHERE n.nspname='aura'
		   AND p.proname='adaptive_policy_snapshot_artifact_valid'`,
	).Scan(
		&functionOwner,
		&securityDefiner,
		&searchPath,
		&appCanExecute,
		&publicCanExecute,
	); err != nil {
		t.Fatal(err)
	}
	if functionOwner != "aura_migrate" ||
		securityDefiner ||
		searchPath != "{search_path=pg_catalog}" ||
		!appCanExecute ||
		publicCanExecute {
		t.Fatalf(
			"unsafe snapshot validator: owner=%q definer=%t path=%q app=%t public=%t",
			functionOwner,
			securityDefiner,
			searchPath,
			appCanExecute,
			publicCanExecute,
		)
	}
	var policyDefinition string
	if err := pool.QueryRow(
		ctx,
		`SELECT pg_get_expr(polqual, polrelid)
		 FROM pg_policy
		 WHERE polname='adaptive_policy_snapshots_owner_isolation'`,
	).Scan(&policyDefinition); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(policyDefinition, "app.current_identity") ||
		!strings.Contains(policyDefinition, "owner_id") {
		t.Fatalf("snapshot RLS policy = %q, want identity owner scope", policyDefinition)
	}
}

func openSnapshotMigrationPool(
	t *testing.T,
	ctx context.Context,
	migrateURL string,
) *pgxpool.Pool {
	t.Helper()
	pool, err := db.Open(ctx, &db.Config{URL: migrateURL})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	return pool
}
