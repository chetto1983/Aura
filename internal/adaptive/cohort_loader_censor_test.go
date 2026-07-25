package adaptive

import (
	"slices"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestReconstructCohortLedgerPreservesCensoredPrimaryRoot(t *testing.T) {
	cohort, claim, assignmentFact := closedCohortLedgerFixture(t)
	facts := append([]cohortLedgerFact{assignmentFact}, successfulCohortOutcomeFacts(t, cohort, assignmentFact)...)
	facts[2] = censoredCohortOutcomeFact(
		t,
		cohort,
		assignmentFact.assignment,
		cohort.PrimaryQualityEndpoint().Evaluator,
	)

	ledger, err := reconstructCohortLedger(cohort, cohort.Cutoff(), []FocalClaim{claim}, facts)
	if err != nil {
		t.Fatalf("reconstructCohortLedger() error = %v", err)
	}
	assertCensoredPrimaryCohortMember(t, ledger)
}

func TestReconstructCohortLedgerPreservesCensoredPrimaryCorrectionLeaf(t *testing.T) {
	cohort, claim, assignmentFact := closedCohortLedgerFixture(t)
	facts := append([]cohortLedgerFact{assignmentFact}, successfulCohortOutcomeFacts(t, cohort, assignmentFact)...)
	facts = append(facts, censoredCohortCorrectionFact(
		t,
		cohort,
		assignmentFact.assignment,
		cohort.PrimaryQualityEndpoint().Evaluator,
		facts[2].event.ID,
	))

	ledger, err := reconstructCohortLedger(cohort, cohort.Cutoff(), []FocalClaim{claim}, facts)
	if err != nil {
		t.Fatalf("reconstructCohortLedger() error = %v", err)
	}
	assertCensoredPrimaryCohortMember(t, ledger)
}

func assertCensoredPrimaryCohortMember(t *testing.T, ledger CohortLedger) {
	t.Helper()
	member := ledger.Members[0]
	if ledger.State != CohortLedgerClosed || member.Quality == nil || member.Quality.Status != OutcomeCensored ||
		member.Harm == nil || member.Harm.Status != OutcomeObserved || member.Observed || !member.ITTEligible || member.OPEEligible ||
		!slices.Equal(member.ReasonCodes, []string{"ope_ineligible"}) || ledger.CensorDiagnostics.Members != 1 ||
		ledger.CensorDiagnostics.Censored != 1 || ledger.CensorDiagnostics.OverallCensorRate != 1 {
		t.Fatalf("censored primary ledger = %#v", ledger)
	}
}

func censoredCohortOutcomeFact(t *testing.T, cohort *FocalCohort, assignment Assignment, evaluator EvaluatorProvenance) cohortLedgerFact {
	t.Helper()
	catalog, err := NewEvaluatorCatalog(cohort.Evaluators()...)
	if err != nil {
		t.Fatal(err)
	}
	observedAt := cohort.Cutoff().Add(-time.Second)
	event, err := NewOutcomeEvent(assignment, OutcomeObservation{
		SchemaVersion: SchemaVersion2, AssignmentID: assignment.AssignmentID, OwnerID: assignment.OwnerID,
		Domain: assignment.Domain, ProviderID: assignment.ProviderID, ModelID: assignment.ModelID, Evaluator: evaluator,
		SourceObservationID: uuid.Must(uuid.NewV7()), Status: OutcomeCensored, ObservedAt: observedAt,
	}, catalog)
	if err != nil {
		t.Fatal(err)
	}
	return cohortLedgerFact{event: event, recordedAt: observedAt}
}

func censoredCohortCorrectionFact(t *testing.T, cohort *FocalCohort, assignment Assignment, evaluator EvaluatorProvenance, supersedes uuid.UUID) cohortLedgerFact {
	t.Helper()
	catalog, err := NewEvaluatorCatalog(cohort.Evaluators()...)
	if err != nil {
		t.Fatal(err)
	}
	observedAt := cohort.Cutoff().Add(-time.Second)
	event, err := NewCorrectionEvent(assignment, CorrectionObservation{
		SchemaVersion: SchemaVersion2, AssignmentID: assignment.AssignmentID, OwnerID: assignment.OwnerID,
		Domain: assignment.Domain, ProviderID: assignment.ProviderID, ModelID: assignment.ModelID, Evaluator: evaluator,
		CorrectionSourceID: uuid.Must(uuid.NewV7()), SupersedesEventID: supersedes, Status: OutcomeCensored, ObservedAt: observedAt,
	}, catalog)
	if err != nil {
		t.Fatal(err)
	}
	return cohortLedgerFact{event: event, recordedAt: observedAt}
}
