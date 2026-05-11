package agentloop

import (
	"reflect"
	"strings"
	"testing"

	"github.com/aura/aura/internal/llm"
)

// TestGovernanceInputPurity asserts the documented "all four functions are
// pure" invariant in governance.go: mutating the input slice after the call
// must not change the returned slice. Catches a regression where a future
// change to llm.Message (pointer field, embedded map) silently breaks the
// slice-of-values assumption (F-021).
func TestGovernanceInputPurity(t *testing.T) {
	build := func() []llm.Message {
		return []llm.Message{
			{Role: "user", Content: "hi"},
			{Role: "assistant", ToolCalls: []llm.ToolCall{{ID: "call-1", Name: "read_file"}}},
			{Role: "tool", ToolCallID: "call-1", Content: strings.Repeat("x", 9000)},
			{Role: "tool", ToolCallID: "ghost", Content: "leaked"},
		}
	}
	cases := []struct {
		name string
		fn   func([]llm.Message) []llm.Message
	}{
		{"drop_orphan", func(in []llm.Message) []llm.Message { return dropOrphanToolResults(in) }},
		{"backfill_missing", func(in []llm.Message) []llm.Message { return backfillMissingToolResults(in) }},
		{"microcompact", func(in []llm.Message) []llm.Message { return microcompactToolResults(in, 1, 100) }},
		{"truncate", func(in []llm.Message) []llm.Message { return truncateOversizedToolResults(in, 100) }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			in := build()
			snapshot := append([]llm.Message(nil), in...)
			_ = tc.fn(in)
			if !reflect.DeepEqual(in, snapshot) {
				t.Fatalf("%s mutated its input", tc.name)
			}
		})
	}
}

func TestDropOrphanToolResultsRemovesUnmatchedToolMessages(t *testing.T) {
	in := []llm.Message{
		{Role: "user", Content: "hi"},
		{Role: "assistant", ToolCalls: []llm.ToolCall{{ID: "call-1", Name: "read_file"}}},
		{Role: "tool", ToolCallID: "call-1", Content: "ok"},
		{Role: "tool", ToolCallID: "ghost", Content: "leaked"}, // orphan
	}
	out := dropOrphanToolResults(in)
	if len(out) != 3 {
		t.Fatalf("len(out) = %d, want 3", len(out))
	}
	for _, msg := range out {
		if msg.ToolCallID == "ghost" {
			t.Fatalf("orphan survived: %+v", msg)
		}
	}
}

func TestDropOrphanToolResultsReturnsInputWhenAllMatched(t *testing.T) {
	in := []llm.Message{
		{Role: "user", Content: "hi"},
		{Role: "assistant", ToolCalls: []llm.ToolCall{{ID: "call-1"}}},
		{Role: "tool", ToolCallID: "call-1"},
	}
	out := dropOrphanToolResults(in)
	if &out[0] != &in[0] {
		// Returning the same backing slice avoids an allocation on the
		// hot path. The test pins this expectation so we notice if the
		// implementation grows an unconditional copy.
		t.Fatalf("expected same backing slice when nothing to drop")
	}
}

func TestBackfillMissingToolResultsInsertsPlaceholderForOrphanedCall(t *testing.T) {
	in := []llm.Message{
		{Role: "user", Content: "hi"},
		{Role: "assistant", ToolCalls: []llm.ToolCall{
			{ID: "a", Name: "read_file"},
			{ID: "b", Name: "execute_code"},
		}},
		{Role: "tool", ToolCallID: "a", Content: "ok"},
		// b missing
	}
	out := backfillMissingToolResults(in)
	if len(out) != 4 {
		t.Fatalf("len(out) = %d, want 4", len(out))
	}
	var stub llm.Message
	for _, msg := range out {
		if msg.Role == "tool" && msg.ToolCallID == "b" {
			stub = msg
		}
	}
	if stub.ToolCallID == "" {
		t.Fatal("backfilled tool message for id=b missing")
	}
	if !strings.Contains(stub.Content, "interrupted") {
		t.Fatalf("stub content = %q, want interrupted marker", stub.Content)
	}
}

func TestMicrocompactToolResultsReplacesStaleResultsBeyondKeepRecent(t *testing.T) {
	// 12 read_file results in a row. Keep 10, compact 2.
	msgs := []llm.Message{{Role: "user", Content: "start"}}
	for i := 0; i < 12; i++ {
		callID := "call-" + string(rune('a'+i))
		msgs = append(msgs,
			llm.Message{Role: "assistant", ToolCalls: []llm.ToolCall{{ID: callID, Name: "read_file"}}},
			llm.Message{Role: "tool", ToolCallID: callID, Content: strings.Repeat("x", 600)},
		)
	}
	out := microcompactToolResults(msgs, 10, 500)
	stubCount := 0
	verbatimCount := 0
	for _, msg := range out {
		if msg.Role != "tool" {
			continue
		}
		if strings.Contains(msg.Content, "result omitted from context") {
			stubCount++
		} else {
			verbatimCount++
		}
	}
	if stubCount != 2 {
		t.Fatalf("stubCount = %d, want 2", stubCount)
	}
	if verbatimCount != 10 {
		t.Fatalf("verbatimCount = %d, want 10", verbatimCount)
	}
}

