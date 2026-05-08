package orchestration

import "time"

type LoopPolicy struct {
	MaxSteps                int
	TerminalTools           []string
	AllowNoToolFinalization bool
	DuplicateToolPolicy     string
	MaxElapsed              time.Duration
}

func LoopPolicyForToolset(toolset Toolset) (LoopPolicy, bool) {
	switch normalizeToolset(string(toolset)) {
	case ToolsetDefault:
		return cloneLoopPolicy(LoopPolicy{
			MaxSteps:                4,
			TerminalTools:           []string{"run_aurabot_swarm"},
			AllowNoToolFinalization: true,
			DuplicateToolPolicy:     "Reject duplicate tool calls with identical arguments; keep default turns short and evidence-backed.",
			MaxElapsed:              30 * time.Second,
		}), true
	case ToolsetCompute:
		return cloneLoopPolicy(LoopPolicy{
			MaxSteps:                4,
			TerminalTools:           []string{"execute_code"},
			AllowNoToolFinalization: true,
			DuplicateToolPolicy:     "Reject repeated execute_code calls that recompute the same artifact; allow one final no-tool response after execute_code.",
			MaxElapsed:              30 * time.Second,
		}), true
	case ToolsetDocument:
		return cloneLoopPolicy(LoopPolicy{
			MaxSteps:                6,
			TerminalTools:           []string{"create_docx", "create_xlsx", "create_pdf"},
			AllowNoToolFinalization: true,
			DuplicateToolPolicy:     "Reject duplicate file generation calls for the same target format unless revising a failed artifact.",
			MaxElapsed:              45 * time.Second,
		}), true
	case ToolsetAdmin:
		return cloneLoopPolicy(LoopPolicy{
			MaxSteps:                6,
			AllowNoToolFinalization: true,
			DuplicateToolPolicy:     "Reject duplicate admin calls that request the same review or mutation action.",
			MaxElapsed:              30 * time.Second,
		}), true
	default:
		return LoopPolicy{}, false
	}
}

func cloneLoopPolicy(policy LoopPolicy) LoopPolicy {
	policy.TerminalTools = append([]string(nil), policy.TerminalTools...)
	return policy
}
