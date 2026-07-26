//go:build db_integration

package adaptive

import (
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

func TestEvidenceStoreAtomicAdmissionRequiresCapabilityWithoutPartialWrites(
	t *testing.T,
) {
	ctx, cancel := context.WithTimeout(t.Context(), 120*time.Second)
	t.Cleanup(cancel)
	pool, migrateURL := schema2MigrationDatabase(
		t, ctx, "aura_evidence_capability", shippedMigrationSteps(t),
	)
	ownerID := uuid.Must(uuid.NewV7())
	if _, err := pool.Exec(
		ctx,
		`INSERT INTO aura.identities (id, name, kind)
		 VALUES ($1, $2, 'user')`,
		ownerID, "evidence-capability-"+ownerID.String(),
	); err != nil {
		t.Fatalf("seed evidence owner: %v", err)
	}
	snapshot := focalCohortSnapshotForOwner(t, ownerID)
	if err := NewSnapshotStore(pool).Save(ctx, snapshot); err != nil {
		t.Fatalf("save evidence snapshot: %v", err)
	}
	cohort, registrations := admissionEvidenceFixture(
		t,
		ownerID,
		snapshot,
	)
	if err := NewCohortStore(pool, NewStore(pool, StoreConfig{})).
		Save(ctx, cohort); err != nil {
		t.Fatalf("save evidence cohort: %v", err)
	}
	operationCtx := admissionOperationContext(
		t,
		ctx,
		pool,
		"missing-capability",
		ownerID,
		cohort.ID(),
		registrations,
	)
	_, err := NewEvidenceStore(pool).SealCanaryAdmission(
		operationCtx,
		ownerID,
		cohort.ID(),
		EvidenceSealer{
			ActorID: ownerID, Capability: "adaptive.evidence.seal",
		},
		admissionTestManifestSHA256(),
		registrations,
	)
	if err == nil || !strings.Contains(
		err.Error(),
		"adaptive evidence sealer capability is unavailable",
	) {
		t.Fatalf("SealCanaryAdmission error = %v, want capability denial", err)
	}
	migrationPool, err := db.Open(ctx, &db.Config{URL: migrateURL})
	if err != nil {
		t.Fatalf("open migration pool: %v", err)
	}
	t.Cleanup(migrationPool.Close)
	var artifacts int
	if err := migrationPool.QueryRow(
		ctx,
		`SELECT count(*) FROM aura.adaptive_evidence_artifacts
		  WHERE owner_id = $1`,
		ownerID,
	).Scan(&artifacts); err != nil {
		t.Fatalf("count unauthorized artifacts: %v", err)
	}
	if artifacts != 0 {
		t.Fatalf("unauthorized artifact rows = %d, want zero", artifacts)
	}
	if _, err := pool.Exec(
		ctx,
		`INSERT INTO aura.capability_grants (identity_id, capability)
		 VALUES ($1, 'adaptive.evidence.seal')`,
		ownerID,
	); err != nil {
		t.Fatalf("grant evidence capability: %v", err)
	}
	if _, err := NewEvidenceStore(pool).SealCanaryAdmission(
		operationCtx,
		ownerID,
		cohort.ID(),
		EvidenceSealer{
			ActorID: ownerID, Capability: "adaptive.evidence.seal",
		},
		admissionTestManifestSHA256(),
		registrations,
	); err != nil {
		t.Fatalf("SealCanaryAdmission after capability grant: %v", err)
	}
	assertAdmissionRowCounts(
		t,
		ctx,
		migrationPool,
		ownerID,
		cohort.ID(),
		7,
		1,
	)
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
	).Scan(
		&legacyRemoved,
		&directInsert,
		&transitionExecute,
	); err != nil {
		t.Fatalf("read adaptive evidence privileges: %v", err)
	}
	if !legacyRemoved || directInsert || !transitionExecute {
		t.Fatalf(
			"evidence privileges legacy=%t insert=%t transition=%t",
			legacyRemoved,
			directInsert,
			transitionExecute,
		)
	}
}

