package runner

import (
	"context"
	"testing"

	"github.com/chetto1983/aura/internal/identityctx"
)

// TestTurnLiveFollowsTheTrackedTurn: a scheduled report is steered only while a turn of its
// owner runs in that conversation, so a later unrelated turn never receives it a second time.
func TestTurnLiveFollowsTheTrackedTurn(t *testing.T) {
	t.Parallel()
	r := &Runner{}
	const owner, other = "aaaaaaaa-0000-0000-0000-000000000001", "bbbbbbbb-0000-0000-0000-000000000002"
	if r.TurnLive(owner, "conv-1") {
		t.Fatal("TurnLive before any turn = true")
	}
	untrack := r.trackSession(identityctx.WithIdentityID(context.Background(), owner), "conv-1", func() {})
	if !r.TurnLive(owner, "conv-1") {
		t.Fatal("TurnLive during the owner's turn = false")
	}
	if r.TurnLive(other, "conv-1") || r.TurnLive(owner, "conv-2") {
		t.Fatal("TurnLive matched another identity or conversation")
	}
	untrack()
	if r.TurnLive(owner, "conv-1") {
		t.Fatal("TurnLive after the turn ended = true")
	}
}
