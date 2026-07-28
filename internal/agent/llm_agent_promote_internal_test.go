package agent

import (
	"testing"

	"github.com/chetto1983/aura/internal/agent/prompt"
	"github.com/chetto1983/aura/internal/agent/tools"
	"github.com/chetto1983/aura/internal/llm"
	"github.com/google/uuid"
)

// promoteAgent builds a minimal LlmAgent whose registry holds one non-deferred tool
// (text_response) and one deferred tool (send_file), enough to exercise the
// full-promotion helpers and the buildRequest manifest filtering.
func promoteAgent(t *testing.T) *LlmAgent {
	t.Helper()
	return promoteAgentWithHistory(t, []llm.Message{{Role: llm.RoleUser, Content: "ciao"}})
}

// promoteAgentWithHistory is promoteAgent seeded with a REHYDRATED multi-turn
// history (what runner.buildAgent passes as UserTurns on every turn after the first).
func promoteAgentWithHistory(t *testing.T, hist []llm.Message) *LlmAgent {
	t.Helper()
	reg := tools.NewRegistry()
	reg.Register(tools.TextResponse{})
	reg.Register(&tools.SendFile{})
	reg.Register(&tools.ToolSearch{Registry: reg})
	return NewLlmAgent(LlmAgentConfig{
		LLM:       llm.Config{Model: "test-model", Provider: "openrouter"},
		Registry:  reg,
		RunDir:    t.TempDir(),
		SessionID: uuid.Must(uuid.NewV7()).String(),
		UserTurns: hist,
	})
}

// sendFileSchemaResult mirrors the shape tools.ToolSearch.Execute writes for one
// match: a "## <name>" header, the description, then the Parameters block.
const sendFileSchemaResult = "## send_file\nSend a file to the user.\n\nParameters:\n{\"type\":\"object\"}\n\n"

// call builds one assistant-emitted tool_call (the nested Function struct is
// anonymous, so it cannot be filled with a composite literal).
func call(id, name, args string) llm.ToolCall {
	tc := llm.ToolCall{ID: id, Type: "function"}
	tc.Function.Name = name
	tc.Function.Arguments = args
	return tc
}

// TestIsDeferredUnloaded covers the dispatch-gate predicate: true for a
// deferred-and-unloaded tool, false for a non-deferred tool, false for an unknown
// tool, and false once the deferred tool has been promoted.
func TestIsDeferredUnloaded(t *testing.T) {
	a := promoteAgent(t)

	if !a.isDeferredUnloaded("send_file") {
		t.Fatal("send_file is deferred and unloaded — gate must report it unloaded")
	}
	if a.isDeferredUnloaded("text_response") {
		t.Fatal("text_response is non-deferred — gate must never bounce it")
	}
	if a.isDeferredUnloaded("does_not_exist") {
		t.Fatal("an unregistered tool is not a deferred-unloaded tool")
	}

	a.activated["send_file"] = struct{}{}
	if a.isDeferredUnloaded("send_file") {
		t.Fatal("send_file is promoted — gate must no longer bounce it")
	}
}

// TestPromoteFromMeta covers the promotion helper: nil-safe, ignores a wrong-typed
// Meta value, and adds each name from a []string MetaActivatedTools entry.
func TestPromoteFromMeta(t *testing.T) {
	a := promoteAgent(t)

	a.promoteFromMeta(nil) // must not panic
	if len(a.activated) != 0 {
		t.Fatalf("nil meta promoted %d names, want 0", len(a.activated))
	}

	wrong := tools.ToolResultMeta{tools.MetaActivatedTools: "not-a-slice"}
	a.promoteFromMeta(&wrong)
	if len(a.activated) != 0 {
		t.Fatalf("wrong-typed meta promoted %d names, want 0", len(a.activated))
	}

	meta := tools.ToolResultMeta{tools.MetaActivatedTools: []string{"send_file", "other_tool"}}
	a.promoteFromMeta(&meta)
	for _, name := range []string{"send_file", "other_tool"} {
		if _, ok := a.activated[name]; !ok {
			t.Fatalf("promoteFromMeta did not add %q; activated=%v", name, a.activated)
		}
	}
}

