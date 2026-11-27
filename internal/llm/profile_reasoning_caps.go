package llm

import (
	"context"
	"slices"
)

// The account catalogue is resolved into the route snapshot at save and boot, for
// both account-catalogue routes (ChatGPT and Anthropic). Reusing that snapshot keeps
// the composer and policy on the inference model's set.
type profileReasoningCaps struct {
	efforts   []ReasoningEffort
	mandatory bool
}

func (s *profileReasoningCaps) AllowedEfforts(context.Context) ([]ReasoningEffort, ReasoningEffort, bool) {
	efforts := slices.Clone(s.efforts)
	if s.mandatory {
		efforts = excludeNone(efforts)
	}
	return efforts, "", len(efforts) > 0
}
