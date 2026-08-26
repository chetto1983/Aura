package agent

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/chetto1983/aura/internal/agent/tools"
	"github.com/chetto1983/aura/internal/idempotency"
)

var errUnsupportedParentOperation = errors.New("unsupported parent operation scope")

// errMissingModelRound is returned when a child key must be derived (a parent
// operation of a different scope is present) but no modelRound is on ctx. This is
// a fail-closed sentinel, mirroring errUnsupportedParentOperation's shape: a
// silent fallback to ordinal 0 would restore the pre-D-01 collapsing behaviour
// that let a deliberate cross-round re-issue replay a stale result (F-1).
var errMissingModelRound = errors.New("missing model round for tool operation derivation")

// deriveToolOperationContext turns an ingress/scheduler operation into a stable
// child owned by the tool's Aura-declared scope and canonical argument intent.
// Request and tool-call IDs stay audit-only and therefore never participate.
//
// The child key's fingerprint carries RoundOrdinal (D-01): two derivations that
// differ only by round produce different keys, so a deliberate cross-round
// re-issue of a mutating call executes again instead of replaying a stale round's
// recorded result (HARN-01). A genuine same-round retry — the same parent operation,
// the same ordinal, the same canonical arguments — still derives the identical key and
// collapses onto one execution (HARN-02).
//
// A scheduler RECLAIM does not, and this once said it did. The parent a scheduler
// dispatch builds is keyed `task.ID + ":" + claim.RunID` with RunID also in its
// fingerprint (cron/dispatch.go), and every claim mints a fresh run id — there is one
// INSERT INTO aura.agent_job_runs in the tree, it takes newUUID(), and nothing
// re-dispatches an existing row (recoverOrphans marks unknown_recovery instead). So a
// re-fire is always a different parent, hence a different child key, hence never a
// replay. Pinned by TestSchedulerReclaimCannotReplayAcrossRuns, which fails loudly if
// someone makes the parent stable across a reclaim — the change that would make the
// old sentence true, and would also make HARN-03 provable without a probe.
func deriveToolOperationContext(ctx context.Context, spec tools.Spec, args json.RawMessage) (context.Context, error) {
	parent, ok := idempotency.OperationFromContext(ctx)
	if !ok {
		return ctx, nil
	}
	toolFingerprint, err := tools.OperationFingerprint(spec, args)
	if err != nil {
		return nil, err
	}
	// Passthrough is a RE-ENTRY guard, so it keys on the whole identity of the call
	// (scope AND fingerprint), never on scope alone. Scope alone was wrong the moment
	// one agent-scoped tool could run inside another: swarm_spawn shares
	// OperationScopeAgent with the ten tools a worker may call, so a worker's own
	// dispatch matched this guard, inherited swarm_spawn's operation verbatim, and
	// gateway.beginOperation then recomputed the fingerprint for the worker's actual
	// tool and denied it as an "operation fingerprint mismatch" — measured live in
	// spike 099 at 4/4 workers, 100% of dispatches, deterministic.
	if parent.Key.Scope == spec.OperationScope && parent.Fingerprint == toolFingerprint {
		return ctx, nil
	}
	switch parent.Key.Scope {
	case idempotency.ScopeHTTPMutation, idempotency.ScopeCLICommand,
		idempotency.ScopeSchedulerRun, idempotency.ScopeApproval,
		// ScopeAgentTool as a PARENT is the nested case above: a tool operation may
		// own another tool operation, one level per delegation hop. The derived key
		// still carries the parent's key and fingerprint, so a worker's call is
		// scoped to the exact swarm_spawn invocation that spawned it.
		idempotency.ScopeAgentTool:
	default:
		return nil, errUnsupportedParentOperation
	}
	round, ok := modelRoundFromContext(ctx)
	if !ok {
		return nil, errMissingModelRound
	}
	childKeyFingerprint, err := idempotency.FingerprintTyped(struct {
		Version           string            `json:"version"`
		ParentScope       idempotency.Scope `json:"parent_scope"`
		ParentKey         string            `json:"parent_key"`
		ParentFingerprint string            `json:"parent_fingerprint"`
		ToolScope         idempotency.Scope `json:"tool_scope"`
		ToolFingerprint   string            `json:"tool_fingerprint"`
		RoundOrdinal      uint32            `json:"round_ordinal"`
	}{
		Version: "tool-child-v2", ParentScope: parent.Key.Scope, ParentKey: parent.Key.Key,
		ParentFingerprint: idempotency.FingerprintHex(parent.Fingerprint), ToolScope: spec.OperationScope,
		ToolFingerprint: idempotency.FingerprintHex(toolFingerprint), RoundOrdinal: round.ordinal,
	})
	if err != nil {
		return nil, err
	}
	return idempotency.WithOperation(ctx, idempotency.Operation{
		Key: idempotency.OperationKey{
			IdentityID: parent.Key.IdentityID,
			Scope:      spec.OperationScope,
			Key:        "child:" + idempotency.FingerprintHex(childKeyFingerprint),
		},
		Fingerprint: toolFingerprint,
		Correlation: idempotency.FingerprintHex(parent.Fingerprint),
	})
}
