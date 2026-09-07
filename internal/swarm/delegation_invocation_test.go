package swarm

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/chetto1983/aura/internal/agent"
	"github.com/chetto1983/aura/internal/agent/tools"
	"github.com/chetto1983/aura/internal/idempotency"
	"github.com/chetto1983/aura/internal/identityctx"
)

// The live cockpit reproduced two successful swarm_spawn calls, but only the
// first created jobs: the adapter never populated the legacy ParentRunID.
func TestDelegationInvocationSeparatesFreshCallsAndPreservesRetry(t *testing.T) {
	const owner = "11111111-1111-1111-1111-111111111111"
	rc := testRunConfig(t, newRouter(), 30)
	store := &fakeDelegationStore{}
	adapter := NewRunnerAdapter(rc.Cfg)
	adapter.Enqueuer = &DelegationEnqueuer{Store: store}
	goals := []string{"calculate the sum", "calculate the hash"}
	invoke := func(operationKey, brief string) delegationQueuedResult {
		t.Helper()
		ctx := identityctx.WithIdentityID(withToolCtx(context.Background(), t), owner)
		ctx = agent.WithSwarmContext(ctx, rc.ParentBudget, rc.ParentRegistry, rc.Client, rc.LLM, rc.ConvID, nil)
		args, err := json.Marshal(map[string]any{"goals": goals, "context": brief})
		if err != nil {
			t.Fatal(err)
		}
		fingerprint, err := tools.OperationFingerprint((&tools.SwarmSpawn{}).Spec(), args)
		if err != nil {
			t.Fatal(err)
		}
		ctx, err = idempotency.WithOperation(ctx, idempotency.Operation{
			Key:         idempotency.OperationKey{IdentityID: owner, Scope: idempotency.ScopeAgentTool, Key: operationKey},
			Fingerprint: fingerprint,
		})
		if err != nil {
			t.Fatal(err)
		}
		result, err := adapter.Run(ctx, goals, brief)
		if err != nil {
			t.Fatal(err)
		}
		var queued delegationQueuedResult
		if err := json.Unmarshal([]byte(result.Preview), &queued); err != nil {
			t.Fatal(err)
		}
		return queued
	}
	first := invoke("child:first-turn", "input=100")
	retry := invoke("child:first-turn", "input=100")
	next := invoke("child:second-turn", "input=100")
	changedContext := invoke("child:changed-context", "input=200")
	for i := range goals {
		if first.Workers[i].ChildID != retry.Workers[i].ChildID || store.created[i].IdempotencyKey != store.created[2+i].IdempotencyKey {
			t.Fatal("retry must preserve worker identity and the durable queue key")
		}
		for _, fresh := range []delegationQueuedResult{next, changedContext} {
			if first.Workers[i].ChildID == fresh.Workers[i].ChildID {
				t.Errorf("a fresh invocation reused completed worker %s", first.Workers[i].ChildID)
			}
		}
	}
	firstFanout := store.created[0].Payload["fanout_key"]
	if firstFanout != store.created[2].Payload["fanout_key"] {
		t.Fatal("retry changed fanout identity")
	}
	for _, offset := range []int{4, 6} {
		if firstFanout == store.created[offset].Payload["fanout_key"] {
			t.Error("fresh invocation reused a completed fanout delivery")
		}
		if firstKey := store.created[0].IdempotencyKey; firstKey == store.created[offset].IdempotencyKey {
			t.Error("fresh invocation would collapse onto the old durable queue row")
		}
	}
}
