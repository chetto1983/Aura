package eval

import (
	"math"
	"slices"
	"strings"
	"testing"

	"github.com/chetto1983/aura/internal/adaptive"
)

func TestAdaptiveBenchmarkReportCanonicalRoundTrip(t *testing.T) {
	t.Parallel()

	report := validAdaptiveBenchmarkReport(t, AdaptiveBenchmarkModeQwenPortability)
	canonical, err := report.CanonicalJSON()
	if err != nil {
		t.Fatalf("CanonicalJSON: %v", err)
	}
	wantPrefix := `{"schema_id":"aura.adaptive.benchmark-report/v1",` +
		`"revision":1,"run_id":`
	if !strings.HasPrefix(string(canonical), wantPrefix) {
		t.Fatalf("canonical prefix = %s", canonical[:min(len(canonical), 160)])
	}
	decoded, err := DecodeAdaptiveBenchmarkReport(canonical)
	if err != nil {
		t.Fatalf("DecodeAdaptiveBenchmarkReport: %v", err)
	}
	reencoded, err := decoded.CanonicalJSON()
	if err != nil {
		t.Fatalf("round-trip CanonicalJSON: %v", err)
	}
	if string(reencoded) != string(canonical) {
		t.Fatal("canonical report changed after strict round trip")
	}
}

func TestDecodeAdaptiveBenchmarkReportRejectsNoncanonicalJSON(t *testing.T) {
	t.Parallel()

	report := validAdaptiveBenchmarkReport(t, AdaptiveBenchmarkModeQwenPortability)
	canonical, err := report.CanonicalJSON()
	if err != nil {
		t.Fatalf("CanonicalJSON: %v", err)
	}
	tests := []struct {
		name    string
		payload []byte
	}{
		{
			name: "unknown field",
			payload: []byte(strings.Replace(
				string(canonical),
				`"revision":1`,
				`"revision":1,"unknown":true`,
				1,
			)),
		},
		{
			name: "duplicate field",
			payload: []byte(strings.Replace(
				string(canonical),
				`"revision":1`,
				`"revision":1,"revision":1`,
				1,
			)),
		},
		{
			name: "top level key order",
			payload: []byte(strings.Replace(
				string(canonical),
				`{"schema_id":"aura.adaptive.benchmark-report/v1","revision":1`,
				`{"revision":1,"schema_id":"aura.adaptive.benchmark-report/v1"`,
				1,
			)),
		},
		{
			name: "nested key order",
			payload: []byte(strings.Replace(
				string(canonical),
				`"git":{"revision":"0123456789abcdef0123456789abcdef01234567","dirty":false`,
				`"git":{"dirty":false,"revision":"0123456789abcdef0123456789abcdef01234567"`,
				1,
			)),
		},
		{name: "noncanonical whitespace", payload: append([]byte(" "), canonical...)},
		{name: "trailing data", payload: append(canonical, []byte(`{}`)...)},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			if _, err := DecodeAdaptiveBenchmarkReport(test.payload); err == nil {
				t.Fatal("DecodeAdaptiveBenchmarkReport accepted noncanonical JSON")
			}
		})
	}
}

