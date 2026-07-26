package adaptive

import (
	"testing"
	"time"
)

func TestReconstructCohortLedgerAtLookExcludesFutureClaimsAndTheirFacts(
	t *testing.T,
) {
	cohort, firstClaim, firstFact := closedCohortLedgerFixture(t)
	secondClaim, secondFact := additionalCohortLedgerMember(t, cohort, firstFact)
	lookCutoff := firstClaim.ClaimedAt.Add(time.Second)
	firstFact.recordedAt = firstClaim.ClaimedAt.Add(500 * time.Millisecond)
	secondClaim.ClaimedAt = lookCutoff
	secondFact.recordedAt = lookCutoff

	ledger, err := reconstructCohortLedgerAtLook(
		cohort,
		lookCutoff,
		lookCutoff,
		[]FocalClaim{firstClaim, secondClaim},
		[]cohortLedgerFact{firstFact, secondFact},
	)
	if err != nil {
		t.Fatalf("reconstructCohortLedgerAtLook: %v", err)
	}
	if len(ledger.Members) != 1 ||
		ledger.Members[0].Claim.AssignmentID != firstClaim.AssignmentID {
		t.Fatalf("members = %#v, want only the pre-cutoff claim", ledger.Members)
	}
}
