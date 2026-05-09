package orchestration

import "time"

type LoopPolicy struct {
	TerminalTools           []string
	AllowNoToolFinalization bool
	DuplicateToolPolicy     string
	MaxElapsed              time.Duration
}

func LoopPolicyForToolset(toolset Toolset) (LoopPolicy, bool) {
	switch normalizeToolset(string(toolset)) {
	case ToolsetDefault:
		return cloneLoopPolicy(LoopPolicy{
			AllowNoToolFinalization: true,
			DuplicateToolPolicy:     "Reject duplicate tool calls with identical arguments; let the model choose among exposed tools.",
			MaxElapsed:              30 * time.Second,
		}), true
	case ToolsetCompute:
		return cloneLoopPolicy(LoopPolicy{
			TerminalTools:           []string{"execute_code"},
			AllowNoToolFinalization: true,
			DuplicateToolPolicy:     "Reject repeated execute_code calls that recompute the same artifact; allow one final no-tool response after execute_code.",
			MaxElapsed:              30 * time.Second,
		}), true
	case ToolsetDocument:
		return cloneLoopPolicy(LoopPolicy{
			TerminalTools:           []string{"create_docx", "create_xlsx", "create_pdf"},
			AllowNoToolFinalization: true,
			DuplicateToolPolicy:     "Reject duplicate retrieval and file generation calls with the same arguments; document turns should use compact retrieval before one typed file tool.",
			MaxElapsed:              30 * time.Second,
		}), true
	case ToolsetAdmin:
		return cloneLoopPolicy(LoopPolicy{
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
