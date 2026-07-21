package agent

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/chetto1983/aura/internal/agent/tools"
	"github.com/chetto1983/aura/internal/config"
	"github.com/chetto1983/aura/internal/gateway"
	"github.com/chetto1983/aura/internal/idempotency"
	"github.com/chetto1983/aura/internal/identityctx"
	"github.com/chetto1983/aura/internal/toolinvocations"
)

// fakeReserveStore satisfies the gateway's (unexported) reservationStore seam so the
// agent tier can drive a real gateway.Decide without the live PG ledger: Reserve
// acquires by default (rows==1 → Allow → Execute), and Insert/GetEnd are inert.
type fakeReserveStore struct {
	reserveErr error
}

func (f *fakeReserveStore) Insert(context.Context, toolinvocations.Event) error { return nil }

func (f *fakeReserveStore) Reserve(_ context.Context, _ toolinvocations.Event) (bool, *toolinvocations.Event, error) {
	if f.reserveErr != nil {
		return false, nil, f.reserveErr
	}
	return true, nil, nil
}

func (f *fakeReserveStore) GetEnd(context.Context, string, string, string) (*toolinvocations.Event, error) {
	return nil, nil
}

// spyMutatingTool is a mutating GateRecommended tool (swarm_spawn-shaped) that counts
// Execute calls so a test can assert the mutating side effect is WITHHELD until approval.
type spyMutatingTool struct {
	name  string
	count int
}

func (s *spyMutatingTool) Spec() tools.Spec {
	return tools.Spec{
		Name: s.name, Mutating: true,
		OperationScope: tools.OperationScopeAgent, OperationNormalizer: tools.OperationNormalizerCanonical,
		ReplayPolicy: tools.ReplayToolResult,
	}
}

func (s *spyMutatingTool) Execute(context.Context, json.RawMessage) (tools.ToolResult, error) {
	s.count++
	return tools.ToolResult{Preview: "executed"}, nil
}

// TestExecToolGatewayApprovalWithheldThenReEnters proves the Task-2 contract end-to-end
// at the execTool seam: under single_user_hardened + a live responder, a mutating
// GateRecommended call with NO ledger approval returns a NORMAL approval-required
// ToolResult (no error, Execute count 0) carrying gateway_approval + args_sha256 + a
// descriptive question; then, after RecordResolvedApproval records the operator's accept
// for the SAME args fingerprint, a re-drive Consumes it and Execute runs EXACTLY ONCE.
func TestExecToolGatewayApprovalWithheldThenReEnters(t *testing.T) {
	gw := gateway.New(config.ProfileSingleUserHardened, &fakeReserveStore{})
	a := &LlmAgent{gateway: gw, ledgerConvID: "conv-gw-1"}
	spy := &spyMutatingTool{name: "swarm_spawn"}
	ctx := gateway.WithResponder(context.Background())
	args := json.RawMessage(`{"goals":["build the thing"]}`)

	// (1) Withheld: a normal ToolResult, no error, Execute NOT called.
	res, err := a.execTool(ctx, spy, true, args)
	if err != nil {
		t.Fatalf("withheld approve must return no error, got %v", err)
	}
	if spy.count != 0 {
		t.Fatalf("Execute count = %d, want 0 (mutating action withheld pending approval)", spy.count)
	}
	var preview map[string]any
	if err := json.Unmarshal([]byte(res.Preview), &preview); err != nil {
		t.Fatalf("approval-required preview not json: %v (%q)", err, res.Preview)
	}
	if preview["error"] != "gateway_approval_required" {
		t.Fatalf("preview error = %v, want gateway_approval_required", preview["error"])
	}
	fp, _ := preview["args_sha256"].(string)
	if fp == "" {
		t.Fatal("preview must carry a non-empty args_sha256 the model relays via ask_user")
	}
	if q, _ := preview["question"].(string); q == "" {
		t.Fatal("preview must carry a non-empty descriptive question")
	}

	// (2) Operator accepts (host-side ledger write — the model never writes it). The
	// re-emit with the SAME args Consumes the approval and executes exactly once.
	gw.RecordResolvedApproval("conv-gw-1", "swarm_spawn", fp,
		gateway.ResolvedApproval{Approved: true, OperatorID: "op"})
	res2, err := a.execTool(ctx, spy, true, args)
	if err != nil {
		t.Fatalf("approved re-emit err: %v", err)
	}
	if spy.count != 1 {
		t.Fatalf("Execute count after approval = %d, want 1 (ran exactly once)", spy.count)
	}
	if res2.Preview != "executed" {
		t.Fatalf("approved re-emit preview = %q, want the real tool output", res2.Preview)
	}
}

