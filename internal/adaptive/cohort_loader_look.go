package adaptive

import (
	"fmt"
	"time"

	"github.com/google/uuid"
)

func reconstructCohortLedgerAtLook(
	cohort *FocalCohort,
	lookCutoff time.Time,
	closureTime time.Time,
	claims []FocalClaim,
	facts []cohortLedgerFact,
) (CohortLedger, error) {
	if cohort == nil || !canonicalEvidenceTime(lookCutoff) ||
		lookCutoff.After(cohort.Cutoff()) {
		return CohortLedger{}, fmt.Errorf("%w: look cutoff", ErrInvalidCohortLedger)
	}
	if closureTime.Before(lookCutoff) {
		return CohortLedger{
			CohortID: cohort.ID(), CohortSHA256: cohort.SHA256(),
			State: CohortLedgerOpen,
		}, nil
	}
	futureAssignments := make(map[uuid.UUID]struct{})
	closedClaims := make([]FocalClaim, 0, len(claims))
	for _, claim := range claims {
		if !claim.ClaimedAt.Round(0).UTC().Before(lookCutoff) {
			futureAssignments[claim.AssignmentID] = struct{}{}
			continue
		}
		closedClaims = append(closedClaims, claim)
	}
	closedFacts := make([]cohortLedgerFact, 0, len(facts))
	for _, fact := range facts {
		if fact.recordedAt.IsZero() {
			return CohortLedger{}, fmt.Errorf("%w: recorded_at", ErrInvalidCohortLedger)
		}
		assignmentID := fact.event.DecisionID
		if fact.event.Kind == EventDecision {
			assignment, err := DecodeAssignment(fact.event.Payload)
			if err != nil {
				return CohortLedger{}, fmt.Errorf("%w: assignment payload", ErrInvalidCohortLedger)
			}
			assignmentID = assignment.AssignmentID
		}
		if _, future := futureAssignments[assignmentID]; future {
			continue
		}
		closedFacts = append(closedFacts, fact)
	}
	atLook := *cohort
	atLook.spec = canonicalCohortSpec(cohort.spec)
	atLook.spec.Cutoff = lookCutoff
	return reconstructCohortLedger(
		&atLook, closureTime, closedClaims, closedFacts,
	)
}
