package swarm

import (
	"fmt"
	"slices"
)

// DirectWriteToolNames is the single source of truth for tool names that
// perform direct mutations. A write_proposal subagent MUST NOT have any of
// these in its ToolAllowlist; proposals must flow through propose_patch
// instead.
var DirectWriteToolNames = []string{
	"wiki_page",
	"source",
	"task",
	"file",
	"agent_note",
}

// EnforceWriteProposalAllowlist validates that allowlist is safe for a
// write_proposal subagent. It must be non-empty, must contain "propose_patch",
// and must not contain any name in DirectWriteToolNames.
func EnforceWriteProposalAllowlist(allowlist []string) error {
	if len(allowlist) == 0 {
		return fmt.Errorf("nodespec: write_proposal requires a non-empty ToolAllowlist containing %q", "propose_patch")
	}
	if !slices.Contains(allowlist, "propose_patch") {
		return fmt.Errorf("nodespec: write_proposal ToolAllowlist must contain %q", "propose_patch")
	}
	for _, name := range allowlist {
		if slices.Contains(DirectWriteToolNames, name) {
			return fmt.Errorf("nodespec: write_proposal ToolAllowlist must not contain direct-write tool %q", name)
		}
	}
	return nil
}
