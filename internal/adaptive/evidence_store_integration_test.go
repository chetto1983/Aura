//go:build db_integration

package adaptive

import (
	"bytes"
	"context"
	"strings"
	"testing"
	"time"

	"github.com/chetto1983/aura/internal/db"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

func TestAdaptiveSealedEvidenceMigration0076DownUp(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 120*time.Second)
	t.Cleanup(cancel)
	_, migrateURL := schema2MigrationDatabase(
		t, ctx, "aura_evidence_migration", 75,
	)
	migrationPool, err := db.Open(ctx, &db.Config{URL: migrateURL})
	if err != nil {
		t.Fatalf("open migration pool: %v", err)
	}
	t.Cleanup(migrationPool.Close)
	ownerID := uuid.Must(uuid.NewV7())
	evidenceID := uuid.Must(uuid.NewV7())
	transitionID := uuid.Must(uuid.NewV7())
	if _, err := migrationPool.Exec(
		ctx,
		`INSERT INTO aura.identities (id, name, kind)
		 VALUES ($1, $2, 'user')`,
		ownerID, "legacy-evidence-"+ownerID.String(),
	); err != nil {
		t.Fatalf("seed legacy evidence owner: %v", err)
	}
	if _, err := migrationPool.Exec(
		ctx,
		`INSERT INTO aura.adaptive_promotion_evidence (
		   id, actor_id, policy_version, model_id, cohort_id,
		   evidence_hash, report
		 ) VALUES (
		   $1, $2, 'legacy-v1', 'legacy-model', 'legacy-cohort',
		   decode(repeat('ab', 32), 'hex'), '{}'::jsonb
		 )`,
		evidenceID, ownerID,
	); err != nil {
		t.Fatalf("seed legacy promotion evidence: %v", err)
	}
	if _, err := migrationPool.Exec(
		ctx,
		`INSERT INTO aura.adaptive_policy_transitions (
		   id, actor_id, from_epoch, to_epoch, from_mode, to_mode,
		   policy_version, rollout_bps, evidence_id, reason
		 ) VALUES (
		   $1, $2, 1, 2, 'shadow', 'canary', 'legacy-v1', 2500,
		   $3, 'legacy transition'
		 )`,
		transitionID, ownerID, evidenceID,
	); err != nil {
		t.Fatalf("seed legacy evidence transition: %v", err)
	}
	assertAdaptiveEvidenceMigrationPresence(t, ctx, migrationPool, false)

	if err := db.MigrateSteps(ctx, migrateURL, 1); err != nil {
		t.Fatalf("migrate 0076 up over legacy evidence: %v", err)
	}
	assertSchema2MigrationVersion(t, ctx, migrateURL, 76)
	assertAdaptiveEvidenceMigrationPresence(t, ctx, migrationPool, true)
	assertLegacyTransitionPreserved(t, ctx, migrationPool, transitionID, evidenceID)

	if err := db.MigrateSteps(ctx, migrateURL, -1); err != nil {
		t.Fatalf("migrate 0076 down: %v", err)
	}
	assertSchema2MigrationVersion(t, ctx, migrateURL, 75)
	assertAdaptiveEvidenceMigrationPresence(t, ctx, migrationPool, false)
	var storedEvidenceID uuid.UUID
	if err := migrationPool.QueryRow(
		ctx,
		`SELECT evidence_id FROM aura.adaptive_policy_transitions WHERE id = $1`,
		transitionID,
	).Scan(&storedEvidenceID); err != nil || storedEvidenceID != evidenceID {
		t.Fatalf("legacy transition after down = %s, error = %v", storedEvidenceID, err)
	}

	if err := db.MigrateSteps(ctx, migrateURL, 1); err != nil {
		t.Fatalf("migrate 0076 re-up: %v", err)
	}
	assertSchema2MigrationVersion(t, ctx, migrateURL, 76)
	assertAdaptiveEvidenceMigrationPresence(t, ctx, migrationPool, true)
	assertLegacyTransitionPreserved(t, ctx, migrationPool, transitionID, evidenceID)
}

func assertLegacyTransitionPreserved(
	t *testing.T,
	ctx context.Context,
	pool interface {
		QueryRow(context.Context, string, ...any) pgx.Row
	},
	transitionID uuid.UUID,
	evidenceID uuid.UUID,
) {
	t.Helper()
	var storedEvidenceID uuid.UUID
	var typedColumnsNull bool
	if err := pool.QueryRow(
		ctx,
		`SELECT evidence_id,
		        evidence_kind IS NULL AND evidence_hash IS NULL
		        AND transition_binding IS NULL
		        AND transition_binding_json IS NULL
		   FROM aura.adaptive_policy_transitions
		  WHERE id = $1`,
		transitionID,
	).Scan(&storedEvidenceID, &typedColumnsNull); err != nil {
		t.Fatalf("read preserved legacy transition: %v", err)
	}
	if storedEvidenceID != evidenceID || !typedColumnsNull {
		t.Fatalf(
			"legacy transition evidence/nulls = %s/%t, want %s/true",
			storedEvidenceID, typedColumnsNull, evidenceID,
		)
	}
}