func TestValidateAdaptiveBenchmarkReportRejectsDriftAndUnsafeEvidence(
	t *testing.T,
) {
	t.Parallel()

	tests := []struct {
		name   string
		mutate func(*AdaptiveBenchmarkReport)
	}{
		{
			name: "portability flag mismatch",
			mutate: func(report *AdaptiveBenchmarkReport) {
				report.PortabilityOnly = false
			},
		},
		{
			name: "missing scenario",
			mutate: func(report *AdaptiveBenchmarkReport) {
				report.Scenarios = report.Scenarios[:51]
				report.Summary = BuildAdaptiveBenchmarkSummary(report.Scenarios)
			},
		},
		{
			name: "scenario schedule drift",
			mutate: func(report *AdaptiveBenchmarkReport) {
				report.Scenarios[0], report.Scenarios[1] =
					report.Scenarios[1], report.Scenarios[0]
			},
		},
		{
			name: "domain mismatch",
			mutate: func(report *AdaptiveBenchmarkReport) {
				report.Scenarios[0].Domain = adaptive.DomainMemoryRecall
			},
		},
		{
			name: "missing assignment linkage",
			mutate: func(report *AdaptiveBenchmarkReport) {
				report.Scenarios[0].AssignmentID = nil
			},
		},
		{
			name: "duplicate fact linkage",
			mutate: func(report *AdaptiveBenchmarkReport) {
				report.Scenarios[1].AssignmentID =
					report.Scenarios[0].AssignmentID
			},
		},
		{
			name: "action catalog drift",
			mutate: func(report *AdaptiveBenchmarkReport) {
				report.Scenarios[0].ActionProbabilities[0].ActionID = "unknown"
			},
		},
		{
			name: "probability sum",
			mutate: func(report *AdaptiveBenchmarkReport) {
				report.Scenarios[0].ActionProbabilities[0].Probability = 0.25
			},
		},
		{
			name: "negative token count",
			mutate: func(report *AdaptiveBenchmarkReport) {
				report.Scenarios[0].PromptTokens = -1
			},
		},
		{
			name: "negative latency",
			mutate: func(report *AdaptiveBenchmarkReport) {
				report.Scenarios[0].LatencyNS = -1
			},
		},
		{
			name: "nonfinite quality",
			mutate: func(report *AdaptiveBenchmarkReport) {
				value := math.Inf(1)
				report.Scenarios[0].Quality.Value = &value
			},
		},
		{
			name: "passing quality value disagrees",
			mutate: func(report *AdaptiveBenchmarkReport) {
				report.Scenarios[0].Quality.Value = new(float64(0))
			},
		},
		{
			name: "quality reasons disagree",
			mutate: func(report *AdaptiveBenchmarkReport) {
				report.Scenarios[0].Status = AdaptiveBenchmarkStatusFail
				report.Scenarios[0].ReasonCodes = []string{
					AdaptiveBenchmarkReasonAnswerMismatch,
				}
				report.Scenarios[0].Quality.Value = new(float64(0))
				report.Scenarios[0].Quality.ReasonCodes = []string{
					AdaptiveBenchmarkReasonToolMismatch,
				}
				report.Summary = BuildAdaptiveBenchmarkSummary(report.Scenarios)
			},
		},
		{
			name: "token summary overflow",
			mutate: func(report *AdaptiveBenchmarkReport) {
				report.Scenarios[0].PromptTokens = math.MaxInt64
				report.Scenarios[0].CompletionTokens = 0
				report.Scenarios[0].TotalTokens = math.MaxInt64
				report.Scenarios[1].PromptTokens = 1
				report.Scenarios[1].CompletionTokens = 0
				report.Scenarios[1].TotalTokens = 1
				report.Summary = BuildAdaptiveBenchmarkSummary(report.Scenarios)
			},
		},
		{
			name: "cost summary overflow",
			mutate: func(report *AdaptiveBenchmarkReport) {
				report.Scenarios[0].CostUSD = new(math.MaxFloat64)
				report.Scenarios[1].CostUSD = new(math.MaxFloat64)
				report.Summary = BuildAdaptiveBenchmarkSummary(report.Scenarios)
			},
		},
		{
			name: "unregistered reason",
			mutate: func(report *AdaptiveBenchmarkReport) {
				report.Scenarios[0].Status = AdaptiveBenchmarkStatusInvalid
				report.Scenarios[0].ReasonCodes = []string{"raw provider error"}
				report.Summary = BuildAdaptiveBenchmarkSummary(report.Scenarios)
				report.Verdict = AdaptiveBenchmarkStatusInvalid
			},
		},
		{
			name: "missing control",
			mutate: func(report *AdaptiveBenchmarkReport) {
				report.Controls = report.Controls[:15]
			},
		},
		{
			name: "unsorted control evidence",
			mutate: func(report *AdaptiveBenchmarkReport) {
				report.Controls[0].EvidenceIDs = []string{
					benchmarkUUID("z"), benchmarkUUID("a"),
				}
			},
		},
		{
			name: "missing control evidence",
			mutate: func(report *AdaptiveBenchmarkReport) {
				report.Controls[0].EvidenceIDs = []string{}
			},
		},
		{
			name: "summary drift",
			mutate: func(report *AdaptiveBenchmarkReport) {
				report.Summary.TotalTokens++
			},
		},
		{
			name: "partial price provenance",
			mutate: func(report *AdaptiveBenchmarkReport) {
				report.FrozenInputs.PriceTableID = new("prices")
			},
		},
		{
			name: "secret shaped model metadata",
			mutate: func(report *AdaptiveBenchmarkReport) {
				report.Model.ModelID = "https://example.test/?token=secret"
			},
		},
		{
			name: "Qwen artifact drift",
			mutate: func(report *AdaptiveBenchmarkReport) {
				report.Model.ArtifactSHA256 =
					new(strings.Repeat("f", 64))
			},
		},
		{
			name: "Qwen missing server provenance",
			mutate: func(report *AdaptiveBenchmarkReport) {
				report.Model.ServerRevision = nil
			},
		},
		{
			name: "catalog provenance drift",
			mutate: func(report *AdaptiveBenchmarkReport) {
				report.FrozenInputs.ActionCatalogsSHA256 = strings.Repeat("e", 64)
			},
		},
		{
			name: "dataset provenance drift",
			mutate: func(report *AdaptiveBenchmarkReport) {
				report.FrozenInputs.DatasetSHA256 = strings.Repeat("e", 64)
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			report := validAdaptiveBenchmarkReport(
				t,
				AdaptiveBenchmarkModeQwenPortability,
			)
			test.mutate(&report)
			if err := ValidateAdaptiveBenchmarkReport(report); err == nil {
				t.Fatal("ValidateAdaptiveBenchmarkReport accepted invalid report")
			}
		})
	}
}

