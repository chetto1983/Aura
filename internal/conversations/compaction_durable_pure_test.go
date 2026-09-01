package conversations

import (
	"testing"

	"github.com/chetto1983/aura/internal/llm"
)

// TestTurnsAfter tests the pure turnsAfter function
func TestTurnsAfter(t *testing.T) {
	tests := []struct {
		name  string
		turns []Turn
		seq   int
		want  []Turn
	}{
		{
			name:  "empty turns",
			turns: []Turn{},
			seq:   5,
			want:  nil,
		},
		{
			name:  "all turns before seq",
			turns: []Turn{{Seq: 1}, {Seq: 2}, {Seq: 3}},
			seq:   5,
			want:  nil,
		},
		{
			name:  "all turns after seq",
			turns: []Turn{{Seq: 6}, {Seq: 7}, {Seq: 8}},
			seq:   5,
			want:  []Turn{{Seq: 6}, {Seq: 7}, {Seq: 8}},
		},
		{
			name:  "turns starting at seq+1",
			turns: []Turn{{Seq: 1}, {Seq: 2}, {Seq: 6}, {Seq: 7}},
			seq:   5,
			want:  []Turn{{Seq: 6}, {Seq: 7}},
		},
		{
			name:  "turns with exact seq",
			turns: []Turn{{Seq: 1}, {Seq: 5}, {Seq: 6}},
			seq:   5,
			want:  []Turn{{Seq: 6}}, // seq must be >, not >=
		},
		{
			name:  "turns with seq at start",
			turns: []Turn{{Seq: 5}, {Seq: 6}, {Seq: 7}},
			seq:   4,
			want:  []Turn{{Seq: 5}, {Seq: 6}, {Seq: 7}},
		},
		{
			name:  "negative seq",
			turns: []Turn{{Seq: 1}, {Seq: 2}, {Seq: 3}},
			seq:   -1,
			want:  []Turn{{Seq: 1}, {Seq: 2}, {Seq: 3}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := turnsAfter(tt.turns, tt.seq)
			if tt.want == nil {
				if got != nil {
					t.Errorf("turnsAfter() = %v, want nil", got)
				}
				return
			}
			if len(got) != len(tt.want) {
				t.Errorf("turnsAfter() length = %d, want %d", len(got), len(tt.want))
				return
			}
			for i, v := range got {
				if v.Seq != tt.want[i].Seq {
					t.Errorf("turnsAfter()[%d].Seq = %d, want %d", i, v.Seq, tt.want[i].Seq)
				}
			}
		})
	}
}

// TestCarriesPreviousSummary tests the pure carriesPreviousSummary function
func TestCarriesPreviousSummary(t *testing.T) {
	tests := []struct {
		name   string
		rounds []llm.Message
		want   bool
	}{
		{
			name:   "empty rounds",
			rounds: []llm.Message{},
			want:   false,
		},
		{
			name:   "non-empty first message without prefix",
			rounds: []llm.Message{{Content: "hello"}},
			want:   false,
		},
		{
			name:   "first message with prefix",
			rounds: []llm.Message{{Content: carriedSummaryPrompt + "older summary"}},
			want:   true,
		},
		{
			name:   "first message with prefix but not at start",
			rounds: []llm.Message{{Content: "hello " + carriedSummaryPrompt}},
			want:   false,
		},
		{
			name:   "second message has prefix but first doesn't",
			rounds: []llm.Message{{Content: "hello"}, {Content: carriedSummaryPrompt + "summary"}},
			want:   false,
		},
		{
			name:   "first message is exactly the prefix",
			rounds: []llm.Message{{Content: carriedSummaryPrompt}},
			want:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := carriesPreviousSummary(tt.rounds)
			if got != tt.want {
				t.Errorf("carriesPreviousSummary() = %v, want %v", got, tt.want)
			}
		})
	}
}
