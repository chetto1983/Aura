//go:build db_integration

package adaptive

import (
	"bytes"
	"context"
	"errors"
	"testing"
	"time"

	"github.com/chetto1983/aura/internal/db"
	"github.com/chetto1983/aura/internal/db/sqlc"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestEvidenceStoreSealsAdmissionRecoversCommitAndTransitionsPolicy(
	t *testing.T,
) {
	ctx, cancel := context.WithTimeout(t.Context(), 120*time.Second)
	t.Cleanup(cancel)
	pool, migrateURL := schema2MigrationDatabase(
		t, ctx, "aura_evidence_transition", shippedMigrationSteps(t),
	)
	ownerID := uuid.Must(uuid.NewV7())
	if _, err := pool.Exec(
		ctx,
		`INSERT INTO aura.identities (id, name, kind)
		 VALUES ($1, $2, 'user')`,
		ownerID, "evidence-transition-"+ownerID.String(),
	); err != nil {
		t.Fatalf("seed evidence owner: %v", err)
	}
	for _, capability := range []string{
		"adaptive.evidence.seal", "adaptive.manage",
	} {
		if _, err := pool.Exec(
			ctx,
			`INSERT INTO aura.capability_grants (identity_id, capability)
			 VALUES ($1, $2)`,
			ownerID, capability,
		); err != nil {
			t.Fatalf("grant %s: %v", capability, err)
		}
	}
	snapshot := focalCohortSnapshotForOwner(t, ownerID)
	if err := NewSnapshotStore(pool).Save(ctx, snapshot); err != nil {
		t.Fatalf("save snapshot: %v", err)
	}
	cohort, artifacts := admissionEvidenceFixture(t, ownerID, snapshot)
	if err := NewCohortStore(pool, NewStore(pool, StoreConfig{})).
		Save(ctx, cohort); err != nil {
		t.Fatalf("save cohort: %v", err)
	}
	sealer := EvidenceSealer{
		ActorID: ownerID, Capability: "adaptive.evidence.seal",
	}
	store := NewEvidenceStore(pool)
	registerAdmissionDependencies(
		t, ctx, store, ownerID, cohort, sealer, artifacts,
	)

	originalTransact := store.transact
	commitUnknown := errors.New("simulated unresolved commit")
	store.transact = func(
		txCtx context.Context,
		txPool *pgxpool.Pool,
		identity string,
		apply func(*sqlc.Queries) error,
	) error {
		if err := originalTransact(
			txCtx, txPool, identity, apply,
		); err != nil {
			return err
		}
		return commitUnknown
	}
	admission, err := store.SealCanaryAdmission(
		ctx, ownerID, cohort.ID(), sealer,
	)
	if err != nil {
		t.Fatalf("SealCanaryAdmission commit recovery: %v", err)
	}
	store.transact = originalTransact
	retried, err := store.SealCanaryAdmission(
		ctx, ownerID, cohort.ID(), sealer,
	)
	if err != nil {
		t.Fatalf("SealCanaryAdmission retry: %v", err)
	}
	admissionSHA256, _ := admission.SHA256()
	retriedSHA256, _ := retried.SHA256()
	if admission.EvidenceID != retried.EvidenceID ||
		admissionSHA256 != retriedSHA256 {
		t.Fatal("admission exact retry changed durable evidence")
	}

	migrationPool, err := db.Open(ctx, &db.Config{URL: migrateURL})
	if err != nil {
		t.Fatalf("open migration pool: %v", err)
	}
	t.Cleanup(migrationPool.Close)
	scope := cohort.Scope()
	if _, err := migrationPool.Exec(
		ctx,
		`UPDATE aura.adaptive_policy_state
		    SET epoch = $1, policy_version = $2, mode = 'shadow',
		        rollout_bps = 0
		  WHERE scope = 'global'`,
		int64(scope.PolicyEpoch), scope.PolicyVersion,
	); err != nil {
		t.Fatalf("seed shadow policy: %v", err)
	}
	policy, err := NewPolicyStore(pool).ApplyEvidenceTransition(
		ctx, ownerID, int64(scope.PolicyEpoch),
		uuid.MustParse(admission.EvidenceID), 2_500, "approved canary admission",
	)
	if err != nil {
		t.Fatalf("ApplyEvidenceTransition: %v", err)
	}
	if policy.Mode != PolicyCanary ||
		policy.Epoch != int64(scope.PolicyEpoch)+1 ||
		policy.RolloutBPS != 2_500 {
		t.Fatalf("transitioned policy = %#v", policy)
	}
}

func TestEvidenceStoreSealsClosedOutcomeFromStoredFacts(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 120*time.Second)
	t.Cleanup(cancel)
	pool, migrateURL := schema2MigrationDatabase(
		t, ctx, "aura_evidence_outcome", shippedMigrationSteps(t),
	)
	ownerID := uuid.Must(uuid.NewV7())
	if _, err := pool.Exec(
		ctx,
		`INSERT INTO aura.identities (id, name, kind)
		 VALUES ($1, $2, 'user')`,
		ownerID, "evidence-outcome-"+ownerID.String(),
	); err != nil {
		t.Fatalf("seed evidence owner: %v", err)
	}
	if _, err := pool.Exec(
		ctx,
		`INSERT INTO aura.capability_grants (identity_id, capability)
		 VALUES ($1, 'adaptive.evidence.seal'), ($1, 'adaptive.manage')`,
		ownerID,
	); err != nil {
		t.Fatalf("grant evidence capabilities: %v", err)
	}
	snapshot := focalCohortSnapshotForOwner(t, ownerID)
	if err := NewSnapshotStore(pool).Save(ctx, snapshot); err != nil {
		t.Fatalf("save snapshot: %v", err)
	}
	cohort, artifacts := admissionEvidenceFixture(t, ownerID, snapshot)
	ledger := NewStore(pool, StoreConfig{})
	cohortStore := NewCohortStore(pool, ledger)
	const memberCount = 40
	cohortStore.entropy = bytes.NewReader(
		bytes.Repeat([]byte{0, 1}, memberCount/2),
	)
	if err := cohortStore.Save(ctx, cohort); err != nil {
		t.Fatalf("save cohort: %v", err)
	}
	sealer := EvidenceSealer{
		ActorID: ownerID, Capability: "adaptive.evidence.seal",
	}
	evidenceStore := NewEvidenceStore(pool)
	registerAdmissionDependencies(
		t, ctx, evidenceStore, ownerID, cohort, sealer, artifacts,
	)
	admission, err := evidenceStore.SealCanaryAdmission(
		ctx, ownerID, cohort.ID(), sealer,
	)
	if err != nil {
		t.Fatalf("seal prerequisite admission: %v", err)
	}
	migrationPool, err := db.Open(ctx, &db.Config{URL: migrateURL})
	if err != nil {
		t.Fatalf("open migration pool: %v", err)
	}
	t.Cleanup(migrationPool.Close)
	scope := cohort.Scope()
	if _, err := migrationPool.Exec(
		ctx,
		`UPDATE aura.adaptive_policy_state
		    SET epoch = $1, policy_version = $2, mode = 'shadow',
		        rollout_bps = 0
		  WHERE scope = 'global'`,
		int64(scope.PolicyEpoch), scope.PolicyVersion,
	); err != nil {
		t.Fatalf("seed shadow policy: %v", err)
	}
	canary, err := NewPolicyStore(pool).ApplyEvidenceTransition(
		ctx, ownerID, int64(scope.PolicyEpoch),
		uuid.MustParse(admission.EvidenceID), 2_500, "admit live canary",
	)
	if err != nil {
		t.Fatalf("apply admission transition: %v", err)
	}
	if canary.Mode != PolicyCanary ||
		canary.Epoch != int64(scope.PolicyEpoch)+1 {
		t.Fatalf("canary policy = %#v", canary)
	}
	enrollments := make([]*FocalEnrollment, memberCount)
	for index := range enrollments {
		request := FocalEnrollmentRequest{
			OwnerID: ownerID, CohortID: cohort.ID(),
			RequestID: uuid.Must(uuid.NewV7()),
			EvaluationConversationID: evaluationConversation(
				t, ctx, pool, ownerID,
			),
			Domain: cohort.Domain(), Point: cohort.Predicate().Point,
			PointOrdinal: cohort.Predicate().Ordinal,
		}
		enrollment, err := cohortStore.Enroll(
			ctx, request, focalEnrollmentBuilder(t),
		)
		if err != nil {
			t.Fatalf("enroll member %d: %v", index, err)
		}
		enrollments[index] = enrollment
	}
	catalog, err := NewEvaluatorCatalog(cohort.Evaluators()...)
	if err != nil {
		t.Fatalf("build evaluator catalog: %v", err)
	}
	recorder, err := NewOutcomeRecorder(ledger, catalog)
	if err != nil {
		t.Fatalf("build outcome recorder: %v", err)
	}
	for index, enrollment := range enrollments {
		assignment := enrollment.Assignment
		if _, err := ledger.RecordDelivery(
			ctx, ownerID, assignment.AssignmentID,
			cohortLoaderSuccessDelivery(t, assignment),
		); err != nil {
			t.Fatalf("record delivery %d: %v", index, err)
		}
		quality := 0.0
		if assignment.ArmID == string(RandomizedArmChallenger) {
			quality = 1
		}
		for _, observation := range []OutcomeObservation{
			cohortLoaderObservedOutcome(
				assignment, cohort.PrimaryQualityEndpoint().Evaluator,
				quality, 0,
			),
			cohortLoaderObservedOutcome(
				assignment, cohort.PrimaryHarmEndpoint().Evaluator,
				0, 0,
			),
		} {
			if _, err := recorder.RecordOutcome(ctx, observation); err != nil {
				t.Fatalf("record outcome %d: %v", index, err)
			}
		}
	}
	lookPlan, err := DecodeEvidenceLookPlanArtifact(artifacts[1].Canonical)
	if err != nil {
		t.Fatalf("decode stored look plan: %v", err)
	}
	wait := time.Until(lookPlan.Looks[0].CutoffAt) + 100*time.Millisecond
	if wait > 0 {
		timer := time.NewTimer(wait)
		defer timer.Stop()
		select {
		case <-timer.C:
		case <-ctx.Done():
			t.Fatalf("wait for look cutoff: %v", ctx.Err())
		}
	}
	outcome, err := evidenceStore.SealCanaryOutcome(
		ctx, ownerID, cohort.ID(), 1, sealer,
	)
	if err != nil {
		t.Fatalf("SealCanaryOutcome: %v", err)
	}
	retried, err := evidenceStore.SealCanaryOutcome(
		ctx, ownerID, cohort.ID(), 1, sealer,
	)
	if err != nil {
		t.Fatalf("SealCanaryOutcome retry: %v", err)
	}
	if outcome.EvidenceID != retried.EvidenceID ||
		outcome.Body.LookNumber != 1 ||
		outcome.Body.Disposition != OutcomeDispositionPromote ||
		!outcome.Body.Eligible ||
		outcome.Body.QualityReportID == outcome.Body.HarmReportID ||
		outcome.Body.QualityOPEReportID == outcome.Body.HarmOPEReportID {
		t.Fatalf("sealed outcome bindings = %#v", outcome.Body)
	}
	active, err := NewPolicyStore(pool).ApplyEvidenceTransition(
		ctx, ownerID, canary.Epoch, uuid.MustParse(outcome.EvidenceID),
		10_000, "promote closed live canary",
	)
	if err != nil {
		t.Fatalf("apply outcome transition: %v", err)
	}
	if active.Mode != PolicyActive || active.Epoch != canary.Epoch+1 ||
		active.RolloutBPS != 10_000 {
		t.Fatalf("active policy = %#v", active)
	}
	var reports, outcomes int
	if err := migrationPool.QueryRow(
		ctx,
		`SELECT
		    (SELECT count(*) FROM aura.adaptive_evidence_artifacts
		      WHERE owner_id = $1 AND cohort_id = $2
		        AND kind IN (
		          'quality_report', 'harm_report', 'quality_ope_report',
		          'harm_ope_report', 'censor_diagnostics'
		        )),
		    (SELECT count(*) FROM aura.adaptive_sealed_evidence
		      WHERE owner_id = $1 AND cohort_id = $2
		        AND kind = 'canary_outcome')`,
		ownerID, cohort.ID(),
	).Scan(&reports, &outcomes); err != nil {
		t.Fatalf("count sealed outcome artifacts: %v", err)
	}
	if reports != 5 || outcomes != 1 {
		t.Fatalf("sealed reports/outcomes = %d/%d, want 5/1", reports, outcomes)
	}
}

