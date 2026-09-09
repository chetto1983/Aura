// swarm_llm_resolve.go is the ONE place runChild resolves the LLM client and
// config a worker runs with (T-02-08b/CRED-07/D-11) — split out of swarm.go
// so the choice (identity resolver, process runtime, or an already-resolved
// parent client) is named and testable on its own rather than inlined at the
// call site.
package swarm

import (
	"context"
	"errors"

	"github.com/chetto1983/aura/internal/llm"
)

// identityLLMResolver is the consumer-side port onto
// internal/runner.IdentityLLMResolver: resolve identityID's own RuntimeSnapshot.
// Declared here (not importing internal/runner) so this package stays free of
// the composition-root's dependency shape; *runner.IdentityLLMResolver satisfies
// it structurally.
type identityLLMResolver interface {
	SnapshotFor(ctx context.Context, identityID string) (llm.RuntimeSnapshot, error)
}

// ErrNoWorkerCredential is returned when a worker's RunConfig carries none of
// Resolver+IdentityID, Runtime, or an already-resolved Client — the fail-closed
// case a misconfigured composition root used to skip past silently by falling
// through to whatever rc.Client happened to hold (T-02-08b). It is refused
// rather than passing a nil llm.Client into agent.NewLlmAgent, which would
// panic on the first Stream call instead of failing cleanly.
var ErrNoWorkerCredential = errors.New("swarm: no LLM credential source configured for this worker")

// resolveWorkerLLM picks the client+config a worker runs with, in priority
// order:
//
//  1. rc.Resolver + rc.IdentityID, when both are set: the identity's OWN
//     credential (CRED-07) — the delegation claim loop's headless workers
//     resolve this way, keyed on the claimed job's owning identity
//     (delegation_run.go's runWithHeartbeat sets RunConfig.IdentityID from
//     job.IdentityID).
//  2. rc.Runtime, when set: a fresh process-wide snapshot, re-read at spawn
//     time rather than captured once at boot.
//  3. rc.Client, when set: an ALREADY-RESOLVED client the caller passed in
//     directly — this is RunnerAdapter.Run's shape for the synchronous
//     swarm_spawn fan-out, where every goal in one call shares the SAME
//     parent-inherited client (rc.Runtime is deliberately nil there; the
//     inheritance already happened before RunConfig was built). Removing this
//     branch would break that legitimate case, not just the boot-time-capture
//     bug T-02-08b targets.
//  4. Neither of the above: ErrNoWorkerCredential. This is the closure —
//     previously a RunConfig with an unset Runtime silently fell back to
//     whatever rc.Client held, which was the process-wide deployment client
//     when the composition root captured it at boot (T-02-08b). The
//     composition root no longer does that (cmd/aura/serve_delegation.go,
//     serve_dispatch.go), so "neither set" is now a real, testable refusal
//     instead of a silent deployment-key default.
func resolveWorkerLLM(ctx context.Context, rc RunConfig) (llm.Client, llm.Config, error) {
	if rc.Resolver != nil && rc.IdentityID != "" {
		snapshot, err := rc.Resolver.SnapshotFor(ctx, rc.IdentityID)
		if err != nil {
			return nil, llm.Config{}, err
		}
		return snapshot.Client, snapshot.Config, nil
	}
	if rc.Runtime != nil {
		snapshot := rc.Runtime.Snapshot()
		return snapshot.Client, snapshot.Config, nil
	}
	if rc.Client != nil {
		return rc.Client, rc.LLM, nil
	}
	return nil, llm.Config{}, ErrNoWorkerCredential
}
