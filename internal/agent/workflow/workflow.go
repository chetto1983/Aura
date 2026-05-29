package workflow

import "github.com/chetto1983/aura/internal/agent"

// joinBranch dot-joins a parent branch label with a child segment (D-15). Branch
// is a LABEL only — hierarchy is reconstructed from span IDs, never by parsing
// this string — so a plain dot-join is sufficient. An empty parent yields just
// the child segment so the root branch is the bare agent/iteration name.
func joinBranch(parent, child string) string {
	if parent == "" {
		return child
	}
	return parent + "." + child
}

// findInTree implements the shared FindAgent contract for an orchestrator: return
// self when name matches, else depth-first recurse into the subs; nil when the
// name is absent from the whole subtree (D-01).
func findInTree(self agent.Agent, subs []agent.Agent, name string) agent.Agent {
	if self.Name() == name {
		return self
	}
	for _, sub := range subs {
		if found := sub.FindAgent(name); found != nil {
			return found
		}
	}
	return nil
}
