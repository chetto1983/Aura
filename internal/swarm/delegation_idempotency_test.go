package swarm

import (
	"context"
	"testing"

	"github.com/chetto1983/aura/internal/documents"
	"github.com/chetto1983/aura/internal/idempotency"
	"github.com/chetto1983/aura/internal/identityctx"
)

// TestDelegationIdempotencyKey pins delegationIdempotencyKey's determinism:
// the same inputs always produce the same key, and a different goal index
// always produces a different one. Daemon-free (no Postgres) so it always
// contributes to the package coverage floor.
func TestDelegationIdempotencyKey(t *testing.T) {
	a := delegationIdempotencyKey("identity-1", "conv-1", "run-1", 0, "goal text")
	b := delegationIdempotencyKey("identity-1", "conv-1", "run-1", 0, "goal text")
	if a != b {
		t.Fatalf("delegationIdempotencyKey is not deterministic: %q != %q", a, b)
	}

	c := delegationIdempotencyKey("identity-1", "conv-1", "run-1", 1, "goal text")
	if a == c {
		t.Fatalf("delegationIdempotencyKey did not vary with goal index: both = %q", a)
	}

	d := delegationIdempotencyKey("identity-1", "conv-1", "run-1", 0, "a different goal")
	if a == d {
		t.Fatalf("delegationIdempotencyKey did not vary with goal text: both = %q", a)
	}
}

// TestEnqueueDelegationEmptyGoals asserts the D-15 domain-rejection idiom: an
// empty goal slice returns a model-readable string, not a Go error, and never
// touches the store (a nil enqueuer must not be dereferenced on this path).
func TestEnqueueDelegationEmptyGoals(t *testing.T) {
	msg, err := EnqueueDelegation(context.Background(), nil, "identity-1", nil, DelegationPayload{})
	if err != nil {
		t.Fatalf("EnqueueDelegation with no goals returned a Go error: %v", err)
	}
	if msg == "" {
		t.Fatal("EnqueueDelegation with no goals returned an empty message")
	}
	const want = "error: no goals provided"
	if len(msg) < len(want) || msg[:len(want)] != want {
		t.Fatalf("EnqueueDelegation message = %q, want prefix %q", msg, want)
	}
}

// TestDelegationOperationContextMintsTrustedRoot pins the fix for a real
// defect found by driving the live agent (2026-08-27): a claimed delegation
// job's worker denied EVERY mutating tool call with "operation context
// missing" (gateway.beginOperation), because runChild's ctx carried no parent
// operation for deriveToolOperationContext to derive a child from -- the
// claim loop is a trusted root exactly like the HTTP ingress and the
// scheduler, and had never minted one. Also asserts identityctx round-trips,
// and that two different lease generations (a reclaim/retry) mint two
// DIFFERENT operations -- a retried attempt must never replay a dead
// attempt's stale result.
func TestDelegationOperationContextMintsTrustedRoot(t *testing.T) {
	job := documents.IngestionJob{ID: "job-1", IdentityID: "11111111-1111-1111-1111-111111111111", LeaseGeneration: 1}
	payload := DelegationPayload{Goal: "do the thing", ConversationID: "conv-1"}

	ctx, err := delegationOperationContext(context.Background(), job, payload)
	if err != nil {
		t.Fatalf("delegationOperationContext: %v", err)
	}
	op, ok := idempotency.OperationFromContext(ctx)
	if !ok {
		t.Fatal("delegationOperationContext did not mint an operation on ctx")
	}
	if op.Key.Scope != idempotency.ScopeSwarmDelegation {
		t.Fatalf("operation scope = %q, want %q", op.Key.Scope, idempotency.ScopeSwarmDelegation)
	}
	if op.Key.IdentityID != job.IdentityID {
		t.Fatalf("operation identity = %q, want %q", op.Key.IdentityID, job.IdentityID)
	}
	if got := identityctx.IdentityID(ctx); got != job.IdentityID {
		t.Fatalf("identityctx.IdentityID(ctx) = %q, want %q", got, job.IdentityID)
	}

	retried := job
	retried.LeaseGeneration = 2
	retriedCtx, err := delegationOperationContext(context.Background(), retried, payload)
	if err != nil {
		t.Fatalf("delegationOperationContext (retry): %v", err)
	}
	retriedOp, _ := idempotency.OperationFromContext(retriedCtx)
	if retriedOp.Key.Key == op.Key.Key {
		t.Fatal("a reclaimed/retried attempt (different LeaseGeneration) must mint a DIFFERENT operation key")
	}
}
