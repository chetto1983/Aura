package gateway

import (
	"encoding/json"
	"testing"
)

// TestGatewayApprovalsLedgerOneShot proves Approve→Consume is one-shot: the first
// Consume returns the stored ResolvedApproval, the second returns ok=false so a retried
// call re-issues the approval-required result (fail-closed) unless re-approved.
func TestGatewayApprovalsLedgerOneShot(t *testing.T) {
	led := NewGatewayApprovals()
	conv, tool, fp := "conv-1", "swarm_spawn", "fp-abc"
	led.Approve(conv, tool, fp, ResolvedApproval{Approved: true, OperatorID: "op-7"})

	r, ok := led.Consume(conv, tool, fp)
	if !ok || !r.Approved || r.OperatorID != "op-7" {
		t.Fatalf("first Consume = (%+v, %v), want approved op-7 true", r, ok)
	}
	if _, ok := led.Consume(conv, tool, fp); ok {
		t.Fatal("second Consume must return ok=false (one-shot delete-on-consume)")
	}
}

// TestGatewayApprovalsPeekNonDestructive proves Peek reports presence WITHOUT consuming.
func TestGatewayApprovalsPeekNonDestructive(t *testing.T) {
	led := NewGatewayApprovals()
	conv, tool, fp := "conv-1", "skill", "fp-xyz"
	if led.Peek(conv, tool, fp) {
		t.Fatal("Peek on an empty ledger must be false")
	}
	led.Approve(conv, tool, fp, ResolvedApproval{Approved: true})
	if !led.Peek(conv, tool, fp) {
		t.Fatal("Peek must see a recorded approval")
	}
	if !led.Peek(conv, tool, fp) {
		t.Fatal("Peek must be non-destructive (a second Peek still sees it)")
	}
	if _, ok := led.Consume(conv, tool, fp); !ok {
		t.Fatal("Consume after Peek must still find the un-consumed approval")
	}
}

// TestGatewayApprovalsEvictPrefixSweep proves Evict drops every entry under a
// conversation prefix and leaves other conversations untouched (R-41 parity).
func TestGatewayApprovalsEvictPrefixSweep(t *testing.T) {
	led := NewGatewayApprovals()
	led.Approve("conv-A", "swarm_spawn", "fp-1", ResolvedApproval{Approved: true})
	led.Approve("conv-A", "skill", "fp-2", ResolvedApproval{Approved: true})
	led.Approve("conv-B", "swarm_spawn", "fp-1", ResolvedApproval{Approved: true})

	led.Evict("conv-A")

	if led.Peek("conv-A", "swarm_spawn", "fp-1") || led.Peek("conv-A", "skill", "fp-2") {
		t.Fatal("Evict must drop every entry under the conversation prefix")
	}
	if !led.Peek("conv-B", "swarm_spawn", "fp-1") {
		t.Fatal("Evict must not touch a different conversation")
	}
	led.Evict("conv-unknown") // no-op, must not panic
}

// TestGatewayApprovalsNilSafe proves every method is nil-receiver-safe (mirrors
// ShellApprovals: a nil ledger is an inert no-op, never a panic).
func TestGatewayApprovalsNilSafe(t *testing.T) {
	var led *GatewayApprovals
	led.Approve("c", "t", "f", ResolvedApproval{Approved: true})
	if _, ok := led.Consume("c", "t", "f"); ok {
		t.Fatal("nil ledger Consume must be false")
	}
	if led.Peek("c", "t", "f") {
		t.Fatal("nil ledger Peek must be false")
	}
	led.Evict("c") // must not panic
}

// TestGatewayApprovalsEmptyArgsRejected proves an empty coordinate is a no-op (no key
// collides on the empty string; mirrors ShellApprovals' sessionID/digest guards).
func TestGatewayApprovalsEmptyArgsRejected(t *testing.T) {
	led := NewGatewayApprovals()
	led.Approve("", "t", "f", ResolvedApproval{Approved: true})
	led.Approve("c", "", "f", ResolvedApproval{Approved: true})
	led.Approve("c", "t", "", ResolvedApproval{Approved: true})
	if _, ok := led.Consume("", "t", "f"); ok {
		t.Fatal("empty convID must not record")
	}
	if _, ok := led.Consume("c", "", "f"); ok {
		t.Fatal("empty toolName must not record")
	}
	if _, ok := led.Consume("c", "t", ""); ok {
		t.Fatal("empty fingerprint must not record")
	}
}

// TestGatewayArgsFingerprintCanonicalEquality proves the fingerprint absorbs cosmetic
// JSON differences (key order, whitespace) so a model re-emit matches the recorded
// approval, while a SEMANTIC difference yields a distinct fingerprint (T-35-06-02).
func TestGatewayArgsFingerprintCanonicalEquality(t *testing.T) {
	a := json.RawMessage(`{"goals":["build x","test y"],"depth":2}`)
	// Same semantics, different key order + whitespace.
	b := json.RawMessage(`{ "depth": 2, "goals": ["build x", "test y"] }`)
	if gatewayArgsFingerprint(a) != gatewayArgsFingerprint(b) {
		t.Fatal("cosmetically-different but canonical-equal args must share a fingerprint")
	}
	// Semantic difference (a different goal) must NOT reuse the approval.
	c := json.RawMessage(`{"goals":["build x","DROP TABLE"],"depth":2}`)
	if gatewayArgsFingerprint(a) == gatewayArgsFingerprint(c) {
		t.Fatal("semantically-different args must yield a different fingerprint (no approval reuse)")
	}
	// A changed VALUE (not just formatting) also yields a distinct fingerprint.
	e := json.RawMessage(`{"goals":["build x","test y"],"depth":3}`)
	if gatewayArgsFingerprint(a) == gatewayArgsFingerprint(e) {
		t.Fatal("a changed scalar value must yield a different fingerprint")
	}
}
