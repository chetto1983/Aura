package adaptive

import (
	"bytes"
	"encoding/json"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestCanaryAdmissionEvidence_CanonicalAndOutcomeFree(t *testing.T) {
	t.Parallel()

	evidence := validCanaryAdmissionEvidence()
	canonical, err := evidence.CanonicalJSON()
	if err != nil {
		t.Fatalf("CanonicalJSON: %v", err)
	}
	if bytes.Contains(canonical, []byte("closed_ledger")) ||
		bytes.Contains(canonical, []byte("quality_report")) ||
		bytes.Contains(canonical, []byte("disposition")) {
		t.Fatalf("admission evidence contains outcome data: %s", canonical)
	}
	if !bytes.Equal(canonical, mustJSON(t, evidence)) {
		t.Fatalf("admission evidence is not canonical:\n%s", canonical)
	}
	digest, err := evidence.SHA256()
	if err != nil {
		t.Fatalf("SHA256: %v", err)
	}
	if !validSHA256(digest) {
		t.Fatalf("invalid evidence digest %q", digest)
	}
}

func TestCanaryOutcomeEvidence_RejectsKindAndEndpointSubstitution(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		mutate func(*CanaryOutcomeEvidence)
	}{
		{
			name: "admission kind",
			mutate: func(evidence *CanaryOutcomeEvidence) {
				evidence.Kind = EvidenceCanaryAdmission
			},
		},
		{
			name: "same quality and harm report",
			mutate: func(evidence *CanaryOutcomeEvidence) {
				evidence.Body.HarmReportID = evidence.Body.QualityReportID
				evidence.Body.HarmReportSHA256 = evidence.Body.QualityReportSHA256
			},
		},
		{
			name: "same quality and harm OPE report",
			mutate: func(evidence *CanaryOutcomeEvidence) {
				evidence.Body.HarmOPEReportID = evidence.Body.QualityOPEReportID
				evidence.Body.HarmOPEReportSHA256 = evidence.Body.QualityOPEReportSHA256
			},
		},
		{
			name: "look one predecessor",
			mutate: func(evidence *CanaryOutcomeEvidence) {
				predecessor := uuid.MustParse("99999999-9999-4999-8999-999999999999").String()
				digest := testEvidenceHash("9")
				evidence.Body.PredecessorEvidenceID = &predecessor
				evidence.Body.PredecessorEvidenceSHA256 = &digest
			},
		},
		{
			name: "continue at look five",
			mutate: func(evidence *CanaryOutcomeEvidence) {
				evidence.Body.LookNumber = 5
				evidence.Body.Disposition = OutcomeDispositionContinue
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			candidate := validCanaryOutcomeEvidence()
			test.mutate(&candidate)
			if _, err := candidate.CanonicalJSON(); err == nil {
				t.Fatal("CanonicalJSON accepted substituted evidence")
			}
		})
	}
}

func TestTransitionBinding_EnforcesEvidenceKindTargetAndLook(t *testing.T) {
	t.Parallel()

	admission := validTransitionBinding(EvidenceCanaryAdmission)
	if _, err := admission.CanonicalJSON(); err != nil {
		t.Fatalf("admission binding: %v", err)
	}
	outcome := validTransitionBinding(EvidenceCanaryOutcome)
	if _, err := outcome.CanonicalJSON(); err != nil {
		t.Fatalf("outcome binding: %v", err)
	}

	tests := []struct {
		name   string
		mutate func(*TransitionBinding)
	}{
		{
			name: "admission cannot activate",
			mutate: func(binding *TransitionBinding) {
				binding.TargetState = string(PolicyActive)
			},
		},
		{
			name: "outcome cannot enter canary",
			mutate: func(binding *TransitionBinding) {
				binding.TargetState = string(PolicyCanary)
			},
		},
		{
			name: "admission cannot bind a look",
			mutate: func(binding *TransitionBinding) {
				look := 1
				binding.LookNumber = &look
			},
		},
		{
			name: "outcome requires look cutoff",
			mutate: func(binding *TransitionBinding) {
				binding.LookCutoff = nil
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			candidate := admission
			if test.name == "outcome cannot enter canary" ||
				test.name == "outcome requires look cutoff" {
				candidate = outcome
			}
			test.mutate(&candidate)
			if _, err := candidate.CanonicalJSON(); err == nil {
				t.Fatal("CanonicalJSON accepted invalid transition binding")
			}
		})
	}
}

