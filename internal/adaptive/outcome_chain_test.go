package adaptive

import (
	"errors"
	"testing"

	"github.com/google/uuid"
)

func TestFoldOutcomeChainReturnsSameLeafRegardlessOfFactOrder(t *testing.T) {
	t.Parallel()
	evaluator := validEvaluatorRegistration(EvaluatorDeterministic).Provenance()
	rootID := uuid.Must(uuid.NewV7())
	firstID := uuid.Must(uuid.NewV7())
	secondID := uuid.Must(uuid.NewV7())
	facts := []outcomeChainFact{
		{id: rootID, kind: EventOutcome, evaluator: evaluator},
		{
			id: firstID, kind: EventCorrection, evaluator: evaluator,
			supersedes: rootID,
		},
		{
			id: secondID, kind: EventCorrection, evaluator: evaluator,
			supersedes: firstID,
		},
	}
	for _, order := range [][]outcomeChainFact{
		facts,
		{facts[2], facts[1], facts[0]},
		{facts[1], facts[0], facts[2]},
	} {
		leaf, err := foldOutcomeChain(order, rootID)
		if err != nil {
			t.Fatalf("foldOutcomeChain: %v", err)
		}
		if leaf.id != secondID {
			t.Fatalf("leaf = %s, want %s", leaf.id, secondID)
		}
	}
}

func TestValidateCorrectionAppendRejectsMissingScopeForkAndCycle(t *testing.T) {
	t.Parallel()
	evaluator := validEvaluatorRegistration(EvaluatorDeterministic).Provenance()
	otherEvaluator := validEvaluatorRegistration(EvaluatorCalibratedJudge).Provenance()
	rootID := uuid.Must(uuid.NewV7())
	childID := uuid.Must(uuid.NewV7())
	newID := uuid.Must(uuid.NewV7())
	facts := []outcomeChainFact{
		{id: rootID, kind: EventOutcome, evaluator: evaluator},
		{
			id: childID, kind: EventCorrection, evaluator: evaluator,
			supersedes: rootID,
		},
	}
	base := CorrectionObservation{
		Evaluator:          evaluator,
		SupersedesEventID:  childID,
		CorrectionSourceID: uuid.Must(uuid.NewV7()),
	}
	tests := []struct {
		name       string
		facts      []outcomeChainFact
		eventID    uuid.UUID
		correction CorrectionObservation
		target     error
	}{
		{
			name: "missing",
			correction: func() CorrectionObservation {
				value := base
				value.SupersedesEventID = uuid.Must(uuid.NewV7())
				return value
			}(),
			target: ErrCorrectionTargetNotFound,
		},
		{
			name: "scope",
			correction: func() CorrectionObservation {
				value := base
				value.Evaluator = otherEvaluator
				return value
			}(),
			target: ErrCorrectionScopeMismatch,
		},
		{
			name: "fork",
			correction: func() CorrectionObservation {
				value := base
				value.SupersedesEventID = rootID
				return value
			}(),
			target: ErrCorrectionFork,
		},
		{
			name:    "new self cycle",
			eventID: childID,
			correction: func() CorrectionObservation {
				value := base
				value.SupersedesEventID = childID
				return value
			}(),
			target: ErrCorrectionCycle,
		},
		{
			name: "persisted cycle",
			facts: []outcomeChainFact{
				{
					id: rootID, kind: EventCorrection, evaluator: evaluator,
					supersedes: childID,
				},
				{
					id: childID, kind: EventCorrection, evaluator: evaluator,
					supersedes: rootID,
				},
			},
			target: ErrCorrectionCycle,
		},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			testFacts := test.facts
			if testFacts == nil {
				testFacts = facts
			}
			eventID := test.eventID
			if eventID == uuid.Nil {
				eventID = newID
			}
			if err := validateCorrectionAppend(
				testFacts,
				eventID,
				test.correction,
			); !errors.Is(err, test.target) {
				t.Fatalf("validateCorrectionAppend error = %v, want %v", err, test.target)
			}
		})
	}
}