func TestValidateAdaptiveBenchmarkReportAllowsNullableRuntimeProvenance(
	t *testing.T,
) {
	t.Parallel()

	report := validAdaptiveBenchmarkReport(
		t,
		AdaptiveBenchmarkModeProductionShadow,
	)
	report.Environment.Neo4jRevision = nil
	report.Environment.AgentMemoryRevision = nil
	report.FrozenInputs.SnapshotID = nil
	report.FrozenInputs.SnapshotSHA256 = nil
	if err := ValidateAdaptiveBenchmarkReport(report); err != nil {
		t.Fatalf("nullable runtime provenance: %v", err)
	}
}

func TestBuildAdaptiveBenchmarkSummaryUsesIntegerNearestRankAndMissingness(
	t *testing.T,
) {
	t.Parallel()

	report := validAdaptiveBenchmarkReport(t, AdaptiveBenchmarkModeQwenPortability)
	summary := BuildAdaptiveBenchmarkSummary(report.Scenarios)
	if summary.ScenarioCount != 52 ||
		summary.PassCount != 52 ||
		summary.LatencyCount != 52 ||
		summary.LatencyP50NS == nil || *summary.LatencyP50NS != 26 ||
		summary.LatencyP95NS == nil || *summary.LatencyP95NS != 50 ||
		summary.LatencyP99NS == nil || *summary.LatencyP99NS != 52 ||
		summary.CostCount != 0 || summary.CostUSD != nil {
		t.Fatalf("summary = %#v", summary)
	}
	if got := make([]adaptive.Domain, len(summary.Domains)); len(got) != 5 {
		t.Fatalf("domain summaries = %d, want 5", len(got))
	} else {
		for index := range summary.Domains {
			got[index] = summary.Domains[index].Domain
		}
		want := []adaptive.Domain{
			adaptive.DomainReasoning,
			adaptive.DomainToolDiscovery,
			adaptive.DomainSkillRouting,
			adaptive.DomainKnowledge,
			adaptive.DomainMemoryRecall,
		}
		if !slices.Equal(got, want) {
			t.Fatalf("domain order = %v, want %v", got, want)
		}
	}
}