func TestEvidenceStoreAtomicAdmissionValidatesReceiptAndIgnoresForeignApproval(
	t *testing.T,
) {
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
	snapshot := focalCohortSnapshotForOwner(t, ownerID)
	if err := NewSnapshotStore(pool).Save(ctx, snapshot); err != nil {
		t.Fatalf("save snapshot: %v", err)
	}
	cohort, artifacts := admissionEvidenceFixture(t, ownerID, snapshot)
	if err := NewCohortStore(pool, NewStore(pool, StoreConfig{})).
		Save(ctx, cohort); err != nil {
		t.Fatalf("save admission cohort: %v", err)
	}
	sealer := EvidenceSealer{
		ActorID: ownerID, Capability: "adaptive.evidence.seal",
	}
	store := NewEvidenceStore(pool)
	migrationPool, err := db.Open(ctx, &db.Config{URL: migrateURL})
	if err != nil {
		t.Fatalf("open migration pool: %v", err)
	}
	t.Cleanup(migrationPool.Close)
	if _, err := pool.Exec(
		ctx,
		`INSERT INTO aura.capability_grants (identity_id, capability)
		 VALUES ($1, 'adaptive.evidence.seal')`,
		ownerID,
	); err != nil {
		t.Fatalf("grant evidence sealer capability: %v", err)
	}
	if _, err := store.SealCanaryAdmission(
		ctx,
		ownerID,
		cohort.ID(),
		sealer,
		admissionTestManifestSHA256(),
		artifacts,
	); err == nil {
		t.Fatal("SealCanaryAdmission accepted a missing operation")
	}
	assertAdmissionRowCounts(t, ctx, migrationPool, ownerID, cohort.ID(), 0, 0)

	unrelatedOperation := admissionOperationContextForManifest(
		t,
		ctx,
		pool,
		"unrelated-semantic-receipt",
		repeatedSHA("d"),
		ownerID,
		cohort.ID(),
		artifacts,
	)
	if _, err := store.SealCanaryAdmission(
		unrelatedOperation,
		ownerID,
		cohort.ID(),
		sealer,
		admissionTestManifestSHA256(),
		artifacts,
	); err == nil {
		t.Fatal("SealCanaryAdmission accepted a valid unrelated receipt")
	}
	assertAdmissionRowCounts(t, ctx, migrationPool, ownerID, cohort.ID(), 0, 0)

	foreignApproval := OperatorApproval{
		SchemaID: "aura.adaptive.operator-approval/v1", Revision: 1,
		ApprovalID: uuid.Must(uuid.NewV7()).String(),
		ActorID:    ownerID.String(), Capability: "adaptive.evidence.seal",
		TargetState: string(PolicyCanary), OwnerID: ownerID.String(),
		CohortID: cohort.ID().String(), CohortSHA256: cohort.SHA256(),
		PolicyEpoch:      cohort.Scope().PolicyEpoch,
		PolicyVersion:    cohort.Scope().PolicyVersion,
		ApprovedAt:       time.Now().UTC().Truncate(time.Microsecond),
		SourceRequestID:  uuid.Must(uuid.NewV7()).String(),
		ProvenanceSHA256: repeatedSHA("f"),
	}
	foreignCanonical, err := foreignApproval.CanonicalJSON()
	if err != nil {
		t.Fatalf("canonicalize foreign approval: %v", err)
	}
	if _, err := migrationPool.Exec(
		ctx,
		`INSERT INTO aura.adaptive_evidence_artifacts (
		   id, owner_id, cohort_id, kind, schema_id, revision,
		   sha256, artifact, artifact_json
		 ) VALUES (
		   $1, $2, $3, 'operator_approval',
		   'aura.adaptive.operator-approval/v1', 1,
		   pg_catalog.sha256($4), $4, convert_from($4, 'UTF8')::jsonb
		 )`,
		foreignApproval.ApprovalID,
		ownerID,
		cohort.ID(),
		foreignCanonical,
	); err != nil {
		t.Fatalf("seed foreign legacy approval: %v", err)
	}
	operationCtx := admissionOperationContext(
		t,
		ctx,
		pool,
		"foreign-approval",
		ownerID,
		cohort.ID(),
		artifacts,
	)
	admission, err := store.SealCanaryAdmission(
		operationCtx,
		ownerID,
		cohort.ID(),
		sealer,
		admissionTestManifestSHA256(),
		artifacts,
	)
	if err != nil {
		t.Fatalf("SealCanaryAdmission: %v", err)
	}
	if admission.Body.OperatorApprovalRef.ArtifactID ==
		foreignApproval.ApprovalID {
		t.Fatal("foreign approval poisoned the derived approval reference")
	}
	assertAdmissionRowCounts(t, ctx, migrationPool, ownerID, cohort.ID(), 8, 1)
}

func assertAdmissionRowCounts(
	t *testing.T,
	ctx context.Context,
	pool interface {
		QueryRow(context.Context, string, ...any) pgx.Row
	},
	ownerID uuid.UUID,
	cohortID uuid.UUID,
	wantArtifacts int,
	wantEvidence int,
) {
	t.Helper()
	var artifacts, evidence int
	if err := pool.QueryRow(
		ctx,
		`SELECT
		   (SELECT count(*) FROM aura.adaptive_evidence_artifacts
		     WHERE owner_id = $1 AND cohort_id = $2),
		   (SELECT count(*) FROM aura.adaptive_sealed_evidence
		     WHERE owner_id = $1 AND cohort_id = $2)`,
		ownerID,
		cohortID,
	).Scan(&artifacts, &evidence); err != nil {
		t.Fatalf("count admission rows: %v", err)
	}
	if artifacts != wantArtifacts || evidence != wantEvidence {
		t.Fatalf(
			"admission artifact/evidence rows = %d/%d, want %d/%d",
			artifacts,
			evidence,
			wantArtifacts,
			wantEvidence,
		)
	}
}
