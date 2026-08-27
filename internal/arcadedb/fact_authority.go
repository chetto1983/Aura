package arcadedb

// fact_authority.go: D-11's supersede-authority check, extracted from
// memory.go (543/600 LOC before this phase, CLAUDE.md's NO GOD CLASS rule)
// into its own file -- mirroring internal/runner's precedent of pulling one
// policy/authorization concern into its own file beside the orchestration
// file it serves (resume_policy.go beside runner.go).
//
// D-11: a worker ADDS facts; closing a still-valid fact stays with the parent
// turn and the operator. This removes the concurrency hazard by SCOPE rather
// than by locking -- UpsertFact is a sequence of separate Command calls, each
// its own transaction, and only the create leg (attachFactSource) has race
// compensation. closeSuperseded has none, so letting N concurrent workers
// each attempt a correction would race two UPDATE...WHERE valid_to IS NULL
// statements against the same rows with no serialization at all. Scoping
// supersede to the parent/operator removes the race by removing the second
// writer, not by adding a lock this phase explicitly defers (the Script
// BEGIN/COMMIT method, internal/arcadedb/client.go:268, stays unused).

// Actor identifies who is attempting a write or a supersede: the same
// RunID+WriterRole a Fact's Source carries, named separately here because
// maySupersede's question ("may THIS actor close a fact") is about the actor,
// not about the specific fact being written.
type Actor struct {
	RunID string
	Role  WriterRole
}

// actorFromSource reads the actor a FactSource already carries -- the SAME
// host-derived value cmd/arcadedb-mcp attached before UpsertFact was ever
// called (D-10), never re-derived or guessed here.
func actorFromSource(source FactSource) Actor {
	return Actor{RunID: source.RunID, Role: source.WriterRole}
}

// maySupersede reports whether actor may close (supersede) an existing fact.
// The refusal reason is model-readable prose naming what the worker may do
// instead, following the same "domain rejection rides in the result, not a Go
// error" idiom already established for swarm_spawn's cap refusals and
// closeSuperseded's own ambiguity refusal -- a worker asking to correct
// something is not a wiring bug, it is a request the model can act on.
func maySupersede(actor Actor) (allowed bool, reason string) {
	if actor.Role == WriterWorker {
		return false, "a worker may not supersede an existing fact -- only the parent " +
			"turn or the operator can close a fact's validity window; add a new fact " +
			"instead of correcting one, and let the parent turn reconcile it later"
	}
	return true, ""
}
