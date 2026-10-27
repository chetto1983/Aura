package runner

import (
	"context"
	"slices"
	"testing"

	"github.com/chetto1983/aura/internal/agent/agenttest"
)

// `/compact` condenses the branch the operator names (spec 2026-10-27, D13), under the same
// context config a turn would replay it with.
func TestCompactConversationCompactsTheNamedLeafWithTheTurnConfig(t *testing.T) {
	r, conv, _ := newTestRunner(t, agenttest.NewFakeClient())

	result, err := r.CompactConversation(context.Background(), "conv-1", 6)
	if err != nil {
		t.Fatalf("CompactConversation: %v", err)
	}
	if result.CoversThroughSeq != 4 {
		t.Fatalf("result = %+v, want the store's result", result)
	}
	if want := []compactCall{{id: "conv-1", leaf: 6}}; !slices.Equal(conv.compacted, want) {
		t.Fatalf("compacted = %+v, want %+v", conv.compacted, want)
	}
	if got := conv.lastCfg.ContextWindow; got != 1000000 {
		t.Fatalf("ContextWindow = %d, want the turn config's 1000000", got)
	}
}
