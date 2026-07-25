package adaptive

import (
	"testing"
	"time"
)

func TestReconstructCohortLedgerCanonicalizesDigestTimestamps(t *testing.T) {
	cohort, claim, assignmentFact := closedCohortLedgerFixture(t)
	facts := append([]cohortLedgerFact{assignmentFact}, successfulCohortOutcomeFacts(t, cohort, assignmentFact)...)
	utcLedger, err := reconstructCohortLedger(cohort, cohort.Cutoff(), []FocalClaim{claim}, facts)
	if err != nil {
		t.Fatalf("reconstruct UTC ledger: %v", err)
	}

	location := time.FixedZone("UTC+02", 2*60*60)
	localClaim := claim
	localClaim.ClaimedAt = claim.ClaimedAt.In(location)
	localClaim.TimeBlockStart = claim.TimeBlockStart.In(location)
	localFacts := append([]cohortLedgerFact(nil), facts...)
	for index := range localFacts {
		localFacts[index].recordedAt = localFacts[index].recordedAt.In(location)
	}
	localLedger, err := reconstructCohortLedger(cohort, cohort.Cutoff(), []FocalClaim{localClaim}, localFacts)
	if err != nil {
		t.Fatalf("reconstruct local ledger: %v", err)
	}
	if localLedger.SHA256 != utcLedger.SHA256 {
		t.Fatalf("digest for equivalent instants = %s, want %s", localLedger.SHA256, utcLedger.SHA256)
	}
	if localLedger.Members[0].Claim.ClaimedAt.Location() != time.UTC ||
		localLedger.Members[0].Claim.TimeBlockStart.Location() != time.UTC {
		t.Fatalf("member claim timestamps = %#v, want UTC", localLedger.Members[0].Claim)
	}
	for _, fact := range localLedger.IncludedFacts {
		if fact.RecordedAt.Location() != time.UTC {
			t.Fatalf("included fact timestamp = %v, want UTC", fact.RecordedAt)
		}
	}
}
