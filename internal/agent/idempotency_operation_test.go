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
// a parent that IS this call's own operation (same scope AND same fingerprint —
// a re-entry), deriveToolOperationContext returns (ctx, nil) UNCHANGED — the round
// is required only on the path that actually derives a child key, never on the
// passthrough path.
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

	// The parent must be THIS CALL's own operation, not merely one sharing its
	// scope. The premise was corrected after spike 099: keying passthrough on scope
	// alone let a swarm worker inherit swarm_spawn's operation and be denied on
	// every dispatch, so the fingerprint now has to match too.
	t.Run("parent is a re-entry of the same call", func(t *testing.T) {
		fingerprint, err := tools.OperationFingerprint(spec, args)
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

// TestDeriveToolOperationContextDerivesForNestedToolCall pins the defect spike 099
// measured live: a swarm worker inherited swarm_spawn's operation verbatim, because
// the passthrough guard keyed on scope alone and swarm_spawn shares
// OperationScopeAgent with the ten tools a worker can call. gateway.beginOperation
// then recomputed the fingerprint for the worker's OWN tool, found swarm_spawn's,
// and denied every dispatch with "operation fingerprint mismatch" — 4/4 workers,
// 100% of calls, deterministic. A nested call is a NEW operation and must derive
// one; only a genuine re-entry of the SAME call may pass through.
func TestDeriveToolOperationContextDerivesForNestedToolCall(t *testing.T) {
	spec := childOperationSpec()
	args := json.RawMessage(`{"action":"restore","name":"calc"}`)
	want, err := tools.OperationFingerprint(spec, args)
	if err != nil {
		t.Fatal(err)
	}

	derived, err := deriveToolOperationContext(swarmParentOperationContextFor(t, `{"goals":["alpha","beta"]}`), spec, args)
	if err != nil {
		t.Fatalf("nested derive: %v", err)
	}
	op, ok := idempotency.OperationFromContext(derived)
	if !ok {
		t.Fatal("nested derive: no operation in context")
	}
	if op.Fingerprint != want {
		t.Fatalf("nested derive carried the PARENT's fingerprint %s, want the worker tool's own %s — the gateway denies this as an operation fingerprint mismatch",
			idempotency.FingerprintHex(op.Fingerprint), idempotency.FingerprintHex(want))
	}
	if op.Key.Scope != spec.OperationScope {
		t.Fatalf("nested derive scope = %q, want %q", op.Key.Scope, spec.OperationScope)
	}
}

// swarmParentOperationContextFor builds the ctx a SWARM WORKER actually runs under:
// the parent agent already derived a child operation for its own swarm_spawn call
// (scope ScopeAgentTool), and that operation is still ambient when the worker
// dispatches a tool of its own. The round is the WORKER's own — llm_agent.go:537
// re-points it onto ic.Ctx before every dispatch, so the derivation path sees it.
// The spawn's goals are the caller's, so two DISTINCT invocations can be compared
// with everything else — identity, scope, the worker's round — held equal.
func swarmParentOperationContextFor(t *testing.T, goals string) context.Context {
	t.Helper()
	swarmSpec := tools.Spec{
		Name: "swarm_spawn", Mutating: true,
		OperationScope: tools.OperationScopeAgent, OperationNormalizer: tools.OperationNormalizerCanonical,
		ReplayPolicy: tools.ReplayToolResult,
	}
	swarmFingerprint, err := tools.OperationFingerprint(swarmSpec, json.RawMessage(goals))
	if err != nil {
		t.Fatal(err)
	}
	parent := idempotency.Operation{
		Key: idempotency.OperationKey{
			IdentityID: identityctx.LocalOperatorIdentity,
			Scope:      tools.OperationScopeAgent,
			Key:        "child:" + idempotency.FingerprintHex(swarmFingerprint),
		},
		Fingerprint: swarmFingerprint,
	}
	ctx, err := idempotency.WithOperation(
		identityctx.WithIdentityID(context.Background(), identityctx.LocalOperatorIdentity), parent,
	)
	if err != nil {
		t.Fatal(err)
	}
	return withModelRound(ctx, modelRound{requestID: uuid.Must(uuid.NewV7()), ordinal: 1})
}

// TestDeriveToolOperationContextScopesNestedCallsToTheirSpawn proves the widened
// parent branch did not trade a denial for a collapse: the derived key carries the
// PARENT's key and fingerprint, so the same worker tool called with the same
// arguments under two different swarm_spawn invocations derives two different keys
// and executes twice. Without this, re-running a fan-out would replay the first
// fan-out's recorded results.
func TestDeriveToolOperationContextScopesNestedCallsToTheirSpawn(t *testing.T) {
	spec := childOperationSpec()
	args := json.RawMessage(`{"action":"restore","name":"calc"}`)

	keyFor := func(goals string) string {
		derived, err := deriveToolOperationContext(swarmParentOperationContextFor(t, goals), spec, args)
		if err != nil {
			t.Fatalf("derive under %s: %v", goals, err)
		}
		op, ok := idempotency.OperationFromContext(derived)
		if !ok {
			t.Fatalf("derive under %s: no operation in context", goals)
		}
		return op.Key.Key
	}

	if first, second := keyFor(`{"goals":["alpha"]}`), keyFor(`{"goals":["beta"]}`); first == second {
		t.Fatalf("two distinct spawns derived the SAME key %s — the second fan-out would replay the first's results", first)
	}
}

// TestSiblingWorkersOfOneSpawnCollapseOnAnIdenticalCall characterizes — it does not
// introduce — the one property the derived key cannot discriminate: worker identity.
// The key is (parent key + parent fingerprint + tool + args + round ordinal) by
// design, because request and tool-call ids are audit-only (a same-round retry must
// collapse, HARN-02). Two siblings of ONE spawn issuing a byte-identical mutating
// call at the same ordinal therefore share a key, and the registry answers the
// second with in-progress or a marked replay rather than mutating twice.
//
// That is the safe outcome, not the obviously right one: sibling workers are given
// DIFFERENT goals, so identical arguments are an edge case. Phase 51's D-10 already
// owns the decision of whether worker identity becomes host-derived and joins this
// key; this test is where that change must announce itself.
func TestSiblingWorkersOfOneSpawnCollapseOnAnIdenticalCall(t *testing.T) {
	spec := childOperationSpec()
	args := json.RawMessage(`{"action":"restore","name":"calc"}`)
	goals := `{"goals":["alpha","beta"]}`

	keyForSibling := func() string {
		derived, err := deriveToolOperationContext(swarmParentOperationContextFor(t, goals), spec, args)
		if err != nil {
			t.Fatalf("sibling derive: %v", err)
		}
		op, ok := idempotency.OperationFromContext(derived)
		if !ok {
			t.Fatal("sibling derive: no operation in context")
		}
		return op.Key.Key
	}

	if first, second := keyForSibling(), keyForSibling(); first != second {
		t.Fatalf("sibling keys diverged (%s vs %s) — worker identity entered the key; update D-10 and this test's rationale together", first, second)
	}
}

// TestDeriveToolOperationContextDerivesForDelegatedDispatch (SWARM-08, plan
// 51-05): 67d24aee4 fixed deriveToolOperationContext to key its re-entry guard on
// scope AND fingerprint, and TestDeriveToolOperationContextDerivesForNestedToolCall
// pins the single-hop case that was measured live (spike 099, 4/4 workers). Plan
// 51-05 opens two dispatch paths that did NOT exist when that fix was measured — a
// nested (worker-issued) swarm_spawn call, and a claim-loop-dispatched worker's own
// tool call — and 51-RESEARCH.md is explicit that lifting the registry restriction
// reopens exactly the fingerprint-collapse defect class if either path ever skips
// derivation. This builds no production code: it extends the SAME regression guard
// to both new paths.
//
// Both subtests assert the DISPATCH derivation itself (the operation a worker's
// tool call actually gets), never registry membership — a membership-only
// assertion would have passed throughout the entire period the shipped defect
// existed (nothing about the registry changed; only the fingerprint comparison
// did).
func TestDeriveToolOperationContextDerivesForDelegatedDispatch(t *testing.T) {
	t.Run("nested worker's own tool call, two delegation hops deep", func(t *testing.T) {
		// depth-1 worker's ambient operation: already a derived child of the
		// top-level swarm_spawn call (swarmParentOperationContextFor's own shape).
		depthOneCtx := swarmParentOperationContextFor(t, `{"goals":["alpha","beta"]}`)

		// The depth-1 worker itself issues a NESTED swarm_spawn call — plan 51-05
		// opens exactly this path (workerRegistry grants swarm_spawn below the
		// depth cap). Derive the depth-2 worker's ambient operation from it.
		nestedSwarmSpec := tools.Spec{
			Name: "swarm_spawn", Mutating: true,
			OperationScope: tools.OperationScopeAgent, OperationNormalizer: tools.OperationNormalizerCanonical,
			ReplayPolicy: tools.ReplayToolResult,
		}
		nestedGoals := json.RawMessage(`{"goals":["gamma"]}`)
		depthTwoCtx, err := deriveToolOperationContext(depthOneCtx, nestedSwarmSpec, nestedGoals)
		if err != nil {
			t.Fatalf("derive nested swarm_spawn operation: %v", err)
		}

		// The depth-2 worker now dispatches its OWN tool — this must derive a key
		// off ITS tool's fingerprint, never inherit the nested swarm_spawn's (the
		// exact shape of the spike 099 defect, one hop further in).
		spec := childOperationSpec()
		args := json.RawMessage(`{"action":"restore","name":"calc"}`)
		want, err := tools.OperationFingerprint(spec, args)
		if err != nil {
			t.Fatal(err)
		}
		derived, err := deriveToolOperationContext(depthTwoCtx, spec, args)
		if err != nil {
			t.Fatalf("depth-2 tool derive: %v", err)
		}
		op, ok := idempotency.OperationFromContext(derived)
		if !ok {
			t.Fatal("depth-2 tool derive: no operation in context")
		}
		if op.Fingerprint != want {
			t.Fatalf("depth-2 worker's tool call carried the NESTED swarm_spawn's fingerprint %s, want its own tool's %s — "+
				"the gateway would deny this as an operation fingerprint mismatch",
				idempotency.FingerprintHex(op.Fingerprint), idempotency.FingerprintHex(want))
		}
		if op.Key.Scope != spec.OperationScope {
			t.Fatalf("depth-2 tool derive scope = %q, want %q", op.Key.Scope, spec.OperationScope)
		}
	})

	t.Run("claim-loop-dispatched worker's own tool call", func(t *testing.T) {
		// Mirrors internal/swarm/delegation_queue.go's delegationOperationContext:
		// the trusted root a background delegation claim loop mints before calling
		// runChild for a claimed job's worker.
		fingerprint, err := idempotency.FingerprintTyped(struct {
			JobID string `json:"job_id"`
			Goal  string `json:"goal"`
		}{JobID: "job-1", Goal: "background goal"})
		if err != nil {
			t.Fatal(err)
		}
		parent := idempotency.Operation{
			Key: idempotency.OperationKey{
				IdentityID: identityctx.LocalOperatorIdentity,
				Scope:      idempotency.ScopeSwarmDelegation,
				Key:        "job-1:0",
			},
			Fingerprint: fingerprint,
			Correlation: "job-1",
		}
		ctx, err := idempotency.WithOperation(
			identityctx.WithIdentityID(context.Background(), identityctx.LocalOperatorIdentity), parent,
		)
		if err != nil {
			t.Fatal(err)
		}
		ctx = withModelRound(ctx, modelRound{requestID: uuid.Must(uuid.NewV7()), ordinal: 1})

		spec := childOperationSpec()
		args := json.RawMessage(`{"action":"restore","name":"calc"}`)
		want, err := tools.OperationFingerprint(spec, args)
		if err != nil {
			t.Fatal(err)
		}
		derived, err := deriveToolOperationContext(ctx, spec, args)
		if err != nil {
			t.Fatalf("claim-loop worker tool derive: %v", err)
		}
		op, ok := idempotency.OperationFromContext(derived)
		if !ok {
			t.Fatal("claim-loop worker tool derive: no operation in context")
		}
		if op.Fingerprint != want {
			t.Fatalf("claim-loop worker's tool call carried the delegation root's fingerprint %s, want its own tool's %s",
				idempotency.FingerprintHex(op.Fingerprint), idempotency.FingerprintHex(want))
		}
		if op.Key.Scope != spec.OperationScope {
			t.Fatalf("claim-loop tool derive scope = %q, want %q", op.Key.Scope, spec.OperationScope)
		}
	})
}
