package tools

// Without returns a FRESH registry holding every tool in parent EXCEPT those whose
// Spec().Name is listed in names. The parent registry is never mutated (it is
// immutable for the lifetime of a run — spec.go), so a derived registry that drops
// a tool (e.g. swarm_spawn for swarm workers per D-08/D-10, or swarm_spawn for an
// agent_job child per D-13) can be built per call without touching the parent.
//
// Promoted out of internal/swarm so internal/cron can consume it without importing
// internal/swarm (D-24 forbids that import; OQ2 carve-out).
func Without(parent *Registry, names ...string) *Registry {
	drop := make(map[string]struct{}, len(names))
	for _, n := range names {
		drop[n] = struct{}{}
	}
	out := NewRegistry()
	for _, t := range parent.All() {
		if _, skip := drop[t.Spec().Name]; skip {
			continue
		}
		out.Register(t)
	}
	return out
}
