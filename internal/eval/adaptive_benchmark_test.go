package eval

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/chetto1983/aura/internal/adaptive"
	"github.com/chetto1983/aura/internal/idempotency"
	"github.com/chetto1983/aura/internal/identityctx"
	"github.com/google/uuid"
)

func TestAdaptiveBenchmarkVerifyCohortReturnsOnlyVerifiedLinkage(t *testing.T) {
	t.Parallel()
	cohort := benchmarkCohort(t)
	assignmentID := uuid.Must(uuid.NewV7())
	claimID := uuid.Must(uuid.NewV7())
	requestID := uuid.Must(uuid.NewV7())
	deliveryID := uuid.Must(uuid.NewV7())
	qualityID := uuid.Must(uuid.NewV7())
	harmID := uuid.Must(uuid.NewV7())
	cohorts := &benchmarkCohortStoreFake{ledger: adaptive.CohortLedger{
		CohortID: cohort.ID(), CohortSHA256: cohort.SHA256(),
		State: adaptive.CohortLedgerClosed, SHA256: strings.Repeat("9", 64),
		Sealable: true,
		Members: []adaptive.CohortMember{{
			Claim: adaptive.FocalClaim{
				ID: claimID, RequestID: requestID, AssignmentID: assignmentID,
			},
			Assignment: adaptive.Assignment{
				AssignmentID: assignmentID, ArmID: "baseline",
				IntendedActionID: "static", ProviderID: "raw-secret-marker",
			},
			Delivery: &adaptive.CohortDeliveryProjection{EventID: deliveryID},
			Quality:  &adaptive.CohortEffectiveObservation{EventID: qualityID},
			Harm:     &adaptive.CohortEffectiveObservation{EventID: harmID},
		}},
		IncludedFacts: []adaptive.IncludedFactRef{
			{EventID: deliveryID}, {EventID: qualityID}, {EventID: harmID},
		},
	}}
	workflow := newAdaptiveBenchmark(cohorts, nil)

	report, err := workflow.VerifyCohort(t.Context(), cohort.Scope().OwnerID, cohort.ID())
	if err != nil {
		t.Fatalf("VerifyCohort: %v", err)
	}
	if report.ClosedLedgerSHA256 != strings.Repeat("9", 64) ||
		report.MemberCount != 1 || report.IncludedFactCount != 3 ||
		len(report.Members) != 1 {
		t.Fatalf("verification report = %#v", report)
	}
	linkage := report.Members[0]
	if linkage.ClaimID != claimID || linkage.RequestID != requestID ||
		linkage.AssignmentID != assignmentID || linkage.DeliveryEventID != deliveryID ||
		linkage.QualityEventID != qualityID || linkage.HarmEventID != harmID {
		t.Fatalf("member linkage = %#v", linkage)
	}
	encoded, err := json.Marshal(report)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(encoded), "raw-secret-marker") {
		t.Fatalf("verify output exposed raw assignment content: %s", encoded)
	}
}

func TestAdaptiveBenchmarkVerifyCohortRejectsOpenLedger(t *testing.T) {
	t.Parallel()
	cohort := benchmarkCohort(t)
	workflow := newAdaptiveBenchmark(
		&benchmarkCohortStoreFake{ledger: adaptive.CohortLedger{
			CohortID: cohort.ID(), CohortSHA256: cohort.SHA256(),
			State: adaptive.CohortLedgerOpen,
		}},
		nil,
	)

	_, err := workflow.VerifyCohort(t.Context(), cohort.Scope().OwnerID, cohort.ID())
	if !errors.Is(err, ErrAdaptiveBenchmarkCohortOpen) {
		t.Fatalf("VerifyCohort error = %v, want ErrAdaptiveBenchmarkCohortOpen", err)
	}
}