func TestAdaptiveBenchmarkVerdictSeparatesPortabilityFromQuality(t *testing.T) {
	t.Parallel()

	qwen := validAdaptiveBenchmarkReport(t, AdaptiveBenchmarkModeQwenPortability)
	qwen.Scenarios[0].Status = AdaptiveBenchmarkStatusFail
	qwen.Scenarios[0].ReasonCodes = []string{
		AdaptiveBenchmarkReasonAnswerMismatch,
	}
	qwen.Scenarios[0].Quality.Value = new(float64(0))
	qwen.Scenarios[0].Quality.ReasonCodes = []string{
		AdaptiveBenchmarkReasonAnswerMismatch,
	}
	qwen.Summary = BuildAdaptiveBenchmarkSummary(qwen.Scenarios)
	if got := ExpectedAdaptiveBenchmarkVerdict(qwen); got !=
		AdaptiveBenchmarkStatusPass {
		t.Fatalf("Qwen quality-only verdict = %q, want pass", got)
	}

	qwen.Scenarios[0].ReasonCodes = []string{
		AdaptiveBenchmarkReasonModelTransportFailure,
	}
	qwen.Scenarios[0].Quality.ReasonCodes = []string{
		AdaptiveBenchmarkReasonModelTransportFailure,
	}
	if got := ExpectedAdaptiveBenchmarkVerdict(qwen); got !=
		AdaptiveBenchmarkStatusFail {
		t.Fatalf("Qwen portability failure verdict = %q, want fail", got)
	}

	production := validAdaptiveBenchmarkReport(
		t,
		AdaptiveBenchmarkModeProductionShadow,
	)
	production.Scenarios[0].Status = AdaptiveBenchmarkStatusFail
	production.Scenarios[0].ReasonCodes = []string{
		AdaptiveBenchmarkReasonAnswerMismatch,
	}
	production.Scenarios[0].Quality.Value = new(float64(0))
	production.Scenarios[0].Quality.ReasonCodes = []string{
		AdaptiveBenchmarkReasonAnswerMismatch,
	}
	production.Summary = BuildAdaptiveBenchmarkSummary(production.Scenarios)
	if got := ExpectedAdaptiveBenchmarkVerdict(production); got !=
		AdaptiveBenchmarkStatusFail {
		t.Fatalf("production quality verdict = %q, want fail", got)
	}
}

