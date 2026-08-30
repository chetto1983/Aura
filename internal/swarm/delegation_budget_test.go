package swarm

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/chetto1983/aura/internal/agent/agenttest"
	"github.com/chetto1983/aura/internal/documents"
	"github.com/chetto1983/aura/internal/identityctx"
	"github.com/chetto1983/aura/internal/llm"
)

type runtimeSnapshotSequence struct {
	mu        sync.Mutex
	snapshots []llm.RuntimeSnapshot
	calls     int
}

func (s *runtimeSnapshotSequence) Snapshot() llm.RuntimeSnapshot {
	s.mu.Lock()
	defer s.mu.Unlock()
	index := min(s.calls, len(s.snapshots)-1)
	s.calls++
	return s.snapshots[index]
}

func (s *runtimeSnapshotSequence) callCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.calls
}

type runtimeProfileClient struct {
	t     *testing.T
	mu    sync.Mutex
	calls int
}

func (c *runtimeProfileClient) Stream(_ context.Context, req llm.Request) (<-chan llm.Chunk, error) {
	c.t.Helper()
	if req.Model != "old-model" {
		c.t.Fatalf("request model = %q, want the first runtime snapshot", req.Model)
	}
	c.mu.Lock()
	c.calls++
	call := c.calls
	c.mu.Unlock()
	if call <= 2 {
		return toolChan(agenttest.MakeToolCall(fmt.Sprintf("call-%d", call), "echo", fmt.Sprintf(`{"v":"step-%d"}`, call))), nil
	}
	return closedChan(llm.Chunk{Text: "old profile completed"}, llm.Chunk{FinishReason: "stop"}), nil
}

func (c *runtimeProfileClient) callCount() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.calls
}

type rejectRuntimeClient struct{ t *testing.T }

func (c rejectRuntimeClient) Stream(context.Context, llm.Request) (<-chan llm.Chunk, error) {
	c.t.Helper()
	c.t.Fatal("background delegation mixed in the replacement runtime client")
	return nil, nil
}

func TestBackgroundDelegationUsesExactlyOneRuntimeSnapshot(t *testing.T) {
	t.Setenv("AURA_LOOP_MAX_STEPS", "")
	t.Setenv("AURA_LOOP_MAX_WALLCLOCK_SEC", "")

	oldClient := &runtimeProfileClient{t: t}
	newClient := rejectRuntimeClient{t: t}
	rc := testRunConfig(t, oldClient, 25)
	oldCfg := rc.LLM
	oldCfg.Model = "old-model"
	oldCfg.LoopMaxSteps = 4
	newCfg := oldCfg
	newCfg.Model = "new-model"
	newCfg.LoopMaxSteps = 1
	runtime := &runtimeSnapshotSequence{snapshots: []llm.RuntimeSnapshot{
		{Client: oldClient, Config: oldCfg},
		{Client: newClient, Config: newCfg},
	}}
	rc.Runtime = runtime

	loop := &DelegationClaimLoop{Worker: rc, LeaseDuration: time.Hour}
	job := documents.IngestionJob{
		ID: "11111111-1111-7111-8111-111111111111", IdentityID: "22222222-2222-7222-8222-222222222222", LeaseGeneration: 1,
	}
	payload := DelegationPayload{
		Goal: "runtime swap task", ConversationID: "33333333-3333-7333-8333-333333333333", FanoutKey: "f-test",
	}
	ctx := identityctx.WithIdentityID(context.Background(), job.IdentityID)
	report, _, err := loop.runWithHeartbeat(ctx, job, payload)
	if err != nil {
		t.Fatalf("runWithHeartbeat: %v", err)
	}
	if report.Status != StatusOK || report.Summary != "old profile completed" {
		t.Fatalf("report = %+v, want completion from the original profile", report)
	}
	if got := runtime.callCount(); got != 1 {
		t.Fatalf("runtime Snapshot called %d times, want exactly 1", got)
	}
	if got := oldClient.callCount(); got != 3 {
		t.Fatalf("original client called %d times, want two tool rounds plus completion", got)
	}
}