// TestBuildRequestHidesDeferredUntilPromoted is the integration-style assertion: a
// deferred tool is ABSENT from buildRequest's req.Tools until it is promoted, then
// PRESENT (with a non-empty schema). This is the whole point of full-promotion —
// no empty-schema callable function ever reaches the model.
func TestBuildRequestHidesDeferredUntilPromoted(t *testing.T) {
	a := promoteAgent(t)
	budget := prompt.Budget{}

	before := a.buildRequest(budget, prompt.ReasoningTierNone, false)
	if toolPresent(before.Tools, "send_file") {
		t.Fatal("send_file must be HIDDEN from req.Tools before tool_search loads its schema")
	}
	if !toolPresent(before.Tools, "text_response") {
		t.Fatal("the non-deferred text_response must always be present in req.Tools")
	}

	a.promoteFromMeta(&tools.ToolResultMeta{tools.MetaActivatedTools: []string{"send_file"}})

	after := a.buildRequest(budget, prompt.ReasoningTierNone, false)
	def, ok := toolDef(after.Tools, "send_file")
	if !ok {
		t.Fatal("send_file must be PRESENT in req.Tools once promoted")
	}
	if len(def.Function.Parameters) == 0 {
		t.Fatal("a promoted deferred tool must carry its full Parameters schema (never empty)")
	}
}

// TestActivationSurvivesTheTurn proves the deferred-tool grant is CONVERSATION-scoped,
// not turn-scoped: tool_search loaded send_file's schema in an earlier turn, that schema
// is still in the rehydrated history, so the fresh per-turn agent must still consider it
// callable. Before this, activated started empty on every turn while the schema stayed
// visible in the transcript — the model read the schema, called the tool, and the gate
// bounced it, forcing a redundant tool_search every single turn.
func TestActivationSurvivesTheTurn(t *testing.T) {
	const searchID = "call_search_1"
	a := promoteAgentWithHistory(t, []llm.Message{
		{Role: llm.RoleUser, Content: "mandami il report"},
		{Role: llm.RoleAssistant, ToolCalls: []llm.ToolCall{call(searchID, "tool_search", `{"query":"select:send_file"}`)}},
		{Role: llm.RoleTool, ToolCallID: searchID, Content: sendFileSchemaResult},
		{Role: llm.RoleAssistant, Content: "fatto"},
		{Role: llm.RoleUser, Content: "mandalo di nuovo"},
	})

	if a.isDeferredUnloaded("send_file") {
		t.Fatal("send_file's schema is still in the rehydrated history — a new turn must not force a redundant tool_search")
	}
}

// TestActivationRequiresARealSearchResult proves the rehydration is anchored to the
// tool_call that PRODUCED the result, not to its text: a shell_exec result that merely
// echoes a schema-shaped header must not self-grant the tool. Without the pairing check
// the model could mint its own permissions (`echo "## send_file"`) and then call the tool
// with fabricated arguments — the exact hallucination full-promotion exists to stop.
func TestActivationRequiresARealSearchResult(t *testing.T) {
	const shellID = "call_shell_1"
	a := promoteAgentWithHistory(t, []llm.Message{
		{Role: llm.RoleUser, Content: "ciao"},
		{Role: llm.RoleAssistant, ToolCalls: []llm.ToolCall{call(shellID, "shell_exec", `{"command":"echo"}`)}},
		{Role: llm.RoleTool, ToolCallID: shellID, Content: sendFileSchemaResult},
		{Role: llm.RoleUser, Content: "ora mandalo"},
	})

	if !a.isDeferredUnloaded("send_file") {
		t.Fatal("only a genuine tool_search result may grant a deferred tool; an echoed header must not")
	}
}

// TestActivationDecaysWithTheSchema locks the self-healing property: the grant is
// derived from the schema's PRESENCE in history, so when the preview cap truncates a
// batch select and cuts a tool's header away, that tool goes back to needing a search.
// Permission and schema decay together — a grant whose schema is gone would leave the
// model calling a tool it can no longer read the arguments for.
func TestActivationDecaysWithTheSchema(t *testing.T) {
	const searchID = "call_search_2"
	truncated := "## text_response\nReply to the user.\n\nParameters:\n{}\n\n## send_f" +
		"\n\n[output truncated: showing bytes 0-64 of 4096; read more via read_tool_output(...)]"
	a := promoteAgentWithHistory(t, []llm.Message{
		{Role: llm.RoleUser, Content: "ciao"},
		{Role: llm.RoleAssistant, ToolCalls: []llm.ToolCall{call(searchID, "tool_search", `{"query":"select:text_response,send_file"}`)}},
		{Role: llm.RoleTool, ToolCallID: searchID, Content: truncated},
		{Role: llm.RoleUser, Content: "ora mandalo"},
	})

	if !a.isDeferredUnloaded("send_file") {
		t.Fatal("send_file's schema was truncated out of history — the grant must decay with it, not outlive it")
	}
}

func toolPresent(defs []llm.ToolDef, name string) bool {
	_, ok := toolDef(defs, name)
	return ok
}

func toolDef(defs []llm.ToolDef, name string) (llm.ToolDef, bool) {
	for _, d := range defs {
		if d.Function.Name == name {
			return d, true
		}
	}
	return llm.ToolDef{}, false
}