func TestAdaptiveBenchmarkSealAdmissionFailsBeforeWriteWithoutCompleteInput(
	t *testing.T,
) {
	t.Parallel()
	cohort := benchmarkCohort(t)
	valid := benchmarkAdmissionRequest(t, cohort)
	tests := []struct {
		name      string
		artifacts []adaptive.EvidenceArtifactRegistration
	}{
		{
			name:      "artifact missing",
			artifacts: valid.Artifacts[:len(valid.Artifacts)-1],
		},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			evidence := &benchmarkEvidenceStoreFake{}
			workflow := newAdaptiveBenchmark(nil, evidence)
			request := valid
			request.Artifacts = testCase.artifacts
			_, err := workflow.SealAdmission(
				t.Context(),
				request,
			)
			if !errors.Is(err, ErrAdaptiveBenchmarkAdmissionInput) {
				t.Fatalf(
					"SealAdmission error = %v, want ErrAdaptiveBenchmarkAdmissionInput",
					err,
				)
			}
			if len(evidence.sealedKinds) != 0 ||
				evidence.atomicSealCalls != 0 {
				t.Fatalf(
					"incomplete admission wrote kinds=%d calls=%d",
					len(evidence.sealedKinds),
					evidence.atomicSealCalls,
				)
			}
		})
	}
}

func TestAdaptiveBenchmarkSealAdmissionOrdersChildrenForAtomicSeal(
	t *testing.T,
) {
	t.Parallel()
	cohort := benchmarkCohort(t)
	evidence := &benchmarkEvidenceStoreFake{}
	workflow := newAdaptiveBenchmark(
		&benchmarkCohortStoreFake{},
		evidence,
	)

	request := benchmarkAdmissionRequest(t, cohort)
	ctx := benchmarkAdmissionOperationContext(t, request)
	_, err := workflow.SealAdmission(ctx, request)
	if err != nil {
		t.Fatalf("SealAdmission: %v", err)
	}
	wantOrder := []string{
		"interference_plan", "look_plan", "quality_outcome_model",
		"harm_outcome_model", "ope_plan",
	}
	if strings.Join(evidence.sealedKinds, ",") != strings.Join(wantOrder, ",") {
		t.Fatalf("atomic seal order = %v, want %v", evidence.sealedKinds, wantOrder)
	}
	if evidence.sealer.ActorID != cohort.Scope().OwnerID ||
		evidence.sealer.Capability != "adaptive.evidence.seal" {
		t.Fatalf("server-built sealer = %#v", evidence.sealer)
	}
	if evidence.atomicSealCalls != 1 {
		t.Fatalf("atomic seal calls = %d, want 1", evidence.atomicSealCalls)
	}
}

func TestAdaptiveBenchmarkAdmissionFingerprintBindsSemanticIntent(t *testing.T) {
	t.Parallel()
	cohort := benchmarkCohort(t)
	base := benchmarkAdmissionRequest(t, cohort)
	first, err := AdaptiveBenchmarkAdmissionFingerprint(base)
	if err != nil {
		t.Fatalf("AdaptiveBenchmarkAdmissionFingerprint: %v", err)
	}
	retry, err := AdaptiveBenchmarkAdmissionFingerprint(base)
	if err != nil || retry != first {
		t.Fatalf("exact retry fingerprint = %x, %v; want %x", retry, err, first)
	}

	tests := []struct {
		name   string
		mutate func(*AdaptiveBenchmarkAdmissionRequest)
	}{
		{name: "manifest bytes", mutate: func(request *AdaptiveBenchmarkAdmissionRequest) {
			request.ManifestSHA256 = strings.Repeat("b", 64)
		}},
		{name: "owner", mutate: func(request *AdaptiveBenchmarkAdmissionRequest) {
			request.OwnerID = uuid.Must(uuid.NewV7())
		}},
		{name: "cohort", mutate: func(request *AdaptiveBenchmarkAdmissionRequest) {
			request.CohortID = uuid.Must(uuid.NewV7())
		}},
		{name: "reference", mutate: func(request *AdaptiveBenchmarkAdmissionRequest) {
			request.Artifacts[0].Ref.ArtifactID = uuid.Must(uuid.NewV7()).String()
		}},
		{name: "child digest", mutate: func(request *AdaptiveBenchmarkAdmissionRequest) {
			request.Artifacts[0].Canonical = bytes.Replace(
				request.Artifacts[0].Canonical,
				[]byte(strings.Repeat("a", 64)),
				[]byte(strings.Repeat("9", 64)),
				1,
			)
			request.Artifacts[0].Ref.ArtifactSHA256 = benchmarkSHA256(
				request.Artifacts[0].Canonical,
			)
		}},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			changed := cloneBenchmarkAdmissionRequest(base)
			testCase.mutate(&changed)
			fingerprint, err := AdaptiveBenchmarkAdmissionFingerprint(changed)
			if err != nil {
				t.Fatalf("changed fingerprint: %v", err)
			}
			if fingerprint == first {
				t.Fatal("changed semantic intent retained admission fingerprint")
			}
		})
	}
}

