package swarm

import (
	"context"
	"encoding/json"
	"sync"
	"testing"

	"github.com/chetto1983/aura/internal/agent"
	"github.com/chetto1983/aura/internal/agent/agenttest"
	"github.com/chetto1983/aura/internal/agent/tools"
	"github.com/chetto1983/aura/internal/idempotency"
	"github.com/chetto1983/aura/internal/identityctx"
	"github.com/chetto1983/aura/internal/llm"
	"go.uber.org/goleak"
)

type workerIdentityProbe struct {
	mu         sync.Mutex
	origins    []string
	operations []idempotency.Operation
}

func (p *workerIdentityProbe) Spec() tools.Spec {
	return tools.Spec{
		Name: "worker_identity_probe", Parameters: json.RawMessage(`{"type":"object"}`), Mutating: true,
		OperationScope: tools.OperationScopeAgent, OperationNormalizer: tools.OperationNormalizerCanonical,
		ReplayPolicy: tools.ReplayToolResult,
	}
}

func (p *workerIdentityProbe) Execute(ctx context.Context, _ json.RawMessage) (tools.ToolResult, error) {
	sc, _ := agent.SwarmContext(ctx)
	op, _ := idempotency.OperationFromContext(ctx)
	p.mu.Lock()
	p.origins = append(p.origins, sc.ConvID)
	p.operations = append(p.operations, op)
	p.mu.Unlock()
	return tools.NewResult(ctx, "probe executed")
}

type workerIdentityClient struct{}

func (workerIdentityClient) Stream(_ context.Context, req llm.Request) (<-chan llm.Chunk, error) {
	for _, m := range req.Messages {
		if m.Role == llm.RoleTool {
			return closedChan(llm.Chunk{Text: "complete", FinishReason: "stop"}), nil
		}
	}
	return toolChan(agenttest.MakeToolCall("same-model-call-id", "worker_identity_probe", `{}`)), nil
}

func workerInvocationContext(t *testing.T, key string) context.Context {
	t.Helper()
	const owner = "11111111-1111-1111-1111-111111111111"
	fingerprint, err := idempotency.FingerprintTyped(map[string]string{"goals": "identical"})
	if err != nil {
		t.Fatal(err)
	}
	ctx, err := idempotency.WithOperation(identityctx.WithIdentityID(context.Background(), owner), idempotency.Operation{
		Key:         idempotency.OperationKey{IdentityID: owner, Scope: idempotency.ScopeAgentTool, Key: key},
		Fingerprint: fingerprint,
	})
	if err != nil {
		t.Fatal(err)
	}
	return ctx
}

func TestWorkerDispatchPreservesOriginConversation(t *testing.T) {
	defer goleak.VerifyNone(t)
	rc := testRunConfig(t, workerIdentityClient{}, 20)
	rc.ConvID = "22222222-2222-2222-2222-222222222222"
	rc.ChildID = "w1-coordinator"
	probe := &workerIdentityProbe{}
	rc.ParentRegistry.Register(probe)
	report, _ := runChild(workerInvocationContext(t, "parent"), rc, rc.ParentBudget, 0, "execute probe")
	if report.Status != StatusOK || len(probe.origins) != 1 {
		t.Fatalf("probe did not execute: %+v", report)
	}
	if probe.origins[0] != rc.ConvID {
		t.Fatalf("nested adapter origin = %q, want root UUID %q", probe.origins[0], rc.ConvID)
	}
}

func TestSynchronousWorkersSeparateInvocationsAndMutationRoots(t *testing.T) {
	defer goleak.VerifyNone(t)
	t.Setenv("AURA_SWARM_MAX_DEPTH", "3")
	type observed struct {
		reports    []ChildReport
		operations []idempotency.Operation
	}
	invoke := func(key string) observed {
		t.Helper()
		rc := testRunConfig(t, workerIdentityClient{}, 30)
		rc.Depth = 2
		rc.Cfg.MaxSwarmConcurrent = 1 // stable observation order; same-round sibling intent
		probe := &workerIdentityProbe{}
		rc.ParentRegistry.Register(probe)
		out, err := Run(workerInvocationContext(t, key), rc, []string{"same goal", "same goal"})
		if err != nil {
			t.Fatal(err)
		}
		reports := parseReports(t, out)
		if len(probe.operations) != 2 {
			t.Fatalf("wanted two actual dispatches: %s", out)
		}
		return observed{reports, probe.operations}
	}
	first, retry, fresh := invoke("first"), invoke("first"), invoke("fresh")
	if first.operations[0].Key == first.operations[1].Key {
		t.Error("sibling mutations share an operation root")
	}
	for i := range first.reports {
		if first.reports[i].ChildID != retry.reports[i].ChildID || first.operations[i].Key != retry.operations[i].Key {
			t.Error("same invocation retry changed worker identity or operation")
		}
		if first.reports[i].ChildID == fresh.reports[i].ChildID {
			t.Error("fresh invocation overwrites an earlier worker transcript")
		}
		if first.operations[i].Key == fresh.operations[i].Key {
			t.Error("fresh invocation replays an earlier worker mutation")
		}
	}
}
