package agent

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/chetto1983/aura/internal/agent/tools"
	"github.com/chetto1983/aura/internal/idempotency"
	"github.com/chetto1983/aura/internal/identityctx"
	"github.com/google/uuid"
)

// childOperationSpec is a mutating tool spec with a complete operation-metadata
// triple (OperationScope != idempotency.ScopeHTTPMutation, so a parent operation
// minted under ScopeHTTPMutation always takes the child-derivation branch below).
func childOperationSpec() tools.Spec {
	return tools.Spec{
		Name: "skill_manage", Mutating: true,
		OperationScope: tools.OperationScopeAgent, OperationNormalizer: tools.OperationNormalizerCanonical,
		ReplayPolicy: tools.ReplayToolResult,
	}
}

// httpMutationParentContext builds a ctx carrying a DETERMINISTIC parent operation
// (fixed key + fixed fingerprint) whose scope (ScopeHTTPMutation) differs from the
// tool's own scope (ScopeAgentTool) — the exact shape deriveToolOperationContext
// takes the child-derivation branch for (idempotency_operation.go:22-27), mirroring
// TestExecToolDerivesStableChildFromHTTPMutation in llm_agent_retry_gateway_test.go.
// It carries NO round — callers thread one on via withModelRound as needed.
func httpMutationParentContext(t *testing.T) context.Context {
	t.Helper()
	parentFingerprint, err := idempotency.FingerprintTyped(struct {
		Route string `json:"route"`
	}{Route: "agent_run"})
	if err != nil {
		t.Fatal(err)
	}
	parent := idempotency.Operation{
		Key: idempotency.OperationKey{
			IdentityID: identityctx.LocalOperatorIdentity,
			Scope:      idempotency.ScopeHTTPMutation,
			Key:        "public-agent-run-key",
		},
		Fingerprint: parentFingerprint,
	}
	ctx, err := idempotency.WithOperation(
		identityctx.WithIdentityID(context.Background(), identityctx.LocalOperatorIdentity), parent,
	)
	if err != nil {
		t.Fatal(err)
	}
	return ctx
}

// TestDeriveToolOperationContextDiscriminatesRounds proves D-01/PE-02: two
// derivations sharing everything except modelRound.ordinal produce DIFFERENT
// idempotency.OperationKey.Key values — a deliberate cross-round re-issue must
// derive a fresh key so it executes again instead of replaying the first round's
// recorded result (HARN-01, SC#1). It also proves PE-03: the key stays exactly
// "child:" + 64 hex chars = 70 bytes, well inside idempotency.MaxOperationKeyBytes.
func TestDeriveToolOperationContextDiscriminatesRounds(t *testing.T) {
	spec := childOperationSpec()
	args := json.RawMessage(`{"action":"restore","name":"calc"}`)
	requestID := uuid.New()

	ctx1 := withModelRound(httpMutationParentContext(t), modelRound{requestID: requestID, ordinal: 1})
	ctx2 := withModelRound(httpMutationParentContext(t), modelRound{requestID: requestID, ordinal: 2})

	derived1, err := deriveToolOperationContext(ctx1, spec, args)
	if err != nil {
		t.Fatalf("round 1 derive: %v", err)
	}
	derived2, err := deriveToolOperationContext(ctx2, spec, args)
	if err != nil {
		t.Fatalf("round 2 derive: %v", err)
	}

	op1, ok := idempotency.OperationFromContext(derived1)
	if !ok {
		t.Fatal("round 1: no derived operation in context")
	}
	op2, ok := idempotency.OperationFromContext(derived2)
	if !ok {
		t.Fatal("round 2: no derived operation in context")
	}
	if op1.Key.Key == op2.Key.Key {
		t.Fatalf("cross-round keys collided: round1=%q round2=%q — a re-issue in a later round would replay instead of executing", op1.Key.Key, op2.Key.Key)
	}

	const wantKeyBytes = len("child:") + 64 // sha256 hex digest
	if len(op1.Key.Key) != wantKeyBytes || len(op2.Key.Key) != wantKeyBytes {
		t.Fatalf("child key length = %d/%d, want %d bytes (\"child:\" + 64 hex chars)", len(op1.Key.Key), len(op2.Key.Key), wantKeyBytes)
	}
	if len(op1.Key.Key) > idempotency.MaxOperationKeyBytes {
		t.Fatalf("child key length %d exceeds MaxOperationKeyBytes %d", len(op1.Key.Key), idempotency.MaxOperationKeyBytes)
	}
}

