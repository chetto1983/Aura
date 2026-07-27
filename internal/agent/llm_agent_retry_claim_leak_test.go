package agent

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/chetto1983/aura/internal/agent/tools"
	"github.com/chetto1983/aura/internal/config"
	"github.com/chetto1983/aura/internal/gateway"
	"github.com/chetto1983/aura/internal/idempotency"
	"github.com/chetto1983/aura/internal/identityctx"
)

// cancelSensitiveRegistry records whether the terminal write arrived on a LIVE context. The
// leak these tests pin is invisible to a registry that ignores ctx: the call was made, it
// just never landed, because the caller handed it the very context that had just expired.
type cancelSensitiveRegistry struct {
	markCalls int
	markLive  int // marks that arrived on a context still alive — the ones that persist
}

func (r *cancelSensitiveRegistry) Begin(context.Context, idempotency.BeginRequest) (idempotency.BeginDecision, error) {
	return idempotency.BeginDecision{Decision: idempotency.DecisionAcquired, ClaimToken: 1}, nil
}

func (r *cancelSensitiveRegistry) Complete(context.Context, idempotency.CompleteRequest) error {
	return nil
}

func (r *cancelSensitiveRegistry) MarkIndeterminate(
	ctx context.Context,
	_ idempotency.OperationKey,
	_ [32]byte,
	_ idempotency.ClaimToken,
) error {
	r.markCalls++
	if ctx.Err() == nil {
		r.markLive++
		return nil
	}
	// What a real store does with a dead context: refuses, and the row stays in_progress.
	return ctx.Err()
}

// cancelledTool fails the way an abandoned turn does: the context is gone, so Execute
// returns its error rather than a result.
type cancelledTool struct{ cancel context.CancelFunc }

func (cancelledTool) Spec() tools.Spec {
	return tools.Spec{
		Name: "shell_exec", Mutating: true,
		OperationScope: tools.OperationScopeAgent, OperationNormalizer: tools.OperationNormalizerCanonical,
		ReplayPolicy: tools.ReplayToolResult,
	}
}

func (c cancelledTool) Execute(ctx context.Context, _ json.RawMessage) (tools.ToolResult, error) {
	c.cancel() // the turn is abandoned mid-call: reload, dropped SSE, per-call timeout
	return tools.ToolResult{}, context.Canceled
}

// TestExecToolClosesTheClaimEvenWhenTheTurnIsCancelled pins the fix for a leak measured on a
// live appliance: 17 operation acquisitions produced only 6 recorded outcomes, leaving 11
// claims stuck in_progress. Every identical tool call that followed was denied with
// "operation is in progress" until the lease expired — which is what made `skill` and
// `shell_exec` look broken.
//
// The cause was ordinary and easy to miss: closing the claim used the CALLER's context. An
// abandoned turn cancels that context, Execute fails because of it, and then the write that
// records the failure fails for the same reason. Bookkeeping about work that already
// happened must not be cancellable by whatever cancelled the work.
func TestExecToolClosesTheClaimEvenWhenTheTurnIsCancelled(t *testing.T) {
	t.Parallel()

	registry := &cancelSensitiveRegistry{}
	gw := gateway.New(config.ProfileSingleUserHardened, &fakeReserveStore{})
	gw.SetOperationRegistry(registry)
	a := &LlmAgent{gateway: gw, ledgerConvID: "11111111-1111-1111-1111-111111111111"}

	args := json.RawMessage(`{"command":"echo hi"}`)
	tool := cancelledTool{}
	fingerprint, err := tools.OperationFingerprint(tool.Spec(), args)
	if err != nil {
		t.Fatal(err)
	}
	op := idempotency.Operation{
		Key: idempotency.OperationKey{
			IdentityID: identityctx.LocalOperatorIdentity,
			Scope:      idempotency.ScopeAgentTool,
			Key:        "claim-leak-probe",
		},
		Fingerprint: fingerprint,
	}
	base, err := idempotency.WithOperation(
		identityctx.WithIdentityID(context.Background(), identityctx.LocalOperatorIdentity), op)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(base)
	defer cancel()
	ctx = tools.WithRequestID(ctx, "22222222-2222-2222-2222-222222222222")
	ctx = tools.WithToolCallContext(ctx, "session-1", "call-1", t.TempDir(), 4096)

	_, execErr := a.execTool(ctx, cancelledTool{cancel: cancel}, true, args)
	if !errors.Is(execErr, context.Canceled) {
		t.Fatalf("exec error = %v, want the cancellation surfaced", execErr)
	}
	if registry.markCalls != 1 {
		t.Fatalf("MarkIndeterminate calls = %d, want 1 (the claim must always be closed)", registry.markCalls)
	}
	// The assertion that matters: it arrived on a context that could still write. Before the
	// fix this was 0 — the call was made and refused, and the claim leaked.
	if registry.markLive != 1 {
		t.Fatal("the terminal write ran on the CANCELLED context, so the claim would stay in_progress")
	}
}
