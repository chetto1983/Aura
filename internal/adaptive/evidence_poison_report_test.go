package adaptive

import (
	"slices"
	"testing"
)

func TestPoisonReportsSealEndpointDistinctIneligibleDiagnostics(t *testing.T) {
	cohort, look, ledger, qualityModel, harmModel, opePlan := opeReportFixture(t)
	ledger.Members = nil
	ledger.PoisonedMemberCount = 40
	ledger.SHA256 = repeatedSHA("d")

	gates, err := buildPoisonGateReports(cohort, look, ledger)
	if err != nil {
		t.Fatalf("buildPoisonGateReports() error = %v", err)
	}
	if gates.GateValid || gates.Quality.Computed || gates.Harm.Computed ||
		gates.Quality.Passes || gates.Harm.Passes ||
		!slices.Equal(gates.Quality.IneligibleReasons, []string{"poisoned_ledger"}) ||
		!slices.Equal(gates.Harm.IneligibleReasons, []string{"poisoned_ledger"}) ||
		gates.Censor.TotalMembers != ledger.PoisonedMemberCount ||
		!slices.Equal(gates.Censor.Reasons, []string{"poisoned_ledger"}) {
		t.Fatalf("poison gate reports = %#v", gates)
	}
	if !validSHA256(gates.QualitySHA256) ||
		!validSHA256(gates.HarmSHA256) ||
		!validSHA256(gates.CensorSHA256) ||
		gates.QualitySHA256 == gates.HarmSHA256 {
		t.Fatalf("poison gate hashes = %#v", gates)
	}

	interference := validInterferencePlanArtifact()
	cluster, err := NewInterferenceCluster(
		cohort.Scope().OwnerID.String(), cohort.Scope().OwnerID.String(),
	)
	if err != nil {
		t.Fatalf("NewInterferenceCluster() error = %v", err)
	}
	interference.ClusterSchemaSHA256 = cluster.SchemaSHA256
	interferenceCanonical, err := interference.CanonicalJSON()
	if err != nil {
		t.Fatalf("interference CanonicalJSON() error = %v", err)
	}
	ope, err := buildPoisonOPEReports(
		cohort, look, ledger,
		outcomeChildren{
			interference: EvidenceArtifactRegistration{
				Canonical: interferenceCanonical,
			},
			qualityModel: qualityModel,
			harmModel:    harmModel,
			opePlan:      opePlan,
		},
	)
	if err != nil {
		t.Fatalf("buildPoisonOPEReports() error = %v", err)
	}
	for _, report := range []EndpointOPEReport{ope.Quality, ope.Harm} {
		if report.Eligible || report.RowCount != uint64(ledger.PoisonedMemberCount) ||
			!slices.Equal(report.IneligibleReasons, []string{"poisoned_ledger"}) {
			t.Fatalf("poison OPE report = %#v", report)
		}
	}
	if ope.Quality.ReportID == ope.Harm.ReportID ||
		ope.QualitySHA256 == ope.HarmSHA256 ||
		!validSHA256(ope.QualitySHA256) || !validSHA256(ope.HarmSHA256) {
		t.Fatalf("poison OPE bindings = %#v", ope)
	}
}