func TestAdaptiveBenchmarkPromotionEvidenceRejectsPortabilityStructurally(
	t *testing.T,
) {
	t.Parallel()

	qwen := validAdaptiveBenchmarkReport(t, AdaptiveBenchmarkModeQwenPortability)
	if _, err := NewAdaptiveBenchmarkPromotionEvidence(qwen); err == nil {
		t.Fatal("promotion evidence accepted portability-only report")
	}
	production := validAdaptiveBenchmarkReport(
		t,
		AdaptiveBenchmarkModeProductionShadow,
	)
	dataset := benchmarkDatasetFixture(t)
	scenarioRun, err := RunAdaptiveBenchmarkScenarios(
		t.Context(),
		dataset,
		newAdaptiveBenchmarkDriverFake(dataset),
	)
	if err != nil {
		t.Fatalf("RunAdaptiveBenchmarkScenarios: %v", err)
	}
	controlRun, err := RunAdaptiveBenchmarkControls(
		t.Context(),
		&adaptiveBenchmarkControlDriverFake{},
	)
	if err != nil {
		t.Fatalf("RunAdaptiveBenchmarkControls: %v", err)
	}
	production, err = FinalizeAdaptiveBenchmarkReport(
		production,
		scenarioRun,
		controlRun,
	)
	if err != nil {
		t.Fatalf("FinalizeAdaptiveBenchmarkReport: %v", err)
	}
	if _, err := NewAdaptiveBenchmarkPromotionEvidence(production); err != nil {
		t.Fatalf("verified production promotion evidence: %v", err)
	}
	canonical, err := production.CanonicalJSON()
	if err != nil {
		t.Fatalf("CanonicalJSON: %v", err)
	}
	decoded, err := DecodeAdaptiveBenchmarkReport(canonical)
	if err != nil {
		t.Fatalf("DecodeAdaptiveBenchmarkReport: %v", err)
	}
	if _, err := NewAdaptiveBenchmarkPromotionEvidence(decoded); err != nil {
		t.Fatalf("persisted production promotion evidence: %v", err)
	}
	if err := VerifyAdaptiveBenchmarkReportForPromotion(decoded); err != nil {
		t.Fatalf("verify persisted promotion evidence: %v", err)
	}
	tampered := decoded
	tampered.FrozenInputs.DatasetSHA256 = strings.Repeat("f", 64)
	if err := VerifyAdaptiveBenchmarkReportForPromotion(tampered); err == nil {
		t.Fatal("promotion verification accepted tampered persisted evidence")
	}

	production.Scenarios[0].Status = AdaptiveBenchmarkStatusFail
	production.Scenarios[0].ReasonCodes = []string{
		AdaptiveBenchmarkReasonAnswerMismatch,
	}
	production.Scenarios[0].Quality.Value = new(float64(0))
	production.Scenarios[0].Quality.ReasonCodes = []string{
		AdaptiveBenchmarkReasonAnswerMismatch,
	}
	production.Summary = BuildAdaptiveBenchmarkSummary(production.Scenarios)
	production.Verdict = ExpectedAdaptiveBenchmarkVerdict(production)
	if _, err := NewAdaptiveBenchmarkPromotionEvidence(production); err == nil {
		t.Fatal("promotion evidence accepted a failed report verdict")
	}

	production, err = FinalizeAdaptiveBenchmarkReport(
		validAdaptiveBenchmarkReport(
			t,
			AdaptiveBenchmarkModeProductionShadow,
		),
		scenarioRun,
		controlRun,
	)
	if err != nil {
		t.Fatalf("FinalizeAdaptiveBenchmarkReport: %v", err)
	}
	production.Controls[0].Status = AdaptiveBenchmarkStatusFail
	production.Controls[0].ReasonCodes = []string{
		AdaptiveBenchmarkReasonControlFailed,
	}
	production.Verdict = ExpectedAdaptiveBenchmarkVerdict(production)
	if _, err := NewAdaptiveBenchmarkPromotionEvidence(production); err == nil {
		t.Fatal("promotion evidence accepted a failed safety control")
	}
}

func TestFinalizeAdaptiveBenchmarkReportRequiresVerifiedRuns(t *testing.T) {
	t.Parallel()

	dataset := benchmarkDatasetFixture(t)
	scenarioRun, err := RunAdaptiveBenchmarkScenarios(
		t.Context(),
		dataset,
		newAdaptiveBenchmarkDriverFake(dataset),
	)
	if err != nil {
		t.Fatalf("RunAdaptiveBenchmarkScenarios: %v", err)
	}
	controlRun, err := RunAdaptiveBenchmarkControls(
		t.Context(),
		&adaptiveBenchmarkControlDriverFake{},
	)
	if err != nil {
		t.Fatalf("RunAdaptiveBenchmarkControls: %v", err)
	}
	report := validAdaptiveBenchmarkReport(
		t,
		AdaptiveBenchmarkModeProductionShadow,
	)
	finalized, err := FinalizeAdaptiveBenchmarkReport(
		report,
		scenarioRun,
		controlRun,
	)
	if err != nil {
		t.Fatalf("FinalizeAdaptiveBenchmarkReport: %v", err)
	}
	if len(finalized.Scenarios) != 52 ||
		len(finalized.Controls) != 16 ||
		finalized.Verdict != AdaptiveBenchmarkStatusPass {
		t.Fatalf("finalized report = %#v", finalized)
	}

	controlRun.verified = false
	if _, err := FinalizeAdaptiveBenchmarkReport(
		report,
		scenarioRun,
		controlRun,
	); err == nil {
		t.Fatal("report finalizer accepted an unverified control run")
	}
}
