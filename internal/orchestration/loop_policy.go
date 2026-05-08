package orchestration

import "time"

type LoopPolicy struct {
	MaxSteps                int
	TerminalTools           []string
	AllowNoToolFinalization bool
	DuplicateToolPolicy     string
	MaxElapsed              time.Duration
}

func LoopPolicyForToolset(toolset Profile) (LoopPolicy, bool) {
	card, ok := ProfileCardFor(toolset)
	if !ok {
		return LoopPolicy{}, false
	}
	return cloneLoopPolicy(card.LoopPolicy), true
}

func LoopPolicyForProfile(profile Profile) (LoopPolicy, bool) {
	return LoopPolicyForToolset(profile)
}

func cloneLoopPolicy(policy LoopPolicy) LoopPolicy {
	policy.TerminalTools = append([]string(nil), policy.TerminalTools...)
	return policy
}
