package conversations

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"testing"

	"github.com/chetto1983/aura/internal/llm"
)

// The Claude route reads each turn's stored history from the cache only because the ladder
// keeps two promises, pinned here on the real ladder: the transient memory block lands
// right before the current question, and everything before that block comes back unchanged
// at the same positions next turn, where the block is gone and the question follows the
// previous answer. internal/llm/anthropic.TestNextTurnReadsThePreviousTurnsHistory puts its
// breakpoint on that shape; if the block moves, this fails first. Measured on the lab VM,
// conversation 01a2223e, 2026-11-28: a breakpoint on the block was never read again.
func TestTransientMemoryStaysOutOfTheReplayedPrefix(t *testing.T) {
	call := func(id, name string) []byte {
		raw, _ := json.Marshal([]map[string]any{{"id": id, "type": "function", "function": map[string]string{"name": name, "arguments": "{}"}}})
		return raw
	}
	turns := []Turn{
		{Seq: 1, Role: llm.RoleUser, Content: "come ti trovi in questo ambiente?"},
		{Seq: 2, Role: llm.RoleAssistant, Content: "Bene, nel senso pratico."},
		{Seq: 3, Role: llm.RoleUser, Content: "si finirti è da maggio che lavoro su Aura"},
		{Seq: 4, Role: llm.RoleAssistant, ToolCalls: call("toolu_01", "memory__memory_upsert_fact")},
		{Seq: 5, Role: llm.RoleTool, ToolCallID: "toolu_01", Content: `{"classified":[{"name":"Davide"}]}`},
		{Seq: 6, Role: llm.RoleAssistant, Content: "Ah, allora conosci Aura molto meglio di me."},
		{Seq: 7, Role: llm.RoleUser, Content: "difatti ti uso per fare debug da dentro"},
		{Seq: 8, Role: llm.RoleAssistant, ToolCalls: call("toolu_02", "memory__memory_upsert_fact")},
		{Seq: 9, Role: llm.RoleTool, ToolCallID: "toolu_02", Content: `{"classified":[]}`},
		// The runner's own notice when a round dies (seq 35 of 01a2223e, the TLS failure).
		{Seq: 10, Role: llm.RoleAssistant, Content: "[run interrupted before it produced an answer]"},
		{Seq: 11, Role: llm.RoleUser, Content: "???"},
		{Seq: 12, Role: llm.RoleAssistant, Content: "Scusa, la risposta precedente è stata interrotta."},
		// A question no answer followed: the ladder pairs it with its own marker.
		{Seq: 13, Role: llm.RoleUser, Content: "e adesso?"},
		{Seq: 14, Role: llm.RoleUser, Content: "ci sei?"},
	}
	enc := mustEncoderRaw(t)
	load := func(through int) []llm.Message {
		cfg := ContextConfig{
			ContextWindow: 200000, MaxOutputTokens: 32000, ToolEvictAfterTurns: 10,
			AlwaysBlock:      "## Skills\n- memory-aura\n\n## Operator profile\nname: Davide",
			TransientContext: &TransientContext{Content: fmt.Sprintf("<memory_context>\n%d facts\n</memory_context>", through), BeforeCurrentUser: true},
			compactionCache:  &fakeCompactionCache{},
		}
		out, err := applyContextLadder(context.Background(), "conv", turns[:through], cfg, enc, &fakeRotEmitter{})
		if err != nil {
			t.Fatalf("ladder through seq %d: %v", through, err)
		}
		return out
	}
	questions := []int{3, 7, 11, 13, 14}
	for i, seq := range questions[:len(questions)-1] {
		turn, next := load(seq), load(questions[i+1])
		memory := llm.Message{Role: llm.RoleUser, Content: fmt.Sprintf("<memory_context>\n%d facts\n</memory_context>", seq)}
		if got := turn[len(turn)-2]; !reflect.DeepEqual(got, memory) {
			t.Fatalf("seq %d: message before the question = %+v, want the memory block", seq, got)
		}
		if got := turn[len(turn)-1].Content; got != turns[seq-1].Content {
			t.Fatalf("seq %d: last message = %q, want the question", seq, got)
		}
		prefix := turn[:len(turn)-2]
		if len(next) <= len(prefix) || !reflect.DeepEqual(next[:len(prefix)], prefix) {
			t.Fatalf("seq %d: the next turn does not replay the history before the memory block:\n turn %+v\n next %+v", seq, prefix, next)
		}
		if got := next[len(prefix)].Content; got != turns[seq-1].Content {
			t.Fatalf("seq %d: the next turn has %q where the memory block was, want the question", seq, got)
		}
	}
}