// TestExecToolGatewayNoApprovalRequiredPaths proves the non-approval arms are unchanged:
// a dev-profile gateway and a read-only tool both dispatch normally (Execute runs, no
// approval-required preview); a hardened+mutating call with NO responder DENIES
// (fail-closed, Execute 0) — the model relaying via ask_user cannot manufacture a responder.
func TestExecToolGatewayNoApprovalRequiredPaths(t *testing.T) {
	args := json.RawMessage(`{"goals":["x"]}`)

	t.Run("dev profile executes", func(t *testing.T) {
		a := &LlmAgent{gateway: gateway.New(config.ProfileDev, &fakeReserveStore{}), ledgerConvID: "c"}
		spy := &spyMutatingTool{name: "swarm_spawn"}
		res, err := a.execTool(gateway.WithResponder(context.Background()), spy, true, args)
		if err != nil || spy.count != 1 {
			t.Fatalf("dev profile: (count=%d, err=%v), want executed once", spy.count, err)
		}
		if res.Preview == "" || containsGatewayApproval(res.Preview) {
			t.Fatalf("dev profile must not return an approval-required preview: %q", res.Preview)
		}
	})

	t.Run("read-only executes", func(t *testing.T) {
		a := &LlmAgent{gateway: gateway.New(config.ProfileSingleUserHardened, &fakeReserveStore{}), ledgerConvID: "c"}
		spy := &spyReadOnlyTool{name: "read_file"}
		_, err := a.execTool(gateway.WithResponder(context.Background()), spy, false, json.RawMessage(`{}`))
		if err != nil || spy.count != 1 {
			t.Fatalf("read-only: (count=%d, err=%v), want executed once", spy.count, err)
		}
	})

	t.Run("no responder denies fail-closed", func(t *testing.T) {
		a := &LlmAgent{gateway: gateway.New(config.ProfileSingleUserHardened, &fakeReserveStore{}), ledgerConvID: "c"}
		spy := &spyMutatingTool{name: "swarm_spawn"}
		_, err := a.execTool(context.Background(), spy, true, args) // no WithResponder
		var denied *gateway.ErrDenied
		if !errors.As(err, &denied) {
			t.Fatalf("err = %v, want *gateway.ErrDenied (fail-closed deny)", err)
		}
		if spy.count != 0 {
			t.Fatalf("Execute count = %d, want 0 (fail-closed deny)", spy.count)
		}
	})
}

// spyReadOnlyTool is a non-mutating tool (classify → Safe, not GateRecommended).
type spyReadOnlyTool struct {
	name  string
	count int
}

func (s *spyReadOnlyTool) Spec() tools.Spec { return tools.Spec{Name: s.name, Mutating: false} }

func (s *spyReadOnlyTool) Execute(context.Context, json.RawMessage) (tools.ToolResult, error) {
	s.count++
	return tools.ToolResult{Preview: "read"}, nil
}

func containsGatewayApproval(preview string) bool {
	var m map[string]any
	if err := json.Unmarshal([]byte(preview), &m); err != nil {
		return false
	}
	return m["error"] == "gateway_approval_required"
}

type replayingOperationRegistry struct {
	begins      []idempotency.BeginRequest
	completed   *idempotency.ReplayResult
	completeErr error
	marked      int
}

func (r *replayingOperationRegistry) Begin(_ context.Context, request idempotency.BeginRequest) (idempotency.BeginDecision, error) {
	r.begins = append(r.begins, request)
	if r.completed != nil {
		return idempotency.BeginDecision{Decision: idempotency.DecisionReplay, Replay: r.completed}, nil
	}
	return idempotency.BeginDecision{Decision: idempotency.DecisionAcquired}, nil
}

func (r *replayingOperationRegistry) Complete(_ context.Context, request idempotency.CompleteRequest) error {
	if r.completeErr != nil {
		return r.completeErr
	}
	result := request.Result
	r.completed = &result
	return nil
}

func (r *replayingOperationRegistry) MarkIndeterminate(context.Context, idempotency.OperationKey, [32]byte) error {
	r.marked++
	return nil
}

