package gateway

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/chetto1983/aura/internal/agent/tools"
	"github.com/chetto1983/aura/internal/config"
	"github.com/chetto1983/aura/internal/idempotency"
	"github.com/chetto1983/aura/internal/identityctx"
)

type fakeOperationRegistry struct {
	decision idempotency.BeginDecision
	beginErr error
	begins   []idempotency.BeginRequest
	complete []idempotency.CompleteRequest
	marked   []idempotency.OperationKey
}

func (f *fakeOperationRegistry) Begin(_ context.Context, request idempotency.BeginRequest) (idempotency.BeginDecision, error) {
	f.begins = append(f.begins, request)
	return f.decision, f.beginErr
}

func (f *fakeOperationRegistry) Complete(_ context.Context, request idempotency.CompleteRequest) error {
	f.complete = append(f.complete, request)
	return nil
}

func (f *fakeOperationRegistry) MarkIndeterminate(_ context.Context, operation idempotency.OperationKey, _ [32]byte) error {
	f.marked = append(f.marked, operation)
	return nil
}

func gatewayOperationContext(t *testing.T, key string, args json.RawMessage) context.Context {
	t.Helper()
	fingerprint, err := idempotency.FingerprintTyped(struct {
		Tool string          `json:"tool"`
		Args json.RawMessage `json:"args"`
	}{Tool: "skill", Args: args})
	if err != nil {
		t.Fatal(err)
	}
	op := idempotency.Operation{
		Key: idempotency.OperationKey{
			IdentityID: identityctx.LocalOperatorIdentity,
			Scope:      idempotency.ScopeAgentTool,
			Key:        key,
		},
		Fingerprint: fingerprint,
	}
	ctx, err := idempotency.WithOperation(identityctx.WithIdentityID(context.Background(), identityctx.LocalOperatorIdentity), op)
	if err != nil {
		t.Fatal(err)
	}
	return ctx
}

func TestGatewayIdempotencyDecisionsPrecedePolicyReservation(t *testing.T) {
	t.Parallel()

	args := json.RawMessage(`{"action":"restore","name":"calc"}`)
	replayBody, err := json.Marshal(tools.ToolResult{Preview: "recorded", Bytes: 8})
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name       string
		decision   idempotency.BeginDecision
		beginErr   error
		wantAllow  bool
		wantReplay bool
	}{
		{name: "acquired", decision: idempotency.BeginDecision{Decision: idempotency.DecisionAcquired}, wantAllow: true},
		{name: "completed replay", decision: idempotency.BeginDecision{Decision: idempotency.DecisionReplay, Replay: &idempotency.ReplayResult{Body: replayBody, ExpiresAt: time.Now().Add(time.Hour)}}, wantAllow: true, wantReplay: true},
		{name: "changed payload conflict", decision: idempotency.BeginDecision{Decision: idempotency.DecisionConflict}},
		{name: "fresh in progress", decision: idempotency.BeginDecision{Decision: idempotency.DecisionInProgress, RetryAfter: time.Second}},
		{name: "indeterminate", decision: idempotency.BeginDecision{Decision: idempotency.DecisionIndeterminate}},
		{name: "registry unavailable", beginErr: errors.New("registry down")},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			ledger := &fakeStore{}
			registry := &fakeOperationRegistry{decision: tt.decision, beginErr: tt.beginErr}
			g := New(config.ProfileSingleUserHardened, ledger)
			g.SetOperationRegistry(registry)
			verdict, err := g.Decide(gatewayOperationContext(t, "public-key", args), tools.Spec{Name: "skill", Mutating: true, Multiplexed: true}, args, testKey())
			if err != nil {
				t.Fatalf("Decide: %v", err)
			}
			if got := verdict.Decision == Allow; got != tt.wantAllow {
				t.Fatalf("allow = %v, want %v; verdict=%+v", got, tt.wantAllow, verdict)
			}
			if got := verdict.Replay != nil; got != tt.wantReplay {
				t.Fatalf("replay = %v, want %v; verdict=%+v", got, tt.wantReplay, verdict)
			}
			if len(registry.begins) != 1 {
				t.Fatalf("registry Begin calls = %d, want 1", len(registry.begins))
			}
			if !tt.wantAllow || tt.wantReplay {
				if got := len(ledger.reserves()); got != 0 {
					t.Fatalf("non-acquired decision reached policy reservation %d time(s)", got)
				}
			}
		})
	}
}
