package adaptive

import (
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestReconstructCohortLedgerRejectsRawClaimPoison(t *testing.T) {
	cohort, claim, assignmentFact := closedCohortLedgerFixture(t)
	tests := []struct {
		name   string
		mutate func(*FocalCohort, *FocalClaim)
	}{
		{
			name:   "nil claim ID",
			mutate: func(_ *FocalCohort, claim *FocalClaim) { claim.ID = uuid.Nil },
		},
		{
			name:   "owner",
			mutate: func(_ *FocalCohort, claim *FocalClaim) { claim.OwnerID = uuid.Must(uuid.NewV7()) },
		},
		{
			name:   "cohort",
			mutate: func(_ *FocalCohort, claim *FocalClaim) { claim.CohortID = uuid.Must(uuid.NewV7()) },
		},
		{
			name:   "request",
			mutate: func(_ *FocalCohort, claim *FocalClaim) { claim.RequestID = uuid.Must(uuid.NewV7()) },
		},
		{
			name:   "conversation",
			mutate: func(_ *FocalCohort, claim *FocalClaim) { claim.EvaluationConversationID = uuid.Nil },
		},
		{
			name:   "predicate",
			mutate: func(_ *FocalCohort, claim *FocalClaim) { claim.PointOrdinal++ },
		},
		{
			name: "claimed at cutoff",
			mutate: func(cohort *FocalCohort, claim *FocalClaim) {
				claim.ClaimedAt = cohort.Cutoff()
				claim.TimeBlockStart = cohortLedgerTestTimeBlockStart(claim.ClaimedAt, cohort.Admission().TimeBlockSeconds)
			},
		},
		{
			name:   "time block",
			mutate: func(_ *FocalCohort, claim *FocalClaim) { claim.TimeBlockStart = claim.TimeBlockStart.Add(time.Second) },
		},
		{
			name:   "missing required session",
			mutate: func(_ *FocalCohort, claim *FocalClaim) { claim.SessionID = nil },
		},
		{
			name:   "missing required episode",
			mutate: func(_ *FocalCohort, claim *FocalClaim) { claim.EpisodeID = nil },
		},
		{
			name: "extra optional session",
			mutate: func(cohort *FocalCohort, claim *FocalClaim) {
				cohort.spec.Admission.BlockKeys = []BlockKey{BlockOwner, BlockTimeBlock}
				sessionID := uuid.Must(uuid.NewV7())
				claim.SessionID = &sessionID
				claim.EpisodeID = nil
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			candidate := *cohort
			candidate.spec.Admission.BlockKeys = append([]BlockKey(nil), cohort.Admission().BlockKeys...)
			poisoned := claim
			test.mutate(&candidate, &poisoned)
			_, err := reconstructCohortLedger(&candidate, candidate.Cutoff(), []FocalClaim{poisoned}, []cohortLedgerFact{assignmentFact})
			if !errors.Is(err, ErrInvalidCohortLedger) {
				t.Fatalf("reconstructCohortLedger() error = %v, want ErrInvalidCohortLedger", err)
			}
		})
	}
}

func cohortLedgerTestTimeBlockStart(claimedAt time.Time, seconds uint64) time.Time {
	blockSeconds := int64(seconds)
	epochSeconds := claimedAt.UTC().Unix()
	quotient := epochSeconds / blockSeconds
	if epochSeconds < 0 && epochSeconds%blockSeconds != 0 {
		quotient--
	}
	return time.Unix(quotient*blockSeconds, 0).UTC()
}
