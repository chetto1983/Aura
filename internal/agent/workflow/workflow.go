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
// self when name matches, else recurse into the subtree; nil when the name is
// absent (D-01).
//
// AG-037: the Agent interface is open by design, so a misconfigured graph can be
// cyclic or diamond-shaped (an agent appearing under two parents, or a back-edge
// to an ancestor). A naive recursion through sub.FindAgent would stack-overflow.
// This traversal is iterative with a visited set keyed by agent identity, so a
// cycle terminates and a diamond is visited once. It descends via SubAgents()
// (the structural contract) rather than delegating to each sub's FindAgent, so
// the visited set is honored across the whole subtree.
func findInTree(self agent.Agent, subs []agent.Agent, name string) agent.Agent {
	if self.Name() == name {
		return self
	}
	visited := map[agent.Agent]struct{}{self: {}}
	queue := append([]agent.Agent(nil), subs...)
	for len(queue) > 0 {
		cur := queue[0]
		queue = queue[1:]
		if cur == nil {
			continue
		}
		if _, seen := visited[cur]; seen {
			continue
		}
		visited[cur] = struct{}{}
		if cur.Name() == name {
			return cur
		}
		queue = append(queue, cur.SubAgents()...)
	}
	return nil
}
