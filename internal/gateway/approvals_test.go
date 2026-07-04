package gateway

import (
	"encoding/json"
	"strings"
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

// TestGatewayApprovalsChallengeApproveConsume proves the shell-parity happy path (CR-01):
// a server-recorded Challenge + a question-MATCHED ApproveChallenge moves the entry to
// approved, which Consume then yields exactly once (one-shot). This is the cooperative
// round-trip in miniature — the operator was shown the SAME question the gateway generated.
func TestGatewayApprovalsChallengeApproveConsume(t *testing.T) {
	led := NewGatewayApprovals()
	conv, tool, fp, q := "conv-1", "swarm_spawn", "fp-abc", "Approve swarm_spawn (risk=risky)? args: goals"
	led.Challenge(conv, tool, fp, q)

	if err := led.ApproveChallenge(conv, tool, fp, q, ResolvedApproval{Approved: true, OperatorID: "op-7"}); err != nil {
		t.Fatalf("ApproveChallenge(matching question): %v", err)
	}
	r, ok := led.Consume(conv, tool, fp)
	if !ok || !r.Approved || r.OperatorID != "op-7" {
		t.Fatalf("Consume = (%+v, %v), want approved op-7 true", r, ok)
	}
	if _, ok := led.Consume(conv, tool, fp); ok {
		t.Fatal("second Consume must return ok=false (one-shot)")
	}
}

// TestGatewayApprovalsApproveClearsSameKeyPendingChallenge proves Approve clears a same-key
// pending challenge (shell parity — ShellApprovals.Approve deletes pending). Without the
// clear a stale challenge would survive a direct Approve, so a later question-matched
// ApproveChallenge could re-approve the key. Approve is a test-seed seam today (production
// records via ApproveChallenge), so this guards the invariant against a future production reuse.
func TestGatewayApprovalsApproveClearsSameKeyPendingChallenge(t *testing.T) {
	led := NewGatewayApprovals()
	conv, tool, fp, q := "conv-1", "swarm_spawn", "fp-abc", "Approve swarm_spawn (risk=risky)? args: goals"
	led.Challenge(conv, tool, fp, q)
	led.Approve(conv, tool, fp, ResolvedApproval{Approved: true, OperatorID: "op-7"})

	// The pending challenge must be gone: a question-matched ApproveChallenge now finds
	// nothing (Approve cleared it), so it cannot re-approve the key.
	if err := led.ApproveChallenge(conv, tool, fp, q, ResolvedApproval{Approved: true}); err == nil {
		t.Fatal("Approve must clear the same-key pending challenge (shell parity)")
	}
	// The direct Approve is still consumable exactly once.
	if _, ok := led.Consume(conv, tool, fp); !ok {
		t.Fatal("the direct Approve must still be consumable after clearing the challenge")
	}
}

// TestGatewayApprovalsApproveChallengeQuestionMismatch is the CR-01/WR-03 ledger-half core:
// a recorded challenge with the REAL question cannot be approved by a DIFFERENT (benign,
// model-relayed) question — ApproveChallenge returns a question-mismatch error and records
// NOTHING, so a subsequent Consume finds no approval. The confused-deputy relay is refused.
func TestGatewayApprovalsApproveChallengeQuestionMismatch(t *testing.T) {
	led := NewGatewayApprovals()
	conv, tool, fp := "conv-1", "swarm_spawn", "fp-abc"
	led.Challenge(conv, tool, fp, "Approve swarm_spawn (risk=risky)? args: goals")

	err := led.ApproveChallenge(conv, tool, fp, "Save your meeting notes?", ResolvedApproval{Approved: true})
	if err == nil {
		t.Fatal("a mismatched/benign question must be rejected")
	}
	if !strings.Contains(err.Error(), "question mismatch") {
		t.Fatalf("error = %v, want a question-mismatch error", err)
	}
	if _, ok := led.Consume(conv, tool, fp); ok {
		t.Fatal("a mismatched-question ApproveChallenge must record nothing (fail-closed)")
	}
}

// TestGatewayApprovalsApproveChallengeNotFound proves ApproveChallenge with NO prior
// Challenge is refused (existence check) — the model cannot fabricate an approval for a
// (conv, tool, fp) the gateway never issued a challenge for, even with the correct fp.
func TestGatewayApprovalsApproveChallengeNotFound(t *testing.T) {
	led := NewGatewayApprovals()
	err := led.ApproveChallenge("conv-1", "swarm_spawn", "fp-none", "any", ResolvedApproval{Approved: true})
	if err == nil {
		t.Fatal("ApproveChallenge with no prior challenge must return an error")
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Fatalf("error = %v, want a not-found error", err)
	}
	if _, ok := led.Consume("conv-1", "swarm_spawn", "fp-none"); ok {
		t.Fatal("no approval must be recorded when the challenge is absent")
	}
}

// TestGatewayApprovalsEvictPrefixSweep proves Evict drops every entry under a conversation
// prefix across BOTH maps — a resolved-but-unconsumed approval AND an issued-but-unresolved
// pending challenge — and leaves other conversations untouched (R-41 / shell parity).
func TestGatewayApprovalsEvictPrefixSweep(t *testing.T) {
	led := NewGatewayApprovals()
	led.Approve("conv-A", "swarm_spawn", "fp-1", ResolvedApproval{Approved: true}) // approved map
	led.Challenge("conv-A", "skill", "fp-2", "Q-A")                                // pending map
	led.Approve("conv-B", "swarm_spawn", "fp-1", ResolvedApproval{Approved: true}) // must survive

	led.Evict("conv-A")

	if _, ok := led.Consume("conv-A", "swarm_spawn", "fp-1"); ok {
		t.Fatal("Evict must drop the resolved approval under the conversation prefix")
	}
	if err := led.ApproveChallenge("conv-A", "skill", "fp-2", "Q-A", ResolvedApproval{Approved: true}); err == nil {
		t.Fatal("Evict must drop the pending challenge under the conversation prefix")
	}
	if _, ok := led.Consume("conv-B", "swarm_spawn", "fp-1"); !ok {
		t.Fatal("Evict must not touch a different conversation")
	}
	led.Evict("conv-unknown") // no-op, must not panic
}

// TestGatewayApprovalsNilSafe proves every method is nil-receiver-safe (mirrors
// ShellApprovals: a nil ledger is an inert no-op, never a panic; ApproveChallenge on a nil
// ledger returns a not-found error rather than recording).
func TestGatewayApprovalsNilSafe(t *testing.T) {
	var led *GatewayApprovals
	led.Approve("c", "t", "f", ResolvedApproval{Approved: true})
	if _, ok := led.Consume("c", "t", "f"); ok {
		t.Fatal("nil ledger Consume must be false")
	}
	led.Challenge("c", "t", "f", "q") // must not panic
	if err := led.ApproveChallenge("c", "t", "f", "q", ResolvedApproval{Approved: true}); err == nil {
		t.Fatal("nil ledger ApproveChallenge must return an error (challenge not found)")
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
	// An empty coordinate must not record a challenge either (guard parity with Approve).
	led.Challenge("", "t", "f", "q")
	if err := led.ApproveChallenge("", "t", "f", "q", ResolvedApproval{Approved: true}); err == nil {
		t.Fatal("empty convID ApproveChallenge must error (no challenge recorded)")
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
