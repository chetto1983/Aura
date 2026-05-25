package governance

import (
	"reflect"
	"strings"
	"testing"

	"github.com/aura/aura/internal/ctxmetrics"
	"github.com/aura/aura/internal/llm"
)

func TestGovernanceInputPurity(t *testing.T) {
	build := func() []llm.Message {
		return []llm.Message{
			{Role: "user", Content: "hi"},
			{Role: "assistant", ToolCalls: []llm.ToolCall{{ID: "call-1", Name: "file"}}},
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
		{Role: "assistant", ToolCalls: []llm.ToolCall{{ID: "call-1", Name: "file"}}},
		{Role: "tool", ToolCallID: "call-1", Content: "ok"},
		{Role: "tool", ToolCallID: "ghost", Content: "leaked"},
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
		t.Fatalf("expected same backing slice when nothing to drop")
	}
}

func TestBackfillMissingToolResultsInsertsPlaceholderForOrphanedCall(t *testing.T) {
	in := []llm.Message{
		{Role: "user", Content: "hi"},
		{Role: "assistant", ToolCalls: []llm.ToolCall{
			{ID: "a", Name: "file"},
			{ID: "b", Name: "execute_code"},
		}},
		{Role: "tool", ToolCallID: "a", Content: "ok"},
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

func appendSequentialToolResults(msgs []llm.Message, prefix, toolName string, count int, contentFor func(string) string) []llm.Message {
	for i := 0; i < count; i++ {
		callID := prefix + string(rune('a'+i))
		msgs = append(msgs,
			llm.Message{Role: "assistant", ToolCalls: []llm.ToolCall{{ID: callID, Name: toolName}}},
			llm.Message{Role: "tool", ToolCallID: callID, Content: contentFor(callID)},
		)
	}
	return msgs
}

func TestMicrocompactToolResultsReplacesStaleResultsBeyondKeepRecent(t *testing.T) {
	msgs := []llm.Message{{Role: "user", Content: "start"}}
	msgs = appendSequentialToolResults(msgs, "call-", "file", 12, func(string) string {
		return strings.Repeat("x", 600)
	})
	msgs = append(msgs, llm.Message{Role: "assistant", Content: "done"})
	out := microcompactToolResults(msgs, 10, 500)
	stubCount := 0
	verbatimCount := 0
	for _, msg := range out {
		if msg.Role != "tool" {
			continue
		}
		if msg.Content == ClearedPlaceholder {
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
	msgs = appendSequentialToolResults(msgs, "call-", "file", 12, func(string) string { return "ok" })
	out := microcompactToolResults(msgs, 10, 500)
	for _, msg := range out {
		if msg.Role == "tool" && msg.Content == ClearedPlaceholder {
			t.Fatalf("short result was compacted: %+v", msg)
		}
	}
}

func TestMicrocompactToolResultsClearsOldResultsForAnyToolName(t *testing.T) {
	msgs := []llm.Message{{Role: "user", Content: "start"}}
	msgs = appendSequentialToolResults(msgs, "call-", "write_file", 12, func(string) string {
		return strings.Repeat("x", 600)
	})
	msgs = append(msgs, llm.Message{Role: "assistant", Content: "done"})
	out := microcompactToolResults(msgs, 10, 500)
	stubCount := 0
	for _, msg := range out {
		if msg.Role == "tool" && msg.Content == ClearedPlaceholder {
			stubCount++
		}
	}
	if stubCount != 2 {
		t.Fatalf("stubCount = %d, want 2", stubCount)
	}
}

func TestMicrocompactPreservesFreshTailBatchEvenWhenOverKeepRecent(t *testing.T) {
	msgs := []llm.Message{{Role: "user", Content: "start"}}
	var calls []llm.ToolCall
	for i := 0; i < 12; i++ {
		calls = append(calls, llm.ToolCall{ID: "fresh-" + string(rune('a'+i)), Name: "search"})
	}
	msgs = append(msgs, llm.Message{Role: "assistant", ToolCalls: calls})
	for _, call := range calls {
		msgs = append(msgs, llm.Message{Role: "tool", ToolCallID: call.ID, Content: strings.Repeat(call.ID, 100)})
	}

	out, stats := Microcompact(msgs, 3, 10)
	if stats.Candidates != 0 || stats.Cleared != 0 {
		t.Fatalf("stats = %+v, want no fresh-tail candidates cleared", stats)
	}
	for _, msg := range out {
		if msg.Role == "tool" && msg.Content == ClearedPlaceholder {
			t.Fatalf("fresh tail result was cleared: %+v", msg)
		}
	}
}

func TestMicrocompactClearsStaleMultiCallBatchAndIsIdempotent(t *testing.T) {
	msgs := []llm.Message{{Role: "user", Content: "start"}}
	calls := []llm.ToolCall{
		{ID: "old-a", Name: "search"},
		{ID: "old-b", Name: "web"},
		{ID: "old-c", Name: "execute_shell"},
	}
	msgs = append(msgs, llm.Message{Role: "assistant", ToolCalls: calls})
	for _, call := range calls {
		msgs = append(msgs, llm.Message{Role: "tool", ToolCallID: call.ID, Content: strings.Repeat(call.ID, 100)})
	}
	msgs = append(msgs, llm.Message{Role: "assistant", Content: "done"})

	first, stats := Microcompact(msgs, 1, 10)
	if stats.Cleared != 2 {
		t.Fatalf("stats.Cleared = %d, want 2", stats.Cleared)
	}
	if first[2].Content != ClearedPlaceholder || first[3].Content != ClearedPlaceholder {
		t.Fatalf("old multi-call entries were not cleared: %+v", first)
	}
	if first[4].Content == ClearedPlaceholder {
		t.Fatalf("most recent historical result should stay full")
	}
	second, secondStats := Microcompact(first, 1, 10)
	if secondStats.Cleared != 0 {
		t.Fatalf("second pass cleared %d entries, want idempotent no-op", secondStats.Cleared)
	}
	if !reflect.DeepEqual(first, second) {
		t.Fatalf("second pass changed messages")
	}
}

func TestTruncateOversizedToolResultsCapsAtMaxChars(t *testing.T) {
	big := strings.Repeat("y", 10_000)
	in := []llm.Message{
		{Role: "assistant", ToolCalls: []llm.ToolCall{{ID: "c", Name: "file"}}},
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
		{Role: "assistant", ToolCalls: []llm.ToolCall{{ID: "c", Name: "file"}}},
		{Role: "tool", ToolCallID: "c", Content: "small"},
	}
	out := truncateOversizedToolResults(in, 1000)
	if out[1].Content != "small" {
		t.Fatalf("small result mutated: %q", out[1].Content)
	}
}

func TestApplyGovernanceChainsAllTransforms(t *testing.T) {
	msgs := []llm.Message{{Role: "user", Content: "start"}}
	msgs = appendSequentialToolResults(msgs, "call-", "file", 12, func(string) string {
		return strings.Repeat("x", 700)
	})
	msgs = append(msgs,
		llm.Message{Role: "assistant", ToolCalls: []llm.ToolCall{{ID: "missing", Name: "execute_code"}}},
		llm.Message{Role: "tool", ToolCallID: "ghost", Content: "i should be dropped"},
	)

	out := Apply(msgs, 500, 0, 0)

	for _, msg := range out {
		if msg.Role == "tool" && msg.ToolCallID == "ghost" {
			t.Fatal("orphan survived Apply")
		}
	}
	var backfilled bool
	for _, msg := range out {
		if msg.Role == "tool" && msg.ToolCallID == "missing" {
			backfilled = true
		}
	}
	if !backfilled {
		t.Fatal("missing tool_call_id was not backfilled")
	}
	for _, msg := range out {
		if msg.Role != "tool" {
			continue
		}
		if len(msg.Content) > 500 {
			t.Fatalf("result above maxChars survived: %d bytes", len(msg.Content))
		}
	}
}

func TestApplyGovernanceIncrementsMicrocompactMetricWhenClearing(t *testing.T) {
	oldCounters := ctxmetrics.Global
	ctxmetrics.Global = &ctxmetrics.Counters{}
	t.Cleanup(func() { ctxmetrics.Global = oldCounters })

	msgs := []llm.Message{{Role: "user", Content: "start"}}
	msgs = appendSequentialToolResults(msgs, "old-", "search", 3, func(string) string {
		return strings.Repeat("x", 600)
	})
	msgs = append(msgs, llm.Message{Role: "assistant", Content: "done"})

	_ = Apply(msgs, 5000, 1, 100)

	if got := ctxmetrics.Global.MicrocompactRunsTotal.Load(); got != 1 {
		t.Fatalf("MicrocompactRunsTotal = %d, want 1", got)
	}
}