func assertAdaptiveEvidenceMigrationPresence(
	t *testing.T,
	ctx context.Context,
	pool interface {
		QueryRow(context.Context, string, ...any) pgx.Row
	},
	present bool,
) {
	t.Helper()
	var sealedTable, typedTransition, legacyTransition bool
	if err := pool.QueryRow(
		ctx,
		`SELECT
		    to_regclass('aura.adaptive_sealed_evidence') IS NOT NULL,
		    to_regprocedure(
		      'aura.apply_adaptive_policy_transition(uuid,bigint,uuid,integer,text)'
		    ) IS NOT NULL,
		    to_regprocedure(
		      'aura.apply_adaptive_policy_transition(uuid,bigint,text,text,integer,jsonb,uuid,text,text,bytea,jsonb,text)'
		    ) IS NOT NULL`,
	).Scan(&sealedTable, &typedTransition, &legacyTransition); err != nil {
		t.Fatalf("read adaptive evidence migration state: %v", err)
	}
	if sealedTable != present || typedTransition != present ||
		legacyTransition == present {
		t.Fatalf(
			"evidence migration table/typed/legacy = %t/%t/%t, present=%t",
			sealedTable, typedTransition, legacyTransition, present,
		)
	}
}

func TestEvidenceStorePersistsAndVerifiesCohortChildArtifact(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 120*time.Second)
	t.Cleanup(cancel)
	pool, migrateURL := schema2MigrationDatabase(
		t, ctx, "aura_evidence_store", shippedMigrationSteps(t),
	)
	ownerID := uuid.Must(uuid.NewV7())
	if _, err := pool.Exec(
		ctx,
		`INSERT INTO aura.identities (id, name, kind)
		 VALUES ($1, $2, 'user')`,
		ownerID, "evidence-store-"+ownerID.String(),
	); err != nil {
		t.Fatalf("seed evidence owner: %v", err)
	}
	if _, err := pool.Exec(
		ctx,
		`INSERT INTO aura.capability_grants (identity_id, capability)
		 VALUES ($1, 'adaptive.evidence.seal')`,
		ownerID,
	); err != nil {
		t.Fatalf("grant evidence sealer capability: %v", err)
	}
	snapshot := focalCohortSnapshotForOwner(t, ownerID)
	if err := NewSnapshotStore(pool).Save(ctx, snapshot); err != nil {
		t.Fatalf("save snapshot: %v", err)
	}
	cluster, err := NewInterferenceCluster(ownerID.String(), ownerID.String())
	if err != nil {
		t.Fatalf("derive interference cluster schema: %v", err)
	}
	interference := validInterferencePlanArtifact()
	interference.ClusterSchemaSHA256 = cluster.SchemaSHA256
	canonical, err := interference.CanonicalJSON()
	if err != nil {
		t.Fatalf("canonicalize interference plan: %v", err)
	}
	refs := validCohortV2ChildRefs()
	refs.InterferencePlan.ArtifactSHA256 = sha256Hex(canonical)
	base := randomizedFocalCohortForSnapshot(
		t, snapshot, time.Now().UTC().Add(time.Hour),
		[]BlockKey{BlockOwner, BlockTimeBlock},
	)
	cohort, err := NewRandomizedFocalCohort(base.spec, snapshot, refs)
	if err != nil {
		t.Fatalf("build cohort with stored child: %v", err)
	}
	if err := NewCohortStore(pool, NewStore(pool, StoreConfig{})).
		Save(ctx, cohort); err != nil {
		t.Fatalf("save cohort: %v", err)
	}
	sealer := EvidenceSealer{
		ActorID: ownerID, Capability: "adaptive.evidence.seal",
	}
	registration := EvidenceArtifactRegistration{
		Ref: refs.InterferencePlan, Canonical: canonical,
	}
	store := NewEvidenceStore(pool)
	if err := store.RegisterCohortArtifact(
		ctx, ownerID, cohort.ID(), sealer, registration,
	); err != nil {
		t.Fatalf("RegisterCohortArtifact: %v", err)
	}
	if err := store.RegisterCohortArtifact(
		ctx, ownerID, cohort.ID(), sealer, registration,
	); err != nil {
		t.Fatalf("RegisterCohortArtifact retry: %v", err)
	}

	migrationPool, err := db.Open(ctx, &db.Config{URL: migrateURL})
	if err != nil {
		t.Fatalf("open migration pool: %v", err)
	}
	t.Cleanup(migrationPool.Close)
	var legacyRemoved, directInsert, transitionExecute bool
	if err := migrationPool.QueryRow(
		ctx,
		`SELECT
		    to_regprocedure(
		      'aura.apply_adaptive_policy_transition(uuid,bigint,text,text,integer,jsonb,uuid,text,text,bytea,jsonb,text)'
		    ) IS NULL,
		    has_table_privilege(
		      'aura_app', 'aura.adaptive_sealed_evidence', 'INSERT'
		    ),
		    has_function_privilege(
		      'aura_app',
		      'aura.apply_adaptive_policy_transition(uuid,bigint,uuid,integer,text)',
		      'EXECUTE'
		    )`,
	).Scan(&legacyRemoved, &directInsert, &transitionExecute); err != nil {
		t.Fatalf("read adaptive evidence privileges: %v", err)
	}
	if !legacyRemoved || directInsert || !transitionExecute {
		t.Fatalf(
			"evidence privileges legacy=%t insert=%t transition=%t",
			legacyRemoved, directInsert, transitionExecute,
		)
	}
	var stored []byte
	var storedSHA256 string
	var documentMatches bool
	if err := migrationPool.QueryRow(
		ctx,
		`SELECT artifact, encode(sha256, 'hex'),
		        artifact_json = convert_from($1, 'UTF8')::jsonb
		   FROM aura.adaptive_evidence_artifacts
		  WHERE id = $2 AND owner_id = $3 AND cohort_id = $4
		    AND kind = 'interference_plan'`,
		canonical, refs.InterferencePlan.ArtifactID, ownerID, cohort.ID(),
	).Scan(&stored, &storedSHA256, &documentMatches); err != nil {
		t.Fatalf("read stored cohort child: %v", err)
	}
	if !bytes.Equal(stored, canonical) ||
		storedSHA256 != refs.InterferencePlan.ArtifactSHA256 ||
		!documentMatches {
		t.Fatalf(
			"stored child = (%s, %q, %t), want exact canonical artifact",
			stored, storedSHA256, documentMatches,
		)
	}

	if _, err := migrationPool.Exec(
		ctx,
		`ALTER TABLE aura.adaptive_evidence_artifacts DISABLE TRIGGER USER`,
	); err != nil {
		t.Fatalf("disable evidence artifact triggers: %v", err)
	}
	t.Cleanup(func() {
		if _, err := migrationPool.Exec(
			context.Background(),
			`ALTER TABLE aura.adaptive_evidence_artifacts ENABLE TRIGGER USER`,
		); err != nil {
			t.Errorf("enable evidence artifact triggers: %v", err)
		}
	})
	tampered := []byte(
		`{"schema_id":"aura.adaptive.interference-plan/v1","revision":1,"cluster_schema_id":"aura.adaptive.interference-cluster/conversation/v1","cluster_revision":1,"cluster_schema_sha256":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","cluster_keys":["owner_id","evaluation_conversation_id"],"carryover_scope":"session","randomized_focal_units_per_cluster":1,"within_cluster_rule":"arbitrary_carryover_one_focal","between_cluster_assumption":"no_treatment_induced_cross_conversation_interference","shared_state_rule":"read_only_or_versioned","fold_unit":"interference_cluster","resampling_unit":"interference_cluster"}`,
	)
	if _, err := migrationPool.Exec(
		ctx,
		`UPDATE aura.adaptive_evidence_artifacts
		    SET sha256 = pg_catalog.sha256($1),
		        artifact = $1,
		        artifact_json = convert_from($1, 'UTF8')::jsonb
		  WHERE id = $2`,
		tampered, refs.InterferencePlan.ArtifactID,
	); err != nil {
		t.Fatalf("tamper stored cohort child: %v", err)
	}

	_, err = store.SealCanaryAdmission(
		ctx, ownerID, cohort.ID(), sealer,
	)
	if err == nil ||
		!strings.Contains(
			err.Error(),
			"persisted adaptive evidence cohort artifact is invalid",
		) {
		t.Fatalf("SealCanaryAdmission error = %v, want invalid stored child", err)
	}
	var sealed int
	if err := migrationPool.QueryRow(
		ctx,
		`SELECT count(*) FROM aura.adaptive_sealed_evidence
		  WHERE owner_id = $1 AND cohort_id = $2`,
		ownerID, cohort.ID(),
	).Scan(&sealed); err != nil {
		t.Fatalf("count sealed evidence: %v", err)
	}
	if sealed != 0 {
		t.Fatalf("sealed evidence rows = %d, want none after child corruption", sealed)
	}
}
