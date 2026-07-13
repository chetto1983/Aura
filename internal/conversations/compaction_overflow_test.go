package conversations

import (
	"errors"
	"testing"
)

func TestBudgetDecisionL24BeforeL25(t *testing.T) {
	decision, err := DecideCompactionBudget(BudgetDecisionInput{
		BudgetInput:      BudgetInput{WindowTokens: 100_000, ReservedOutputTokens: 10_000, RenderedHistoryTokens: 80_000},
		SemanticEligible: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !decision.SemanticCandidate || decision.EmergencyCandidate {
		t.Fatalf("decision = %+v; want semantic candidate only", decision)
	}
	if decision.Reason != BudgetReasonSemanticRequired {
		t.Fatalf("reason = %q", decision.Reason)
	}
}

func TestBudgetDecisionWaiverAllowsL25(t *testing.T) {
	for _, waiver := range []L24Waiver{WaiverDisabled, WaiverNoEligiblePrefix, WaiverProviderUnsupported, WaiverQualityRejected, WaiverTimeout} {
		t.Run(string(waiver), func(t *testing.T) {
			decision, err := DecideCompactionBudget(BudgetDecisionInput{
				BudgetInput: BudgetInput{WindowTokens: 100_000, ReservedOutputTokens: 10_000, RenderedHistoryTokens: 80_000},
				Waiver:      waiver,
			})
			if err != nil || !decision.EmergencyCandidate || decision.Waiver != waiver {
				t.Fatalf("decision=%+v err=%v", decision, err)
			}
		})
	}
	if _, err := DecideCompactionBudget(BudgetDecisionInput{BudgetInput: BudgetInput{WindowTokens: 100_000, ReservedOutputTokens: 10_000, RenderedHistoryTokens: 80_000}, Waiver: "provider_error"}); !errors.Is(err, ErrInvalidL24Waiver) {
		t.Fatalf("invalid waiver error = %v", err)
	}
}

func TestBudgetDecisionHysteresis(t *testing.T) {
	decision, err := DecideCompactionBudget(BudgetDecisionInput{
		BudgetInput:      BudgetInput{WindowTokens: 100_000, ReservedOutputTokens: 10_000, RenderedHistoryTokens: 80_000},
		SemanticEligible: true, HysteresisActive: true,
	})
	if err != nil || decision.SemanticCandidate || decision.EmergencyCandidate || decision.Reason != BudgetReasonHysteresis {
		t.Fatalf("decision=%+v err=%v", decision, err)
	}
}

func TestBudgetDecisionInvalidCapacityHasNoCandidate(t *testing.T) {
	decision, err := DecideCompactionBudget(BudgetDecisionInput{BudgetInput: BudgetInput{WindowTokens: 1000, ReservedOutputTokens: 1000}, SemanticEligible: true, Waiver: WaiverTimeout})
	if !errors.Is(err, ErrInsufficientInputCapacity) || decision.SemanticCandidate || decision.EmergencyCandidate {
		t.Fatalf("decision=%+v err=%v", decision, err)
	}
}