func TestAdaptiveBenchmarkAdmissionFingerprintDelegatesToAdaptiveContract(
	t *testing.T,
) {
	t.Parallel()
	request := benchmarkAdmissionRequest(t, benchmarkCohort(t))
	got, err := AdaptiveBenchmarkAdmissionFingerprint(request)
	if err != nil {
		t.Fatalf("AdaptiveBenchmarkAdmissionFingerprint: %v", err)
	}
	want, err := adaptive.AdmissionOperationFingerprint(
		request.ManifestSHA256,
		request.OwnerID,
		request.CohortID,
		request.Artifacts,
	)
	if err != nil {
		t.Fatalf("adaptive.AdmissionOperationFingerprint: %v", err)
	}
	if got != want {
		t.Fatalf("eval fingerprint = %x, want adaptive contract %x", got, want)
	}
}

func TestAdaptiveBenchmarkSealAdmissionRequiresExactOperationFingerprint(
	t *testing.T,
) {
	t.Parallel()
	cohort := benchmarkCohort(t)
	base := benchmarkAdmissionRequest(t, cohort)
	tests := []struct {
		name      string
		request   func() AdaptiveBenchmarkAdmissionRequest
		context   func(AdaptiveBenchmarkAdmissionRequest) context.Context
		wantCalls int
	}{
		{
			name: "exact fingerprint",
			request: func() AdaptiveBenchmarkAdmissionRequest {
				return cloneBenchmarkAdmissionRequest(base)
			},
			context: func(request AdaptiveBenchmarkAdmissionRequest) context.Context {
				return benchmarkAdmissionOperationContext(t, request)
			},
			wantCalls: 1,
		},
		{
			name: "missing fingerprint",
			request: func() AdaptiveBenchmarkAdmissionRequest {
				return cloneBenchmarkAdmissionRequest(base)
			},
			context: func(AdaptiveBenchmarkAdmissionRequest) context.Context {
				return t.Context()
			},
		},
		{
			name: "wrong fingerprint",
			request: func() AdaptiveBenchmarkAdmissionRequest {
				return cloneBenchmarkAdmissionRequest(base)
			},
			context: func(request AdaptiveBenchmarkAdmissionRequest) context.Context {
				ctx := benchmarkAdmissionOperationContext(t, request)
				operation, _ := idempotency.OperationFromContext(ctx)
				operation.Fingerprint[0] ^= 0xff
				return attachBenchmarkAdmissionOperation(t, operation)
			},
		},
		{
			name: "wrong command",
			request: func() AdaptiveBenchmarkAdmissionRequest {
				request := cloneBenchmarkAdmissionRequest(base)
				request.Command = "aura eval adaptive verify"
				return request
			},
			context: func(AdaptiveBenchmarkAdmissionRequest) context.Context {
				return benchmarkAdmissionOperationContext(t, base)
			},
		},
		{
			name: "missing confirmation",
			request: func() AdaptiveBenchmarkAdmissionRequest {
				request := cloneBenchmarkAdmissionRequest(base)
				request.Confirmed = false
				return request
			},
			context: func(AdaptiveBenchmarkAdmissionRequest) context.Context {
				return benchmarkAdmissionOperationContext(t, base)
			},
		},
		{
			name: "manifest changed after preparation",
			request: func() AdaptiveBenchmarkAdmissionRequest {
				request := cloneBenchmarkAdmissionRequest(base)
				request.ManifestSHA256 = strings.Repeat("b", 64)
				return request
			},
			context: func(AdaptiveBenchmarkAdmissionRequest) context.Context {
				return benchmarkAdmissionOperationContext(t, base)
			},
		},
		{
			name: "child changed after preparation",
			request: func() AdaptiveBenchmarkAdmissionRequest {
				request := cloneBenchmarkAdmissionRequest(base)
				request.Artifacts[0].Canonical = []byte(`{"changed":true}`)
				request.Artifacts[0].Ref.ArtifactSHA256 = benchmarkSHA256(
					request.Artifacts[0].Canonical,
				)
				return request
			},
			context: func(AdaptiveBenchmarkAdmissionRequest) context.Context {
				return benchmarkAdmissionOperationContext(t, base)
			},
		},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			request := testCase.request()
			evidence := &benchmarkEvidenceStoreFake{}
			workflow := newAdaptiveBenchmark(nil, evidence)
			_, err := workflow.SealAdmission(
				testCase.context(request),
				request,
			)
			if testCase.wantCalls == 0 &&
				!errors.Is(err, ErrAdaptiveBenchmarkAdmissionInput) {
				t.Fatalf("SealAdmission error = %v, want admission input rejection", err)
			}
			if testCase.wantCalls == 1 && err != nil {
				t.Fatalf("SealAdmission exact fingerprint: %v", err)
			}
			if evidence.atomicSealCalls != testCase.wantCalls {
				t.Fatalf(
					"evidence calls = %d, want %d",
					evidence.atomicSealCalls,
					testCase.wantCalls,
				)
			}
		})
	}
}

