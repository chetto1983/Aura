package agent

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/chetto1983/aura/internal/idempotency"
	"github.com/chetto1983/aura/internal/identityctx"
	"github.com/google/uuid"
)

// schedulerRunParentContext builds the parent operation a scheduler dispatch
// establishes, byte-for-byte as internal/cron/dispatch.go does it: the key is
// task.ID + ":" + claim.RunID and the fingerprint carries TaskID, RunID, Kind and
// Payload. Mirroring the real shape is the whole point — a hand-simplified parent
// would prove nothing about the production derivation.
func schedulerRunParentContext(t *testing.T, taskID, runID string) context.Context {
	t.Helper()
	fingerprint, err := idempotency.FingerprintTyped(struct {
		TaskID  string `json:"task_id"`
		RunID   string `json:"run_id"`
		Kind    string `json:"kind"`
		Payload []byte `json:"payload"`
	}{TaskID: taskID, RunID: runID, Kind: "agent_job", Payload: []byte(`{"goal":"x"}`)})
	if err != nil {
		t.Fatal(err)
	}
	parent := idempotency.Operation{
		Key: idempotency.OperationKey{
			IdentityID: identityctx.LocalOperatorIdentity,
			Scope:      idempotency.ScopeSchedulerRun,
			Key:        taskID + ":" + runID,
		},
		Fingerprint: fingerprint,
		Correlation: runID,
	}
	ctx, err := idempotency.WithOperation(
		identityctx.WithIdentityID(context.Background(), identityctx.LocalOperatorIdentity), parent,
	)
	if err != nil {
		t.Fatal(err)
	}
	return ctx
}

// TestSchedulerReclaimCannotReplayAcrossRuns is the executable half of a claim made
// in 45-VALIDATION.md, and it exists to keep an inaccurate comment from being
// believed.
//
// deriveToolOperationContext documents that "a genuine same-round retry, OR A
// SCHEDULER RECLAIM whose ordinal restarts at 1 against the same stable parent
// operation, still derives the identical key and collapses onto one execution
// (HARN-02)". The first clause holds. The second cannot occur, because the parent
// key a scheduler dispatch builds embeds claim.RunID — and every claim mints a fresh
// run id (internal/cron/claim.go -> insertRunOnConn -> newUUID(); there is exactly
// ONE INSERT INTO aura.agent_job_runs in the tree and nothing re-dispatches an
// existing row). So a re-fire is a DIFFERENT parent, and a different parent is a
// different child key, and a different child key can never hit DecisionReplay.
//
// This test pins that: same task, same tool, same args, ordinal restarted at 1 —
// only the run id differs, exactly as it would across a reclaim — and the derived
// keys MUST differ. If someone later makes the parent key stable across a reclaim
// (the fix that would make the comment true), this test fails loudly and tells them
// the replay path just became reachable, which is the moment HARN-03 becomes
// live-provable.
func TestSchedulerReclaimCannotReplayAcrossRuns(t *testing.T) {
	spec := childOperationSpec()
	args := json.RawMessage(`{"action":"restore","name":"calc"}`)
	taskID := uuid.New().String()
	requestID := uuid.New()

	// Two dispatches of the SAME task. The ordinal restarts at 1 both times, so the
	// round is not what separates them — only the run id is.
	ctxFirst := withModelRound(schedulerRunParentContext(t, taskID, uuid.New().String()),
		modelRound{requestID: requestID, ordinal: 1})
	ctxReclaim := withModelRound(schedulerRunParentContext(t, taskID, uuid.New().String()),
		modelRound{requestID: requestID, ordinal: 1})

	derivedFirst, err := deriveToolOperationContext(ctxFirst, spec, args)
	if err != nil {
		t.Fatalf("first dispatch derive: %v", err)
	}
	derivedReclaim, err := deriveToolOperationContext(ctxReclaim, spec, args)
	if err != nil {
		t.Fatalf("reclaim dispatch derive: %v", err)
	}

	opFirst, ok := idempotency.OperationFromContext(derivedFirst)
	if !ok {
		t.Fatal("first dispatch carries no derived operation")
	}
	opReclaim, ok := idempotency.OperationFromContext(derivedReclaim)
	if !ok {
		t.Fatal("reclaim dispatch carries no derived operation")
	}

	if opFirst.Key.Key == opReclaim.Key.Key {
		t.Fatalf("scheduler reclaim derived the SAME child key %q across two run ids — "+
			"the replay path just became reachable; HARN-03 is now live-provable and "+
			"45-VALIDATION.md's structural-blocker finding is stale", opFirst.Key.Key)
	}
}

// TestSchedulerSameRunSameRoundDerivesIdenticalKey is the control. It proves the
// divergence above is caused by the run id ALONE and not by some incidental
// instability in the derivation: hold the run id fixed, keep the ordinal at 1, and
// the key must be identical. This is the "genuine same-round retry" clause of the
// same comment — the half that IS reachable, and the only surviving route to a live
// replay today.
func TestSchedulerSameRunSameRoundDerivesIdenticalKey(t *testing.T) {
	spec := childOperationSpec()
	args := json.RawMessage(`{"action":"restore","name":"calc"}`)
	taskID := uuid.New().String()
	runID := uuid.New().String()
	requestID := uuid.New()

	ctxA := withModelRound(schedulerRunParentContext(t, taskID, runID),
		modelRound{requestID: requestID, ordinal: 1})
	ctxB := withModelRound(schedulerRunParentContext(t, taskID, runID),
		modelRound{requestID: requestID, ordinal: 1})

	derivedA, err := deriveToolOperationContext(ctxA, spec, args)
	if err != nil {
		t.Fatalf("attempt A derive: %v", err)
	}
	derivedB, err := deriveToolOperationContext(ctxB, spec, args)
	if err != nil {
		t.Fatalf("attempt B derive: %v", err)
	}

	opA, _ := idempotency.OperationFromContext(derivedA)
	opB, _ := idempotency.OperationFromContext(derivedB)
	if opA.Key.Key != opB.Key.Key {
		t.Fatalf("same run + same round derived DIFFERENT keys (%q vs %q) — a genuine "+
			"same-round retry would execute twice instead of replaying (HARN-02)",
			opA.Key.Key, opB.Key.Key)
	}
}
