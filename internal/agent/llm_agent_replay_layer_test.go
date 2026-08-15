package agent

import (
	"testing"

	"github.com/chetto1983/aura/internal/idempotency"
)

// TestReplayLayerAttributes proves the three-way replay-layer derivation D-10/T-45-10
// needs to stamp the tool.execute span, purely from state gateway.Verdict already
// carries — OperationDecision, and whether Replay is non-nil. No new field is added
// to Verdict to get here (PATTERNS.md §1, decide.go's existing composition).
func TestReplayLayerAttributes(t *testing.T) {
	cases := []struct {
		name         string
		decision     idempotency.Decision
		replay       bool
		wantReplayed bool
		wantLayer    string
	}{
		{
			name:         "operation registry replay (Layer B)",
			decision:     idempotency.DecisionReplay,
			replay:       true,
			wantReplayed: true,
			wantLayer:    "operation",
		},
		{
			name:         "reservation ledger replay (Layer A), operation registry acquired",
			decision:     idempotency.DecisionAcquired,
			replay:       true,
			wantReplayed: true,
			wantLayer:    "reservation",
		},
		{
			name:         "reservation ledger replay (Layer A), no shared registry configured",
			decision:     idempotency.Decision(""),
			replay:       true,
			wantReplayed: true,
			wantLayer:    "reservation",
		},
		{
			name:         "fresh execution — no replay to attribute",
			decision:     idempotency.DecisionAcquired,
			replay:       false,
			wantReplayed: false,
			wantLayer:    "",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			replayed, layer := replayLayerAttributes(tc.decision, tc.replay)
			if replayed != tc.wantReplayed || layer != tc.wantLayer {
				t.Fatalf("replayLayerAttributes(%v, %v) = (%v, %q), want (%v, %q)",
					tc.decision, tc.replay, replayed, layer, tc.wantReplayed, tc.wantLayer)
			}
		})
	}
}
