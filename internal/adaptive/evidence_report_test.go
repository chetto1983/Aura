package adaptive

import (
	"bytes"
	"context"
	"math"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestBuildGateReportsBindsDistinctExactEndpointsAndCensoring(t *testing.T) {
	t.Parallel()

	cohort, _ := randomizedFocalCohortUnitFixture(t)
	cohort.spec.Power.MinimumMembers = 2
	qualityZero, qualityOne, harmZero := 0.0, 1.0, 0.0
	stratumID := testEvidenceHash("a")
	clusterA, clusterB := testEvidenceHash("b"), testEvidenceHash("c")
	member := func(
		requestID, assignmentID uuid.UUID,
		armID string,
		quality *float64,
		clusterID string,
	) CohortMember {
		return CohortMember{
			Claim: FocalClaim{
				ID:      uuid.NewSHA1(uuid.NameSpaceOID, requestID[:]),
				OwnerID: cohort.Scope().OwnerID, CohortID: cohort.ID(),
				RequestID: requestID, AssignmentID: assignmentID,
				AnalysisStratumSchemaSHA256:     cohort.v2.randomizationPlan.AnalysisStratumSchemaSHA256,
				AnalysisStratumID:               stratumID,
				InterferenceClusterSchemaSHA256: testEvidenceHash("d"),
				InterferenceClusterID:           clusterID,
			},
			Assignment: Assignment{
				AssignmentID: assignmentID, ArmID: armID,
				IntendedActionID: StaticActionID,
			},
			Quality: &CohortEffectiveObservation{
				Status: OutcomeObserved, Measures: OutcomeMeasures{Quality: quality},
			},
			Harm: &CohortEffectiveObservation{
				Status: OutcomeObserved, Measures: OutcomeMeasures{Harm: &harmZero},
			},
			ITTEligible: true,
		}
	}
	requestA := uuid.MustParse("aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa")
	requestB := uuid.MustParse("bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb")
	ledger := CohortLedger{
		CohortID: cohort.ID(), CohortSHA256: cohort.SHA256(),
		State: CohortLedgerClosed, SHA256: testEvidenceHash("e"), Sealable: true,
		Members: []CohortMember{
			member(requestA, uuid.MustParse("11111111-1111-4111-8111-111111111111"), "baseline", &qualityZero, clusterA),
			member(requestB, uuid.MustParse("22222222-2222-4222-8222-222222222222"), "challenger", &qualityOne, clusterB),
		},
		CensorDiagnostics: CohortCensorDiagnostics{
			Members: 2,
			ArmDiagnostics: []CohortArmCensorDiagnostics{
				{ArmID: "baseline", Members: 1},
				{ArmID: "challenger", Members: 1},
			},
		},
	}
	look := EvidenceLook{
		Number: 1, Cutoff: cohort.Cutoff().Add(-time.Minute),
		Alpha:           MustExactRational(1, 200),
		CumulativeAlpha: MustExactRational(1, 200),
		PredecessorRule: "none",
	}
	reports, err := buildGateReports(context.Background(), cohort, look, ledger)
	if err != nil {
		t.Fatalf("buildGateReports: %v", err)
	}
	if !reports.Quality.Computed || reports.Quality.ATELower == nil ||
		!reports.Harm.Computed || reports.Harm.ATEUpper == nil ||
		reports.Quality.ReportID == reports.Harm.ReportID ||
		reports.QualitySHA256 == reports.HarmSHA256 ||
		!reports.Censor.Passes {
		t.Fatalf("gate reports = %#v", reports)
	}
}

func TestEndpointOPEReports_BindDistinctQualityAndHarmArtifacts(t *testing.T) {
	quality := validEndpointOPEReport(EvidenceEndpointQuality)
	harm := validEndpointOPEReport(EvidenceEndpointHarm)
	qualityBytes, err := quality.CanonicalJSON()
	if err != nil {
		t.Fatalf("quality CanonicalJSON() error = %v", err)
	}
	harmBytes, err := harm.CanonicalJSON()
	if err != nil {
		t.Fatalf("harm CanonicalJSON() error = %v", err)
	}
	qualityHash, err := quality.SHA256()
	if err != nil {
		t.Fatalf("quality SHA256() error = %v", err)
	}
	harmHash, err := harm.SHA256()
	if err != nil {
		t.Fatalf("harm SHA256() error = %v", err)
	}
	if bytes.Equal(qualityBytes, harmBytes) || qualityHash == harmHash {
		t.Fatal("quality and harm reports share canonical content")
	}
	if err := ValidateDistinctEndpointOPEReports(quality, harm); err != nil {
		t.Fatalf("ValidateDistinctEndpointOPEReports() error = %v", err)
	}
}

func TestEndpointOPEReport_RejectsMalformedCanonicalFields(t *testing.T) {
	base := validEndpointOPEReport(EvidenceEndpointQuality)
	tests := []struct {
		name   string
		mutate func(*EndpointOPEReport)
	}{
		{name: "schema", mutate: func(report *EndpointOPEReport) { report.Revision = 2 }},
		{name: "uuid", mutate: func(report *EndpointOPEReport) { report.OwnerID = "" }},
		{name: "look", mutate: func(report *EndpointOPEReport) { report.LookNumber = 0 }},
		{name: "endpoint", mutate: func(report *EndpointOPEReport) { report.EndpointID = "" }},
		{name: "hash", mutate: func(report *EndpointOPEReport) { report.ClosedLedgerSHA256 = "" }},
		{name: "reason order", mutate: func(report *EndpointOPEReport) {
			report.Eligible = false
			report.IneligibleReasons = []string{"z", "a"}
		}},
		{name: "reason count", mutate: func(report *EndpointOPEReport) {
			report.Eligible = false
			report.IneligibleReasons = []string{"missing"}
			report.IneligibleReasonCounts = []OPEIneligibleReasonCount{{
				SchemaID: "wrong", Revision: 1, Reason: "missing", Count: 1,
			}}
		}},
		{name: "eligible reasons", mutate: func(report *EndpointOPEReport) {
			report.IneligibleReasons = []string{"missing"}
		}},
		{name: "nonfinite", mutate: func(report *EndpointOPEReport) {
			value := math.Inf(1)
			report.DR = &value
		}},
		{name: "missing estimator", mutate: func(report *EndpointOPEReport) { report.DR = nil }},
		{name: "overlap", mutate: func(report *EndpointOPEReport) {
			value := MustExactRational(2, 1)
			report.ObservedMinimumOverlap = &value
		}},
		{name: "uncertainty", mutate: func(report *EndpointOPEReport) {
			report.UncertaintyRevision = 2
		}},
		{name: "interval", mutate: func(report *EndpointOPEReport) {
			lower, upper := 2.0, 1.0
			report.DRLower, report.DRUpper = &lower, &upper
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			report := base
			test.mutate(&report)
			if _, err := report.CanonicalJSON(); err == nil {
				t.Fatal("CanonicalJSON() accepted malformed report")
			}
			if _, err := report.SHA256(); err == nil {
				t.Fatal("SHA256() accepted malformed report")
			}
		})
	}
	harm := validEndpointOPEReport(EvidenceEndpointHarm)
	if err := ValidateDistinctEndpointOPEReports(harm, base); err == nil {
		t.Fatal("ValidateDistinctEndpointOPEReports() accepted reversed endpoints")
	}
}

func TestEndpointOPEReports_RejectCrossEndpointAndCompositeSubstitution(t *testing.T) {
	quality := validEndpointOPEReport(EvidenceEndpointQuality)
	harm := validEndpointOPEReport(EvidenceEndpointHarm)
	harm.ReportID = quality.ReportID
	if err := ValidateDistinctEndpointOPEReports(quality, harm); err == nil {
		t.Fatal("ValidateDistinctEndpointOPEReports() accepted shared report id")
	}
	harm = validEndpointOPEReport(EvidenceEndpointHarm)
	harm.OutcomeModelSHA256 = quality.OutcomeModelSHA256
	if err := ValidateDistinctEndpointOPEReports(quality, harm); err == nil {
		t.Fatal("ValidateDistinctEndpointOPEReports() accepted shared endpoint model")
	}
	quality.EndpointKind = "quality_harm"
	if _, err := quality.CanonicalJSON(); err == nil {
		t.Fatal("CanonicalJSON() accepted a composite endpoint")
	}
}

func validEndpointOPEReport(endpoint EvidenceEndpointKind) EndpointOPEReport {
	reportID := "11111111-1111-4111-8111-111111111111"
	modelID := "22222222-2222-4222-8222-222222222222"
	modelHash := repeatedSHA("a")
	predictionHash := repeatedSHA("b")
	endpointID := "quality.primary"
	if endpoint == EvidenceEndpointHarm {
		reportID = "33333333-3333-4333-8333-333333333333"
		modelID = "44444444-4444-4444-8444-444444444444"
		modelHash = repeatedSHA("c")
		predictionHash = repeatedSHA("d")
		endpointID = "harm.primary"
	}
	value := 1.0
	overlap := MustExactRational(1, 2)
	return EndpointOPEReport{
		SchemaID: "aura.adaptive.report/ope/v1", Revision: 1,
		ReportID: reportID, OwnerID: "55555555-5555-4555-8555-555555555555",
		CohortID:   "66666666-6666-4666-8666-666666666666",
		LookNumber: 1, LookCutoff: time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC),
		EndpointKind: endpoint, EndpointID: endpointID,
		Evaluator: CanonicalEvaluator{
			Kind: "deterministic", ID: "golden", Version: "1", RubricID: "binary",
			CalibrationID: "golden-v1", ProvenanceSHA256: repeatedSHA("e"),
		},
		ClosedLedgerSHA256:              repeatedSHA("1"),
		AnalysisStratumSchemaSHA256:     repeatedSHA("2"),
		InterferenceClusterSchemaSHA256: repeatedSHA("3"),
		OPEPlanSHA256:                   repeatedSHA("4"), TargetPolicySHA256: repeatedSHA("5"),
		BehaviorPolicySHA256: repeatedSHA("6"), OutcomeModelID: modelID,
		OutcomeModelSHA256: modelHash, PredictionSHA256: predictionHash,
		Eligible: true, IneligibleReasons: []string{},
		RowCount: 2, IneligibleRowCount: 0, IneligibleReasonCounts: []OPEIneligibleReasonCount{},
		DR: &value, SNIPS: &value, ESS: &value,
		MinimumESS: MustExactRational(1, 1), MinimumOverlap: overlap,
		ObservedMinimumOverlap: &overlap,
		UncertaintyAlgorithmID: "cluster-percentile-bootstrap-sha256-v1",
		UncertaintyRevision:    1, Confidence: MustExactRational(19, 20),
		DRLower: &value, DRUpper: &value,
	}
}