func TestEvidenceLookPlanArtifact_RequiresFiveFrozenUTCClosures(t *testing.T) {
	t.Parallel()

	cutoffs := make([]time.Time, 5)
	for index := range cutoffs {
		cutoffs[index] = time.Date(2026, 7, 26, 10+index, 0, 0, 0, time.UTC)
	}
	artifact, err := NewEvidenceLookPlanArtifact(cutoffs)
	if err != nil {
		t.Fatalf("NewEvidenceLookPlanArtifact: %v", err)
	}
	canonical, err := artifact.CanonicalJSON()
	if err != nil {
		t.Fatalf("CanonicalJSON: %v", err)
	}
	decoded, err := DecodeEvidenceLookPlanArtifact(canonical)
	if err != nil {
		t.Fatalf("DecodeEvidenceLookPlanArtifact: %v", err)
	}
	if decoded.Looks[4].CutoffAt != cutoffs[4] ||
		decoded.Looks[4].CumulativeAlpha.Cmp(MustExactRational(1, 20)) != 0 {
		t.Fatalf("decoded look five = %#v", decoded.Looks[4])
	}
	candidate := artifact
	candidate.Looks = append([]EvidenceLookArtifact(nil), artifact.Looks...)
	candidate.Looks[1].CutoffAt = candidate.Looks[0].CutoffAt
	if _, err := candidate.CanonicalJSON(); err == nil {
		t.Fatal("look plan accepted a repeated cutoff")
	}
}

func TestOperatorApprovalBindsAdmissionOnly(t *testing.T) {
	t.Parallel()

	approval := OperatorApproval{
		SchemaID: "aura.adaptive.operator-approval/v1", Revision: 1,
		ApprovalID: uuid.MustParse("aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa").String(),
		ActorID:    uuid.MustParse("11111111-1111-4111-8111-111111111111").String(),
		Capability: "adaptive.evidence.seal", TargetState: string(PolicyCanary),
		OwnerID:      uuid.MustParse("11111111-1111-4111-8111-111111111111").String(),
		CohortID:     uuid.MustParse("22222222-2222-4222-8222-222222222222").String(),
		CohortSHA256: testEvidenceHash("a"), PolicyEpoch: 2, PolicyVersion: "policy-v2",
		ApprovedAt:       time.Date(2026, 7, 26, 7, 0, 0, 0, time.UTC),
		SourceRequestID:  uuid.MustParse("bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb").String(),
		ProvenanceSHA256: testEvidenceHash("b"),
	}
	if _, err := approval.CanonicalJSON(); err != nil {
		t.Fatalf("CanonicalJSON: %v", err)
	}
	approval.TargetState = string(PolicyActive)
	if _, err := approval.CanonicalJSON(); err == nil {
		t.Fatal("operator admission approval accepted active target")
	}
}

func validCanaryAdmissionEvidence() CanaryAdmissionEvidence {
	ownerID := uuid.MustParse("11111111-1111-4111-8111-111111111111")
	cohortID := uuid.MustParse("22222222-2222-4222-8222-222222222222")
	snapshotID := uuid.MustParse("33333333-3333-4333-8333-333333333333")
	body := CanaryAdmissionBody{
		SchemaID: "aura.adaptive.evidence/canary-admission-body/v1", Revision: 1,
		TargetState:                string(PolicyCanary),
		RandomizationPlanRef:       evidenceChildRef("randomization_plan", "aura.adaptive.randomization-plan/v1", "1"),
		InterferencePlanRef:        evidenceChildRef("interference_plan", "aura.adaptive.interference-plan/v1", "2"),
		LookPlanRef:                evidenceChildRef("look_plan", "aura.adaptive.look-plan/utc-cutoff/v1", "3"),
		SnapshotRef:                evidenceChildRef("snapshot", SnapshotSchemaVersion, "4"),
		EvaluatorCalibrationSHA256: testEvidenceHash("5"),
		QualityOutcomeModelRef:     evidenceChildRef("quality_outcome_model", "aura.adaptive.outcome-model/action-mean/v1", "6"),
		HarmOutcomeModelRef:        evidenceChildRef("harm_outcome_model", "aura.adaptive.outcome-model/action-mean/v1", "7"),
		OPEPlanRef:                 evidenceChildRef("ope_plan", "aura.adaptive.ope-plan/v1", "8"),
		PowerSimulationSHA256:      testEvidenceHash("9"),
		OperatorApprovalRef:        evidenceChildRef("operator_approval", "aura.adaptive.operator-approval/v1", "a"),
	}
	bodySHA256, _ := body.SHA256()
	return CanaryAdmissionEvidence{
		SchemaID: "aura.adaptive.evidence/canary-admission/v1", Revision: 1,
		EvidenceID: uuid.MustParse("44444444-4444-4444-8444-444444444444").String(),
		Kind:       EvidenceCanaryAdmission, OwnerID: ownerID.String(), CohortID: cohortID.String(),
		CohortSHA256: testEvidenceHash("b"), Domain: DomainReasoning,
		ProviderID: "provider", ModelID: "model", PolicyEpoch: 2, PolicyVersion: "policy-v2",
		SnapshotID: snapshotID.String(), SnapshotSHA256: testEvidenceHash("c"),
		CreatedAt:     time.Date(2026, 7, 26, 8, 0, 0, 123000, time.UTC),
		SealerActorID: ownerID.String(), SealerCapability: "adaptive.evidence.seal",
		BodySHA256: bodySHA256, Body: body,
	}
}