func TestExecToolRetryReusesOperationWhileAuditIDsChange(t *testing.T) {
	t.Parallel()

	registry := &replayingOperationRegistry{}
	gw := gateway.New(config.ProfileSingleUserHardened, &fakeReserveStore{})
	gw.SetOperationRegistry(registry)
	a := &LlmAgent{gateway: gw, ledgerConvID: "11111111-1111-1111-1111-111111111111"}
	spy := &spyMutatingTool{name: "skill"}
	args := json.RawMessage(`{"action":"restore","name":"calc"}`)
	fingerprint, err := tools.OperationFingerprint(spy.Spec(), args)
	if err != nil {
		t.Fatal(err)
	}
	op := idempotency.Operation{
		Key: idempotency.OperationKey{
			IdentityID: identityctx.LocalOperatorIdentity,
			Scope:      idempotency.ScopeAgentTool,
			Key:        "stable-public-operation",
		},
		Fingerprint: fingerprint,
	}
	base, err := idempotency.WithOperation(identityctx.WithIdentityID(context.Background(), identityctx.LocalOperatorIdentity), op)
	if err != nil {
		t.Fatal(err)
	}
	firstCtx := tools.WithRequestID(base, "22222222-2222-2222-2222-222222222222")
	firstCtx = tools.WithToolCallContext(firstCtx, "session-1", "call-1", t.TempDir(), 4096)
	first, err := a.execTool(firstCtx, spy, true, args)
	if err != nil {
		t.Fatalf("first exec: %v", err)
	}
	if first.Preview != "executed" || spy.count != 1 {
		t.Fatalf("first result=%+v count=%d", first, spy.count)
	}

	secondCtx := tools.WithRequestID(base, "33333333-3333-3333-3333-333333333333")
	secondCtx = tools.WithToolCallContext(secondCtx, "session-1", "call-2", t.TempDir(), 4096)
	second, err := a.execTool(secondCtx, spy, true, args)
	if err != nil {
		t.Fatalf("replay exec: %v", err)
	}
	if second.Preview != "executed" || spy.count != 1 {
		t.Fatalf("replay result=%+v count=%d, want recorded result and one effect", second, spy.count)
	}
	if len(registry.begins) != 2 {
		t.Fatalf("Begin calls = %d, want 2", len(registry.begins))
	}
	if registry.begins[0].Operation != registry.begins[1].Operation || registry.begins[0].Fingerprint != registry.begins[1].Fingerprint {
		t.Fatal("retry changed public operation identity")
	}
	if registry.begins[0].Audit == nil || registry.begins[1].Audit == nil || registry.begins[0].Audit.RequestID == registry.begins[1].Audit.RequestID || registry.begins[0].Audit.ToolCallID == registry.begins[1].Audit.ToolCallID {
		t.Fatalf("audit IDs did not remain per-attempt: first=%+v second=%+v", registry.begins[0].Audit, registry.begins[1].Audit)
	}
	if registry.completed == nil || registry.completed.ExpiresAt.Before(time.Now()) {
		t.Fatal("first effect did not persist a bounded replay")
	}
}

func TestExecToolCompletionFailureMarksOperationIndeterminate(t *testing.T) {
	t.Parallel()

	registry := &replayingOperationRegistry{completeErr: errors.New("completion write failed")}
	gw := gateway.New(config.ProfileSingleUserHardened, &fakeReserveStore{})
	gw.SetOperationRegistry(registry)
	a := &LlmAgent{gateway: gw, ledgerConvID: "11111111-1111-1111-1111-111111111111"}
	spy := &spyMutatingTool{name: "skill"}
	args := json.RawMessage(`{"action":"restore","name":"calc"}`)
	fingerprint, err := tools.OperationFingerprint(spy.Spec(), args)
	if err != nil {
		t.Fatal(err)
	}
	op := idempotency.Operation{
		Key:         idempotency.OperationKey{IdentityID: identityctx.LocalOperatorIdentity, Scope: idempotency.ScopeAgentTool, Key: "ambiguous-operation"},
		Fingerprint: fingerprint,
	}
	ctx, err := idempotency.WithOperation(identityctx.WithIdentityID(context.Background(), identityctx.LocalOperatorIdentity), op)
	if err != nil {
		t.Fatal(err)
	}
	ctx = tools.WithRequestID(ctx, "22222222-2222-2222-2222-222222222222")
	ctx = tools.WithToolCallContext(ctx, "session-1", "call-1", t.TempDir(), 4096)

	_, err = a.execTool(ctx, spy, true, args)
	if err == nil || !strings.Contains(err.Error(), "completion write failed") {
		t.Fatalf("error = %v, want completion failure", err)
	}
	if spy.count != 1 {
		t.Fatalf("effect calls = %d, want 1", spy.count)
	}
	if registry.marked != 1 {
		t.Fatalf("indeterminate marks = %d, want 1", registry.marked)
	}
}
