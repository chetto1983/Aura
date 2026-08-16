package conversations

import (
	"strings"
	"testing"

	"github.com/chetto1983/aura/internal/llm"
)

// The name lives only on the assistant turn that requested the call, so without this lookup
// every evicted result reads as an anonymous "[tool result evicted]".
func TestToolNamesAreReadFromTheAssistantTurns(t *testing.T) {
	turns := []Turn{
		{Seq: 1, Role: llm.RoleAssistant, ToolCalls: []byte(
			`[{"id":"call_a","function":{"name":"shell_exec"}},{"id":"call_b","function":{"name":"document_search"}}]`)},
		{Seq: 2, Role: llm.RoleTool, ToolCallID: "call_a", Content: "output"},
		// Unparsable history must not fail the turn; it only loses the name.
		{Seq: 3, Role: llm.RoleAssistant, ToolCalls: []byte(`{ this is not a call list `)},
	}
	names := toolNamesByCallID(turns)
	if names["call_a"] != "shell_exec" || names["call_b"] != "document_search" {
		t.Fatalf("names = %v", names)
	}
}

// The pointer has to say which tool and how big, because that is precisely what the model
// can no longer see -- and the size is the cheapest signal of whether paging back is worth
// a round trip.
func TestEvictedPointerNamesTheToolAndTheSize(t *testing.T) {
	line := describeEvictedResult("shell_exec", 27515, "spill-1")
	for _, want := range []string{"shell_exec", "27 KB", "read_tool_output", "spill-1"} {
		if !strings.Contains(line, want) {
			t.Fatalf("pointer %q lost %q", line, want)
		}
	}

	// With nothing to page back from, the pointer must say so rather than hand out a call
	// that returns nothing.
	orphan := describeEvictedResult("", 300, "")
	if strings.Contains(orphan, "read_tool_output") || !strings.Contains(orphan, "not retrievable") {
		t.Fatalf("unretrievable pointer = %q", orphan)
	}
}

// Both wordings must survive the repair pass: the durable history holds rows written before
// the pointer was given a name, and dropping them would silently delete the only trace that
// a tool ran at all.
func TestBothPointerWordingsAreRecognised(t *testing.T) {
	for name, content := range map[string]string{
		"current": describeEvictedResult("shell_exec", 1000, "spill-1"),
		"legacy":  "[tool output evicted to save context; page it back via read_tool_output(tool_call_id=\"x\")]",
	} {
		t.Run(name, func(t *testing.T) {
			if !compactedToolPointer(llm.Message{Content: content}) {
				t.Fatalf("not recognised: %q", content)
			}
		})
	}
	if compactedToolPointer(llm.Message{Content: "a normal assistant sentence"}) {
		t.Fatal("ordinary prose was taken for an eviction pointer")
	}
}

func TestHumanBytesReadsLikeALog(t *testing.T) {
	for in, want := range map[int]string{0: "empty", 300: "300 bytes", 27515: "27 KB", 1024: "1 KB"} {
		if got := humanBytes(in); got != want {
			t.Errorf("humanBytes(%d) = %q, want %q", in, got, want)
		}
	}
}