func benchmarkAdmissionRequest(
	t *testing.T,
	cohort *adaptive.FocalCohort,
) AdaptiveBenchmarkAdmissionRequest {
	t.Helper()
	return AdaptiveBenchmarkAdmissionRequest{
		Command:        AdaptiveBenchmarkAdmissionCommand,
		Confirmed:      true,
		ManifestSHA256: strings.Repeat("a", 64),
		OwnerID:        cohort.Scope().OwnerID, CohortID: cohort.ID(),
		Artifacts: benchmarkAdmissionArtifacts(t),
	}
}

func cloneBenchmarkAdmissionRequest(
	request AdaptiveBenchmarkAdmissionRequest,
) AdaptiveBenchmarkAdmissionRequest {
	cloned := request
	cloned.Artifacts = make(
		[]adaptive.EvidenceArtifactRegistration,
		len(request.Artifacts),
	)
	for index, registration := range request.Artifacts {
		cloned.Artifacts[index] = registration
		cloned.Artifacts[index].Canonical = append(
			[]byte(nil),
			registration.Canonical...,
		)
	}
	return cloned
}

func benchmarkAdmissionOperationContext(
	t *testing.T,
	request AdaptiveBenchmarkAdmissionRequest,
) context.Context {
	t.Helper()
	fingerprint, err := AdaptiveBenchmarkAdmissionFingerprint(request)
	if err != nil {
		t.Fatalf("fingerprint benchmark admission: %v", err)
	}
	return attachBenchmarkAdmissionOperation(t, idempotency.Operation{
		Key: idempotency.OperationKey{
			IdentityID: identityctx.CLIServiceIdentity,
			Scope:      idempotency.ScopeCLICommand,
			Key:        "adaptive-seal-operation",
		},
		Fingerprint: fingerprint,
	})
}

func attachBenchmarkAdmissionOperation(
	t *testing.T,
	operation idempotency.Operation,
) context.Context {
	t.Helper()
	trusted := identityctx.WithIdentityID(
		t.Context(),
		identityctx.CLIServiceIdentity,
	)
	ctx, err := idempotency.WithOperation(trusted, operation)
	if err != nil {
		t.Fatalf("attach benchmark admission operation: %v", err)
	}
	return ctx
}

type benchmarkCohortStoreFake struct {
	ledger         adaptive.CohortLedger
	reconstructErr error
}

func (fake *benchmarkCohortStoreFake) Reconstruct(
	context.Context,
	uuid.UUID,
	uuid.UUID,
) (adaptive.CohortLedger, error) {
	return fake.ledger, fake.reconstructErr
}

type benchmarkEvidenceStoreFake struct {
	sealedKinds     []string
	atomicSealCalls int
	sealErr         error
	sealer          adaptive.EvidenceSealer
}

func (fake *benchmarkEvidenceStoreFake) SealCanaryAdmission(
	_ context.Context,
	_ uuid.UUID,
	_ uuid.UUID,
	sealer adaptive.EvidenceSealer,
	_ string,
	registrations []adaptive.EvidenceArtifactRegistration,
) (adaptive.CanaryAdmissionEvidence, error) {
	fake.atomicSealCalls++
	fake.sealer = sealer
	for _, registration := range registrations {
		fake.sealedKinds = append(fake.sealedKinds, registration.Ref.Kind)
	}
	return adaptive.CanaryAdmissionEvidence{}, fake.sealErr
}

