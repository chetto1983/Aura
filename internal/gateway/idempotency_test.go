package gateway

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/chetto1983/aura/internal/agent/tools"
	"github.com/chetto1983/aura/internal/config"
	"github.com/chetto1983/aura/internal/idempotency"
	"github.com/chetto1983/aura/internal/identityctx"
)

type fakeOperationRegistry struct {
	decision  idempotency.BeginDecision
	beginErr  error
	begins    []idempotency.BeginRequest
	complete  []idempotency.CompleteRequest
	marked    []idempotency.OperationKey
	markToken []idempotency.ClaimToken
}

func (f *fakeOperationRegistry) Begin(_ context.Context, request idempotency.BeginRequest) (idempotency.BeginDecision, error) {
	f.begins = append(f.begins, request)
	return f.decision, f.beginErr
}

func (f *fakeOperationRegistry) Complete(_ context.Context, request idempotency.CompleteRequest) error {
	f.complete = append(f.complete, request)
	return nil
}

func (f *fakeOperationRegistry) MarkIndeterminate(
	_ context.Context,
	operation idempotency.OperationKey,
	_ [32]byte,
	claimToken idempotency.ClaimToken,
) error {
	f.marked = append(f.marked, operation)
	f.markToken = append(f.markToken, claimToken)
	return nil
}

func gatewayOperationContext(t *testing.T, key string, args json.RawMessage) context.Context {
	t.Helper()
	fingerprint, err := idempotency.FingerprintTyped(struct {
		Tool string          `json:"tool"`
		Args json.RawMessage `json:"args"`
	}{Tool: "skill_manage", Args: args})
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
		{name: "acquired", decision: idempotency.BeginDecision{Decision: idempotency.DecisionAcquired, ClaimToken: 1}, wantAllow: true},
		{name: "completed replay", decision: idempotency.BeginDecision{Decision: idempotency.DecisionReplay, Replay: &idempotency.ReplayResult{Body: replayBody, ExpiresAt: time.Now().Add(time.Hour)}}, wantAllow: true, wantReplay: true},
		{name: "changed payload conflict", decision: idempotency.BeginDecision{Decision: idempotency.DecisionConflict}},
		{name: "fresh in progress", decision: idempotency.BeginDecision{Decision: idempotency.DecisionInProgress, RetryAfter: time.Second}},
		{name: "indeterminate", decision: idempotency.BeginDecision{Decision: idempotency.DecisionIndeterminate}},
		{name: "expired result", decision: idempotency.BeginDecision{Decision: idempotency.DecisionResultExpired}},
		{name: "registry unavailable", beginErr: errors.New("registry down")},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			ledger := &fakeStore{}
			registry := &fakeOperationRegistry{decision: tt.decision, beginErr: tt.beginErr}
			g := New(config.ProfileSingleUserHardened, ledger)
			g.SetOperationRegistry(registry)
			spec := tools.Spec{Name: "skill_manage", Mutating: true, Multiplexed: true, OperationScope: tools.OperationScopeAgent, OperationNormalizer: tools.OperationNormalizerCanonical, ReplayPolicy: tools.ReplayToolResult}
			verdict, err := g.Decide(gatewayOperationContext(t, "public-key", args), spec, args, testKey())
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

func TestGatewayOperationCompletionAndIndeterminateLifecycle(t *testing.T) {
	t.Parallel()

	args := json.RawMessage(`{"action":"restore","name":"calc"}`)
	ctx := gatewayOperationContext(t, "lifecycle-key", args)
	var err error
	ctx, err = idempotency.WithClaimToken(ctx, 7)
	if err != nil {
		t.Fatal(err)
	}
	registry := &fakeOperationRegistry{}
	g := New(config.ProfileSingleUserHardened, nil)
	g.SetOperationRegistry(registry)

	result := tools.ToolResult{
		Preview:  strings.Repeat("p", idempotency.MaxReplayPreviewBytes+10),
		FullPath: strings.Repeat("s", idempotency.MaxSidecarRefBytes+10),
		Bytes:    42,
	}
	if err := g.CompleteOperation(ctx, result); err != nil {
		t.Fatalf("CompleteOperation: %v", err)
	}
	if len(registry.complete) != 1 {
		t.Fatalf("complete calls = %d, want 1", len(registry.complete))
	}
	if registry.complete[0].ClaimToken != 7 {
		t.Fatalf("complete claim token = %d, want 7", registry.complete[0].ClaimToken)
	}
	replay := registry.complete[0].Result
	if len(replay.Preview) != idempotency.MaxReplayPreviewBytes || len(replay.SidecarRef) != idempotency.MaxSidecarRefBytes || len(replay.Body) == 0 {
		t.Fatalf("bounded replay = %+v", replay)
	}
	if err := g.MarkOperationIndeterminate(ctx); err != nil {
		t.Fatalf("MarkOperationIndeterminate: %v", err)
	}
	if len(registry.marked) != 1 || registry.marked[0].Key != "lifecycle-key" ||
		registry.markToken[0] != 7 {
		t.Fatalf("marked operations = %+v", registry.marked)
	}

	if err := (*Gateway)(nil).CompleteOperation(ctx, result); err != nil {
		t.Fatalf("nil gateway complete: %v", err)
	}
	if err := (*Gateway)(nil).MarkOperationIndeterminate(ctx); err != nil {
		t.Fatalf("nil gateway mark: %v", err)
	}
	if err := g.CompleteOperation(context.Background(), result); err == nil {
		t.Fatal("complete accepted a missing trusted operation")
	}
	if err := g.MarkOperationIndeterminate(context.Background()); err == nil {
		t.Fatal("mark accepted a missing trusted operation")
	}
}

func TestGatewayOperationReplayValidationAndBounds(t *testing.T) {
	t.Parallel()

	fallback, err := decodeOperationReplay(&idempotency.ReplayResult{Preview: "safe", SidecarRef: "sidecar", ExpiresAt: time.Now().Add(time.Hour)})
	// D-10: decodeOperationReplay now appends replayedMarker to every successful
	// decode, so the preview contains "safe" plus the marker rather than equaling
	// it verbatim.
	if err != nil || !strings.Contains(fallback.Preview, "safe") || fallback.FullPath != "sidecar" {
		t.Fatalf("fallback replay = %+v, %v", fallback, err)
	}
	for _, replay := range []*idempotency.ReplayResult{
		nil,
		{Body: json.RawMessage(`{not-json`), ExpiresAt: time.Now().Add(time.Hour)},
		{ExpiresAt: time.Now().Add(time.Hour)},
	} {
		if _, err := decodeOperationReplay(replay); err == nil {
			t.Fatalf("decodeOperationReplay(%+v) error = nil", replay)
		}
	}
	if got := boundedString("éé", 3); got != "é" {
		t.Fatalf("boundedString UTF-8 = %q, want one rune", got)
	}
}