func TestMicrocompactToolResultsSkipsShortResults(t *testing.T) {
	msgs := []llm.Message{{Role: "user", Content: "start"}}
	for i := 0; i < 12; i++ {
		callID := "call-" + string(rune('a'+i))
		msgs = append(msgs,
			llm.Message{Role: "assistant", ToolCalls: []llm.ToolCall{{ID: callID, Name: "read_file"}}},
			llm.Message{Role: "tool", ToolCallID: callID, Content: "ok"}, // way below minChars
		)
	}
	out := microcompactToolResults(msgs, 10, 500)
	for _, msg := range out {
		if msg.Role == "tool" && strings.Contains(msg.Content, "result omitted") {
			t.Fatalf("short result was compacted: %+v", msg)
		}
	}
}

func TestMicrocompactToolResultsIgnoresNonCompactableTools(t *testing.T) {
	msgs := []llm.Message{{Role: "user", Content: "start"}}
	for i := 0; i < 12; i++ {
		callID := "call-" + string(rune('a'+i))
		msgs = append(msgs,
			llm.Message{Role: "assistant", ToolCalls: []llm.ToolCall{{ID: callID, Name: "write_file"}}},
			llm.Message{Role: "tool", ToolCallID: callID, Content: strings.Repeat("x", 600)},
		)
	}
	out := microcompactToolResults(msgs, 10, 500)
	for _, msg := range out {
		if msg.Role == "tool" && strings.Contains(msg.Content, "result omitted") {
			t.Fatalf("write_file (non-compactable) was compacted: %+v", msg)
		}
	}
}

func TestTruncateOversizedToolResultsCapsAtMaxChars(t *testing.T) {
	big := strings.Repeat("y", 10_000)
	in := []llm.Message{
		{Role: "assistant", ToolCalls: []llm.ToolCall{{ID: "c", Name: "read_file"}}},
		{Role: "tool", ToolCallID: "c", Content: big},
	}
	out := truncateOversizedToolResults(in, 1000)
	if len(out[1].Content) > 1000 {
		t.Fatalf("oversized result not truncated: %d bytes", len(out[1].Content))
	}
	if !strings.HasSuffix(out[1].Content, truncationMarker) {
		t.Fatalf("truncation marker missing: %q", out[1].Content[len(out[1].Content)-50:])
	}
}

func TestTruncateOversizedToolResultsLeavesSmallResultsAlone(t *testing.T) {
	in := []llm.Message{
		{Role: "assistant", ToolCalls: []llm.ToolCall{{ID: "c", Name: "read_file"}}},
		{Role: "tool", ToolCallID: "c", Content: "small"},
	}
	out := truncateOversizedToolResults(in, 1000)
	if out[1].Content != "small" {
		t.Fatalf("small result mutated: %q", out[1].Content)
	}
}

func TestApplyGovernanceChainsAllTransforms(t *testing.T) {
	// Build a history that exercises every transform:
	// - 12 read_file pairs (microcompact will trim 2)
	// - One assistant with a missing tool result (backfill)
	// - One orphan tool message (drop)
	msgs := []llm.Message{{Role: "user", Content: "start"}}
	for i := 0; i < 12; i++ {
		callID := "call-" + string(rune('a'+i))
		msgs = append(msgs,
			llm.Message{Role: "assistant", ToolCalls: []llm.ToolCall{{ID: callID, Name: "read_file"}}},
			llm.Message{Role: "tool", ToolCallID: callID, Content: strings.Repeat("x", 700)},
		)
	}
	msgs = append(msgs,
		llm.Message{Role: "assistant", ToolCalls: []llm.ToolCall{{ID: "missing", Name: "execute_code"}}},
		llm.Message{Role: "tool", ToolCallID: "ghost", Content: "i should be dropped"},
	)

	out := applyGovernance(msgs, 500, 0, 0)

	// Orphan dropped.
	for _, msg := range out {
		if msg.Role == "tool" && msg.ToolCallID == "ghost" {
			t.Fatal("orphan survived applyGovernance")
		}
	}
	// Backfill inserted for "missing".
	var backfilled bool
	for _, msg := range out {
		if msg.Role == "tool" && msg.ToolCallID == "missing" {
			backfilled = true
		}
	}
	if !backfilled {
		t.Fatal("missing tool_call_id was not backfilled")
	}
	// Microcompact + truncate: the two oldest read_file results should be
	// either stubbed or below the 500-char cap.
	for _, msg := range out {
		if msg.Role != "tool" {
			continue
		}
		if len(msg.Content) > 500 {
			t.Fatalf("result above maxChars survived: %d bytes", len(msg.Content))
		}
	}
}
