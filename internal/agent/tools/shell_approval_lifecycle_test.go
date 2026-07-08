package tools

import "testing"

// shell_approval_lifecycle_test.go unit-covers the ShellApprovals challenge lifecycle
// (CreateChallenge → PendingChallenge → ApproveChallenge → Consume) and its guard paths — the
// daemon-free destructive-command approval bookkeeping the routed shell_exec gates on.
func TestShellApprovals_Lifecycle(t *testing.T) {
	t.Parallel()
	a := &ShellApprovals{}

	// Guards: a nil-ish key is never pending / approvable / consumable.
	if _, ok := a.PendingChallenge("", "d"); ok {
		t.Fatal("empty session must not resolve a pending challenge")
	}
	if err := a.ApproveChallenge("", "d", "q"); err == nil {
		t.Fatal("approve with empty session must error")
	}
	if a.Consume("sess", "d") {
		t.Fatal("consume with no approval must be false")
	}

	// Create registers a challenge with a stable digest + question.
	ch := a.CreateChallenge("sess-1", "rm -rf /tmp/x", "/work")
	if ch.Digest == "" || ch.Question == "" {
		t.Fatalf("CreateChallenge returned an incomplete challenge: %+v", ch)
	}
	got, ok := a.PendingChallenge("sess-1", ch.Digest)
	if !ok || got.Question != ch.Question {
		t.Fatalf("PendingChallenge = (%+v, %v), want the registered challenge", got, ok)
	}

	// Wrong question and unknown digest are both refused.
	if err := a.ApproveChallenge("sess-1", ch.Digest, "not-the-question"); err == nil {
		t.Fatal("approve with a mismatched question must error")
	}
	if err := a.ApproveChallenge("sess-1", "unknown-digest", ch.Question); err == nil {
		t.Fatal("approve of an unknown digest must error")
	}

	// Correct approval moves it out of pending into approved.
	if err := a.ApproveChallenge("sess-1", ch.Digest, ch.Question); err != nil {
		t.Fatalf("valid ApproveChallenge: %v", err)
	}
	if _, ok := a.PendingChallenge("sess-1", ch.Digest); ok {
		t.Fatal("challenge still pending after approval")
	}

	// Consume is single-use: the first succeeds, a replay fails.
	if !a.Consume("sess-1", ch.Digest) {
		t.Fatal("first Consume of an approved challenge must succeed")
	}
	if a.Consume("sess-1", ch.Digest) {
		t.Fatal("second Consume must fail (approval is single-use)")
	}
}
