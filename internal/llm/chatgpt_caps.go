package llm

import (
	"context"
	"slices"
)

// The account catalogue is resolved into the route snapshot at save and boot.
// Reusing that snapshot keeps the composer and policy on the inference model's set.
type chatGPTReasoningCaps struct {
	efforts   []ReasoningEffort
	mandatory bool
}

func (s *chatGPTReasoningCaps) AllowedEfforts(context.Context) ([]ReasoningEffort, ReasoningEffort, bool) {
	efforts := slices.Clone(s.efforts)
	if s.mandatory {
		efforts = excludeNone(efforts)
	}
	return efforts, "", len(efforts) > 0
}
