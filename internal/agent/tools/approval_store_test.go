package tools

import (
	"strings"
	"testing"
)

// The store is ready from its zero value, binds an approval to the exact question its
// challenge carried, redeems it once, and keeps one conversation's approvals from another's.
func TestApprovalStoreZeroValueAndBinding(t *testing.T) {
	t.Parallel()
	var a ApprovalStore[string]

	ch := a.CreateChallenge("conv-a", "digest-1", "Approve this?", "data")
	if ch.Digest != "digest-1" || ch.Question != "Approve this?" || ch.Data != "data" {
		t.Fatalf("challenge = %+v", ch)
	}
	if got, ok := a.PendingChallenge("conv-a", "digest-1"); !ok || got != ch {
		t.Fatalf("PendingChallenge = (%+v, %v)", got, ok)
	}
	if _, ok := a.PendingChallenge("conv-b", "digest-1"); ok {
		t.Fatal("another conversation sees the challenge")
	}
	if err := a.ApproveChallenge("conv-b", "digest-1", "Approve this?"); err == nil {
		t.Fatal("another conversation approved the challenge")
	}
	if err := a.ApproveChallenge("conv-a", "digest-1", "Approve that?"); err == nil || !strings.Contains(err.Error(), "question mismatch") {
		t.Fatalf("a relayed question that differs: err = %v", err)
	}
	if a.Consume("conv-a", "digest-1") {
		t.Fatal("a refused approval was recorded")
	}
	if err := a.ApproveChallenge("conv-a", "digest-1", "Approve this?"); err != nil {
		t.Fatalf("ApproveChallenge: %v", err)
	}
	if _, ok := a.PendingChallenge("conv-a", "digest-1"); ok {
		t.Fatal("the challenge stayed pending after its approval")
	}
	if a.Consume("conv-b", "digest-1") {
		t.Fatal("another conversation redeemed the approval")
	}
	if !a.Consume("conv-a", "digest-1") || a.Consume("conv-a", "digest-1") {
		t.Fatal("an approval is redeemed exactly once")
	}

	a.CreateChallenge("conv-a", "digest-2", "q", "")
	a.Approve("conv-a", "digest-3")
	a.CreateChallenge("conv-b", "digest-2", "q", "")
	a.Evict("conv-a")
	if _, ok := a.PendingChallenge("conv-a", "digest-2"); ok || a.Consume("conv-a", "digest-3") {
		t.Fatal("Evict left the conversation's state behind")
	}
	if _, ok := a.PendingChallenge("conv-b", "digest-2"); !ok {
		t.Fatal("Evict removed another conversation's challenge")
	}
	a.DiscardChallenge("conv-b", "digest-2")
	if _, ok := a.PendingChallenge("conv-b", "digest-2"); ok || a.Consume("conv-b", "digest-2") {
		t.Fatal("a discarded challenge stayed or became an approval")
	}
}

// An empty conversation or digest records nothing, and a nil store still hands back the
// challenge so the caller can put it to the operator.
func TestApprovalStoreRefusesEmptyKeysAndANilStore(t *testing.T) {
	t.Parallel()
	var a ApprovalStore[int]
	a.CreateChallenge("", "d", "q", 1)
	a.CreateChallenge("s", "", "q", 1)
	a.Approve("", "d")
	if _, ok := a.PendingChallenge("", "d"); ok || a.Consume("", "d") || a.Consume("s", "") {
		t.Fatal("an empty key was recorded")
	}
	if err := a.ApproveChallenge("s", "", "q"); err == nil {
		t.Fatal("an empty digest was approved")
	}

	var nilStore *ApprovalStore[int]
	if ch := nilStore.CreateChallenge("s", "d", "q", 7); ch.Question != "q" || ch.Data != 7 {
		t.Fatalf("nil store challenge = %+v", ch)
	}
	nilStore.Approve("s", "d")
	nilStore.DiscardChallenge("s", "d")
	nilStore.Evict("s")
	if _, ok := nilStore.PendingChallenge("s", "d"); ok || nilStore.Consume("s", "d") {
		t.Fatal("a nil store recorded something")
	}
	if err := nilStore.ApproveChallenge("s", "d", "q"); err == nil {
		t.Fatal("a nil store approved")
	}
}
