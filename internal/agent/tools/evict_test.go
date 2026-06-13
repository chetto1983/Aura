package tools

import "testing"

// TestSessionEvictor_TodoTool seeds two sessions' todo state, evicts one, and
// asserts only that session's entry is gone (the other is intact). Evicting an
// unknown session is a no-op (idempotent).
func TestSessionEvictor_TodoTool(t *testing.T) {
	tool := &TodoTool{}
	ctxA := ctxWith(t, "sess-A", "call-A")
	ctxB := ctxWith(t, "sess-B", "call-B")
	seed := mustJSON(t, map[string]any{"todos": []map[string]string{{"content": "x", "status": "pending"}}})
	if _, err := tool.Execute(ctxA, seed); err != nil {
		t.Fatalf("seed A: %v", err)
	}
	if _, err := tool.Execute(ctxB, seed); err != nil {
		t.Fatalf("seed B: %v", err)
	}

	var evictor SessionEvictor = tool
	evictor.Evict("sess-A")
	if _, ok := tool.byID["sess-A"]; ok {
		t.Fatal("sess-A todo state should be evicted")
	}
	if _, ok := tool.byID["sess-B"]; !ok {
		t.Fatal("sess-B todo state must remain after evicting sess-A")
	}

	// Idempotent: evicting an unknown / already-evicted session is a no-op.
	evictor.Evict("sess-A")
	evictor.Evict("does-not-exist")
	if _, ok := tool.byID["sess-B"]; !ok {
		t.Fatal("sess-B todo state must survive no-op evictions")
	}
}

// TestSessionEvictor_ShellExecCwd seeds two sessions' tracked cwd, evicts one,
// and asserts only that session's tracked directory is reclaimed.
func TestSessionEvictor_ShellExecCwd(t *testing.T) {
	s := &ShellExec{}
	ctxA := ctxWith(t, "sess-A", "call-A")
	ctxB := ctxWith(t, "sess-B", "call-B")
	s.storeCwd(ctxA, "/work/a")
	s.storeCwd(ctxB, "/work/b")

	var evictor SessionEvictor = s
	evictor.Evict("sess-A")
	if _, ok := s.cwd["sess-A"]; ok {
		t.Fatal("sess-A tracked cwd should be evicted")
	}
	if got := s.cwd["sess-B"]; got != "/work/b" {
		t.Fatalf("sess-B tracked cwd = %q, want /work/b", got)
	}

	evictor.Evict("unknown") // no-op
	if got := s.cwd["sess-B"]; got != "/work/b" {
		t.Fatalf("sess-B tracked cwd must survive a no-op evict, got %q", got)
	}
}

// TestSessionEvictor_ShellApprovals seeds two sessions' approval ledger entries
// (approved + pending), evicts one, and asserts only that session's entries are
// reclaimed while the other session's approval still consumes.
func TestSessionEvictor_ShellApprovals(t *testing.T) {
	a := NewShellApprovals()
	const digest = "deadbeef"
	a.Approve("sess-A", digest)
	a.Approve("sess-B", digest)
	a.CreateChallenge("sess-A", "rm -rf x", "/work")

	var evictor SessionEvictor = a
	evictor.Evict("sess-A")

	if a.Consume("sess-A", digest) {
		t.Fatal("sess-A approval should be evicted (Consume must return false)")
	}
	if _, ok := a.PendingChallenge("sess-A", ShellApprovalDigest("rm -rf x", "/work")); ok {
		t.Fatal("sess-A pending challenge should be evicted")
	}
	if !a.Consume("sess-B", digest) {
		t.Fatal("sess-B approval must remain consumable after evicting sess-A")
	}

	evictor.Evict("unknown") // no-op, must not panic
}
