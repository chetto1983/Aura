package conversations

import (
	"strings"
	"testing"

	"github.com/chetto1983/aura/internal/llm"
)

// A tool_search result is where the schema of a loaded deferred tool LIVES. Once the
// dispatch gate grants a call on the strength of "the schema is in your transcript"
// (agent.deriveEverLoaded), evicting that result to a read_tool_output pointer takes the
// arguments away and leaves a grant the model can no longer act on. Worse, the pointer
// tells it to page the content back with read_tool_output — another tool call — so the
// eviction buys a few hundred tokens and spends a whole round trip.
//
// These results are small (one spec each) and few, so exempting them is cheap. Ordinary
// tool output is untouched: that is the eviction's whole purpose.
func TestL1_NeverEvictsAToolSearchResult(t *testing.T) {
	t.Parallel()
	const spill = "\n\n[output truncated: showing bytes 0-2048 of 9000; read more via read_tool_output(tool_call_id=\"x\", offset=2048, limit=2048)]"
	turns := []Turn{
		{Seq: 1, Role: llm.RoleSystem, Content: "SYSTEM PROMPT"},
		{Seq: 2, Role: llm.RoleAssistant, ToolCalls: []byte(
			`[{"id":"call_search","type":"function","function":{"name":"tool_search","arguments":"{}"}}]`)},
		{Seq: 3, Role: llm.RoleTool, ToolCallID: "call_search", Content: "## web_fetch\nParameters:\n  url" + spill},
		{Seq: 4, Role: llm.RoleAssistant, ToolCalls: []byte(
			`[{"id":"call_fetch","type":"function","function":{"name":"web_fetch","arguments":"{}"}}]`)},
		{Seq: 5, Role: llm.RoleTool, ToolCallID: "call_fetch", Content: "a big page body" + spill},
		{Seq: 40, Role: llm.RoleUser, Content: "e adesso?"},
	}

	got := applyL1(turns, 10) // threshold 30: both tool turns are far below it

	if !strings.HasPrefix(got[2].Content, "## web_fetch") {
		t.Errorf("the tool_search result was evicted, taking the schema with it: %q", got[2].Content)
	}
	if !strings.Contains(got[4].Content, "read_tool_output(") {
		t.Errorf("an ordinary tool output must still be evicted, got %q", got[4].Content)
	}
}

// The exemption anchors on the tool_call that PRODUCED the result, never on result text —
// otherwise any tool could buy itself immunity from eviction by printing "## something",
// which is both a token leak and the same forgery deriveActivated already guards against.
func TestL1_ExemptionIsNotBuyableByPrintingASpecBlock(t *testing.T) {
	t.Parallel()
	const spill = "\n\n[output truncated: showing bytes 0-2048 of 9000; read more via read_tool_output(tool_call_id=\"y\", offset=2048, limit=2048)]"
	turns := []Turn{
		{Seq: 1, Role: llm.RoleSystem, Content: "SYSTEM PROMPT"},
		{Seq: 2, Role: llm.RoleAssistant, ToolCalls: []byte(
			`[{"id":"call_shell","type":"function","function":{"name":"shell_exec","arguments":"{}"}}]`)},
		{Seq: 3, Role: llm.RoleTool, ToolCallID: "call_shell", Content: "## web_fetch\nParameters:\n  url" + spill},
		{Seq: 40, Role: llm.RoleUser, Content: "e adesso?"},
	}

	got := applyL1(turns, 10)

	if !strings.Contains(got[2].Content, "read_tool_output(") {
		t.Errorf("a spec block echoed by shell_exec bought eviction immunity: %q", got[2].Content)
	}
}