func validCanaryOutcomeEvidence() CanaryOutcomeEvidence {
	admission := validCanaryAdmissionEvidence()
	body := CanaryOutcomeBody{
		SchemaID: "aura.adaptive.evidence/canary-outcome-body/v1", Revision: 1,
		TargetState: string(PolicyActive), LookNumber: 1,
		LookCutoff: time.Date(2026, 7, 26, 9, 0, 0, 0, time.UTC),
		Alpha:      MustExactRational(1, 200), CumulativeAlpha: MustExactRational(1, 200),
		RandomizationPlanSHA256:     testEvidenceHash("1"),
		AnalysisStratumSchemaSHA256: testEvidenceHash("2"),
		AnalysisStrataSHA256:        testEvidenceHash("3"), ClosedLedgerSHA256: testEvidenceHash("4"),
		QualityReportID:         uuid.MustParse("55555555-5555-4555-8555-555555555555").String(),
		QualityReportSHA256:     testEvidenceHash("5"),
		HarmReportID:            uuid.MustParse("66666666-6666-4666-8666-666666666666").String(),
		HarmReportSHA256:        testEvidenceHash("6"),
		QualityOPEReportID:      uuid.MustParse("77777777-7777-4777-8777-777777777777").String(),
		QualityOPEReportSHA256:  testEvidenceHash("7"),
		HarmOPEReportID:         uuid.MustParse("88888888-8888-4888-8888-888888888888").String(),
		HarmOPEReportSHA256:     testEvidenceHash("8"),
		CensorDiagnosticsID:     uuid.MustParse("99999999-9999-4999-8999-999999999999").String(),
		CensorDiagnosticsSHA256: testEvidenceHash("9"),
		Disposition:             OutcomeDispositionContinue, Eligible: true, IneligibleReasons: []string{},
	}
	bodySHA256, _ := body.SHA256()
	return CanaryOutcomeEvidence{
		SchemaID: "aura.adaptive.evidence/canary-outcome/v1", Revision: 1,
		EvidenceID: uuid.MustParse("aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa").String(),
		Kind:       EvidenceCanaryOutcome, OwnerID: admission.OwnerID, CohortID: admission.CohortID,
		CohortSHA256: admission.CohortSHA256, Domain: admission.Domain,
		ProviderID: admission.ProviderID, ModelID: admission.ModelID,
		PolicyEpoch: admission.PolicyEpoch, PolicyVersion: admission.PolicyVersion,
		SnapshotID: admission.SnapshotID, SnapshotSHA256: admission.SnapshotSHA256,
		CreatedAt: admission.CreatedAt, SealerActorID: admission.SealerActorID,
		SealerCapability: admission.SealerCapability, BodySHA256: bodySHA256, Body: body,
	}
}

func validTransitionBinding(kind EvidenceKind) TransitionBinding {
	look := 1
	cutoff := time.Date(2026, 7, 26, 9, 0, 0, 0, time.UTC)
	binding := TransitionBinding{
		SchemaID: "aura.adaptive.transition-binding/v1", Revision: 1,
		FromState: string(PolicyShadow), TargetState: string(PolicyCanary),
		EvidenceKind:   kind,
		EvidenceID:     uuid.MustParse("aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa").String(),
		EvidenceSHA256: testEvidenceHash("a"),
		OwnerID:        uuid.MustParse("11111111-1111-4111-8111-111111111111").String(),
		Domain:         DomainReasoning, ProviderID: "provider", ModelID: "model",
		PolicyEpoch: 2, PolicyVersion: "policy-v2",
		SnapshotID:     uuid.MustParse("33333333-3333-4333-8333-333333333333").String(),
		SnapshotSHA256: testEvidenceHash("b"),
		CohortID:       uuid.MustParse("22222222-2222-4222-8222-222222222222").String(),
		CohortSHA256:   testEvidenceHash("c"),
		ActorID:        uuid.MustParse("11111111-1111-4111-8111-111111111111").String(),
		Capability:     "adaptive.manage",
	}
	if kind == EvidenceCanaryOutcome {
		binding.FromState = string(PolicyCanary)
		binding.TargetState = string(PolicyActive)
		binding.LookNumber = &look
		binding.LookCutoff = &cutoff
	}
	return binding
}

func evidenceChildRef(kind, schemaID, fill string) ChildArtifactRef {
	return ChildArtifactRef{
		SchemaID: ChildArtifactRefSchemaID, Revision: 1, Kind: kind,
		ArtifactSchemaID: schemaID, ArtifactRevision: 1,
		ArtifactID:     uuid.NewSHA1(uuid.NameSpaceOID, []byte(kind)).String(),
		ArtifactSHA256: testEvidenceHash(fill),
	}
}

func testEvidenceHash(fill string) string {
	return fill + fill + fill + fill + fill + fill + fill + fill +
		fill + fill + fill + fill + fill + fill + fill + fill +
		fill + fill + fill + fill + fill + fill + fill + fill +
		fill + fill + fill + fill + fill + fill + fill + fill +
		fill + fill + fill + fill + fill + fill + fill + fill +
		fill + fill + fill + fill + fill + fill + fill + fill +
		fill + fill + fill + fill + fill + fill + fill + fill +
		fill + fill + fill + fill + fill + fill + fill + fill
}

func mustJSON(t *testing.T, value any) []byte {
	t.Helper()
	encoded, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}
	return encoded
}