func registerAdmissionDependencies(
	t *testing.T,
	ctx context.Context,
	store *EvidenceStore,
	ownerID uuid.UUID,
	cohort *FocalCohort,
	sealer EvidenceSealer,
	artifacts []EvidenceArtifactRegistration,
) {
	t.Helper()
	for _, artifact := range artifacts {
		if err := store.RegisterCohortArtifact(
			ctx, ownerID, cohort.ID(), sealer, artifact,
		); err != nil {
			t.Fatalf("register %s: %v", artifact.Ref.Kind, err)
		}
	}
	approval := OperatorApproval{
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
	canonical, err := approval.CanonicalJSON()
	if err != nil {
		t.Fatalf("canonicalize approval: %v", err)
	}
	if err := store.RegisterCohortArtifact(
		ctx, ownerID, cohort.ID(), sealer,
		EvidenceArtifactRegistration{
			Ref: ChildArtifactRef{
				SchemaID: ChildArtifactRefSchemaID, Revision: 1,
				Kind:             "operator_approval",
				ArtifactSchemaID: "aura.adaptive.operator-approval/v1",
				ArtifactRevision: 1, ArtifactID: approval.ApprovalID,
				ArtifactSHA256: sha256Hex(canonical),
			},
			Canonical: canonical,
		},
	); err != nil {
		t.Fatalf("register operator approval: %v", err)
	}
}

func admissionEvidenceFixture(
	t *testing.T,
	ownerID uuid.UUID,
	snapshot *PolicySnapshot,
) (*FocalCohort, []EvidenceArtifactRegistration) {
	t.Helper()
	now := time.Now().UTC().Truncate(time.Microsecond)
	cutoffs := make([]time.Time, 5)
	for index := range cutoffs {
		cutoffs[index] = now.Add(time.Duration(index+5) * time.Second)
	}
	lookPlan, err := NewEvidenceLookPlanArtifact(cutoffs)
	if err != nil {
		t.Fatalf("build look plan: %v", err)
	}
	lookCanonical, err := lookPlan.CanonicalJSON()
	if err != nil {
		t.Fatalf("canonicalize look plan: %v", err)
	}
	cluster, err := NewInterferenceCluster(ownerID.String(), ownerID.String())
	if err != nil {
		t.Fatalf("derive interference schema: %v", err)
	}
	interference := validInterferencePlanArtifact()
	interference.ClusterSchemaSHA256 = cluster.SchemaSHA256
	interferenceCanonical, err := interference.CanonicalJSON()
	if err != nil {
		t.Fatalf("canonicalize interference plan: %v", err)
	}
	base := randomizedFocalCohortForSnapshot(
		t, snapshot, cutoffs[4],
		[]BlockKey{BlockOwner, BlockTimeBlock},
	)
	actions := cohortActionIDs(base.Actions())
	actionHash, err := (ActionSchemaArtifact{
		SchemaID: "aura.adaptive.action-schema/v1", Revision: 1,
		Actions: actions,
	}).SHA256()
	if err != nil {
		t.Fatalf("hash action schema: %v", err)
	}
	targetHash, err := (TargetPolicyManifest{
		SchemaID: "aura.adaptive.policy-manifest/target/v1", Revision: 1,
		PolicyID: "aura/deterministic-challenger", PolicyRevision: 1,
		ActionSchemaSHA256: actionHash, Rule: "snapshot_recommended_action",
	}).SHA256()
	if err != nil {
		t.Fatalf("hash target policy: %v", err)
	}
	behaviorHash, err := (BehaviorPolicyManifest{
		SchemaID: "aura.adaptive.policy-manifest/behavior/v1", Revision: 1,
		PolicyID: "aura/equal-bernoulli-arm-mixture", PolicyRevision: 1,
		ActionSchemaSHA256: actionHash,
		RandomizationPlanSHA256: base.v2.document.
			RandomizationPlanRef.ArtifactSHA256,
		Rule: "marginalize_deterministic_arm_actions",
	}).SHA256()
	if err != nil {
		t.Fatalf("hash behavior policy: %v", err)
	}
	predictions := func(success uint64) []ActionMeanPrediction {
		values := make([]ActionMeanPrediction, len(actions))
		for index, actionID := range actions {
			values[index] = ActionMeanPrediction{
				ActionID: actionID, SupportCount: 2, SuccessCount: success,
				QHat: MustExactRational(int64(success), 2),
			}
		}
		return values
	}
	qualityModel := opeReportModelRegistration(
		t, base, EvidenceEndpointQuality, actionHash, targetHash,
		predictions(1),
	)
	harmModel := opeReportModelRegistration(
		t, base, EvidenceEndpointHarm, actionHash, targetHash,
		predictions(0),
	)
	plan := opeReportTestPlan{
		SchemaID: "aura.adaptive.ope-plan/v1", Revision: 1,
		Estimand: "selected_intention", SupportedActions: actions,
		TargetPolicyID:       "aura/deterministic-challenger",
		TargetPolicyRevision: 1, TargetPolicySHA256: targetHash,
		BehaviorPolicyID:       "aura/equal-bernoulli-arm-mixture",
		BehaviorPolicyRevision: 1, BehaviorPolicySHA256: behaviorHash,
		EndpointModels: []opeReportTestModelRef{
			opeReportModelRef(t, qualityModel),
			opeReportModelRef(t, harmModel),
		},
		WeightClipping: "none", MinimumESS: MustExactRational(1, 1),
		MinimumOverlap: MustExactRational(1, 2),
		Uncertainty: opeReportTestUncertainty{
			AlgorithmID: "cluster-percentile-bootstrap-sha256-v1",
			Revision:    1, Confidence: MustExactRational(19, 20),
			Replicates: 10_000, Unit: "interference_cluster",
			IntervalTarget:   "dr",
			SeedDomain:       "aura/ope-cluster-bootstrap-sha256-v1",
			RoundingRevision: "neumaier-binary64-no-fma-outward-8dp-v1",
		},
	}
	opePlan := opeReportRegistration(
		t, "ope_plan", "aura.adaptive.ope-plan/v1", plan, 0x73,
	)
	refs := validCohortV2ChildRefs()
	refs.InterferencePlan.ArtifactSHA256 = sha256Hex(interferenceCanonical)
	refs.LookPlan.ArtifactSHA256 = sha256Hex(lookCanonical)
	refs.QualityOutcomeModel = qualityModel.Ref
	refs.HarmOutcomeModel = harmModel.Ref
	refs.OPEPlan = opePlan.Ref
	artifacts := []EvidenceArtifactRegistration{
		{Ref: refs.InterferencePlan, Canonical: interferenceCanonical},
		{Ref: refs.LookPlan, Canonical: lookCanonical},
		qualityModel, harmModel, opePlan,
	}
	spec := base.spec
	spec.Power.ExpectedMembers = 40
	spec.Power.MinimumMembers = 40
	spec.Power.MinimumArmSupport = 0.5
	spec.Margins.MaximumHarmIncrease = 0.75
	cohort, err := NewRandomizedFocalCohort(spec, snapshot, refs)
	if err != nil {
		t.Fatalf("build admission cohort: %v", err)
	}
	return cohort, artifacts
}