// TestDeriveToolOperationContextIsStableWithinARound proves the paired half: two
// derivations at the SAME round with identical parent/spec/args produce the SAME
// key — a genuine same-round retry (or a scheduler reclaim restarting the ordinal
// at 1 against the same stable parent) must still collapse onto one execution
// (HARN-02, SC#2).
func TestDeriveToolOperationContextIsStableWithinARound(t *testing.T) {
	spec := childOperationSpec()
	args := json.RawMessage(`{"action":"restore","name":"calc"}`)
	round := modelRound{requestID: uuid.New(), ordinal: 3}

	ctxA := withModelRound(httpMutationParentContext(t), round)
	ctxB := withModelRound(httpMutationParentContext(t), round)

	derivedA, err := deriveToolOperationContext(ctxA, spec, args)
	if err != nil {
		t.Fatalf("derive A: %v", err)
	}
	derivedB, err := deriveToolOperationContext(ctxB, spec, args)
	if err != nil {
		t.Fatalf("derive B: %v", err)
	}
	opA, ok := idempotency.OperationFromContext(derivedA)
	if !ok {
		t.Fatal("derivation A: no operation in context")
	}
	opB, ok := idempotency.OperationFromContext(derivedB)
	if !ok {
		t.Fatal("derivation B: no operation in context")
	}
	if opA.Key.Key != opB.Key.Key {
		t.Fatalf("same-round derivations diverged: A=%q B=%q", opA.Key.Key, opB.Key.Key)
	}
}

// TestDeriveToolOperationContextFailsClosedWithoutRound proves D-04/PE-04: a
// derivation with a parent operation of a DIFFERENT scope but NO modelRound on ctx
// returns (nil, err) with err matching the package sentinel errMissingModelRound —
// never a silent fallback to ordinal 0, which would restore today's collapsing
// behaviour under a passing test suite.
func TestDeriveToolOperationContextFailsClosedWithoutRound(t *testing.T) {
	spec := childOperationSpec()
	args := json.RawMessage(`{"action":"restore","name":"calc"}`)

	ctx := httpMutationParentContext(t) // different scope parent, NO round set

	derived, err := deriveToolOperationContext(ctx, spec, args)
	if derived != nil {
		t.Fatalf("fail-closed derivation must return a nil context, got %v", derived)
	}
	if !errors.Is(err, errMissingModelRound) {
		t.Fatalf("err = %v, want errMissingModelRound", err)
	}
}

// TestDeriveToolOperationContextPassesThroughWithoutParent proves PE-01 (backstop
// promoted to an explicit assertion here): with no parent operation in context, or
// a parent whose scope already equals the tool's own scope, deriveToolOperationContext
// returns (ctx, nil) UNCHANGED — the round is required only on the path that
// actually derives a child key, never on the passthrough path.
func TestDeriveToolOperationContextPassesThroughWithoutParent(t *testing.T) {
	spec := childOperationSpec()
	args := json.RawMessage(`{"action":"restore","name":"calc"}`)

	t.Run("no parent operation in context", func(t *testing.T) {
		ctx := context.Background() // no parent, no round — must not matter
		derived, err := deriveToolOperationContext(ctx, spec, args)
		if err != nil {
			t.Fatalf("no-parent derive: %v", err)
		}
		if derived != ctx {
			t.Fatal("no-parent derive must return the SAME ctx unchanged")
		}
	})

	t.Run("parent scope equals tool scope", func(t *testing.T) {
		fingerprint, err := idempotency.FingerprintTyped(struct {
			Tool string `json:"tool"`
		}{Tool: spec.Name})
		if err != nil {
			t.Fatal(err)
		}
		parent := idempotency.Operation{
			Key: idempotency.OperationKey{
				IdentityID: identityctx.LocalOperatorIdentity,
				Scope:      spec.OperationScope, // same scope as the tool
				Key:        "same-scope-key",
			},
			Fingerprint: fingerprint,
		}
		ctx, err := idempotency.WithOperation(
			identityctx.WithIdentityID(context.Background(), identityctx.LocalOperatorIdentity), parent,
		) // NO round set — must not be required on this path
		if err != nil {
			t.Fatal(err)
		}
		derived, derr := deriveToolOperationContext(ctx, spec, args)
		if derr != nil {
			t.Fatalf("same-scope derive: %v", derr)
		}
		if derived != ctx {
			t.Fatal("same-scope derive must return the SAME ctx unchanged")
		}
	})
}
