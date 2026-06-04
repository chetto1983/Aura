package swarm

import "github.com/chetto1983/aura/internal/agent/tools"

// Without returns a FRESH registry holding every tool in parent EXCEPT those whose
// Spec().Name is listed in names. The parent registry is never mutated (it is
// immutable for the lifetime of a run — spec.go) so a worker registry that drops
// swarm_spawn (D-08/D-10 flat-v1 depth guard) can be derived per call without
// touching the parent the coordinator itself runs against.
func Without(parent *tools.Registry, names ...string) *tools.Registry {
	drop := make(map[string]struct{}, len(names))
	for _, n := range names {
		drop[n] = struct{}{}
	}
	out := tools.NewRegistry()
	for _, t := range parent.All() {
		if _, skip := drop[t.Spec().Name]; skip {
			continue
		}
		out.Register(t)
	}
	return out
}
