package swarm

import (
	"context"
	"testing"
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