func benchmarkCohort(t *testing.T) *adaptive.FocalCohort {
	t.Helper()
	quality := adaptive.EvaluatorRegistration{
		Kind: adaptive.EvaluatorDeterministic, ID: "citation-check",
		Version: "v3", RubricID: "citation-support",
		CalibrationID:    "heldout-2026-07",
		ProvenanceSHA256: strings.Repeat("a", 64),
	}
	harm := adaptive.EvaluatorRegistration{
		Kind: adaptive.EvaluatorCalibratedJudge, ID: "harm-rubric",
		Version: "v2", RubricID: "harm-binary",
		CalibrationID:    "heldout-harm-2026-07",
		ProvenanceSHA256: strings.Repeat("b", 64),
	}
	human := adaptive.EvaluatorRegistration{
		Kind: adaptive.EvaluatorHuman, ID: "human-review",
		Version: "v1", RubricID: "human-binary",
		CalibrationID:    "human-2026-07",
		ProvenanceSHA256: strings.Repeat("c", 64),
	}
	cohort, err := adaptive.NewFocalCohort(adaptive.CohortSpec{
		Scope: adaptive.CohortScope{
			OwnerID:    uuid.MustParse("6f1a63d6-8a4e-4c0c-9a3e-2b7d5f0c1e88"),
			ProviderID: "openrouter", ModelID: "configured-production-model",
			PolicyVersion: "policy-2026-07-24", PolicyEpoch: 7,
			SnapshotID:     uuid.MustParse("2c9b4e11-5d3a-4f77-8e21-90a6c4b3d5f2"),
			SnapshotSHA256: strings.Repeat("d", 64),
			Environment:    adaptive.EvaluationProductionCanary,
		},
		Predicate: adaptive.FocalPredicate{
			Domain: adaptive.DomainKnowledge, Point: adaptive.PointKnowledge, Ordinal: 1,
		},
		ExperimentID: "knowledge-depth-2026-07",
		Arms: []adaptive.CohortArm{
			{ArmID: "baseline", Probability: 0.5},
			{ArmID: "challenger", Probability: 0.5},
		},
		Actions: []adaptive.ActionProbability{
			{ActionID: "graph_expansion", Probability: 0.25},
			{ActionID: "static", Probability: 0.5},
			{ActionID: "vector_rerank", Probability: 0.25},
		},
		Evaluators: []adaptive.EvaluatorRegistration{harm, quality, human},
		PrimaryQuality: adaptive.CohortEndpoint{
			ID: "citation-support", Kind: adaptive.CohortBinaryQuality,
			Evaluator: quality.Provenance(),
		},
		PrimaryHarm: adaptive.CohortEndpoint{
			ID: "harm-binary", Kind: adaptive.CohortBinaryHarm,
			Evaluator: harm.Provenance(),
		},
		Power: adaptive.CohortPowerPlan{
			BaselineQualityRate: 0.65, BaselineHarmRate: 0.01,
			MinimumDetectableLift: 0.08, ExpectedMembers: 100,
			MinimumMembers: 50, MinimumArmSupport: 0.05,
			SimulationSHA256: strings.Repeat("f", 64),
		},
		Margins: adaptive.CohortMargins{
			MinimumQualityUplift: 0.02, MaximumHarmIncrease: 0.02,
		},
		Censoring: adaptive.CohortCensoring{
			MaxCensorRate: 0.1, MaxCensorImbalance: 0.05,
		},
		Admission: adaptive.CohortAdmission{
			BlockKeys: []adaptive.BlockKey{
				adaptive.BlockEpisode, adaptive.BlockOwner,
				adaptive.BlockSession, adaptive.BlockTimeBlock,
			},
			TimeBlockSeconds: 3600,
			Interference:     adaptive.InterferenceNoneBetweenUnits,
		},
		Looks: []adaptive.CohortLook{
			{Look: 1, Alpha: 0.005}, {Look: 2, Alpha: 0.005},
			{Look: 3, Alpha: 0.01}, {Look: 4, Alpha: 0.01},
			{Look: 5, Alpha: 0.02},
		},
		Cutoff: time.Date(2026, time.July, 24, 12, 0, 0, 123_456_000, time.UTC),
	})
	if err != nil {
		t.Fatalf("NewFocalCohort: %v", err)
	}
	return cohort
}
