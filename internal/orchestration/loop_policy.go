package orchestration

import "time"

type LoopPolicy struct {
	MaxSteps                int
	TerminalTools           []string
	AllowNoToolFinalization bool
	DuplicateToolPolicy     string
	MaxElapsed              time.Duration
}

func LoopPolicyForProfile(profile Profile) (LoopPolicy, bool) {
	card, ok := ProfileCardFor(profile)
	if !ok {
		return LoopPolicy{}, false
	}
	return cloneLoopPolicy(card.LoopPolicy), true
}

func cloneLoopPolicy(policy LoopPolicy) LoopPolicy {
	policy.TerminalTools = append([]string(nil), policy.TerminalTools...)
	return policy
}
