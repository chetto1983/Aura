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
	reg := tools.NewRegistry()
	reg.Register(tools.TextResponse{})
	reg.Register(&tools.SendFile{})
	return NewLlmAgent(LlmAgentConfig{
		LLM:       llm.Config{Model: "test-model", Provider: "openrouter"},
		Registry:  reg,
		RunDir:    t.TempDir(),
		SessionID: uuid.Must(uuid.NewV7()).String(),
		UserTurns: []llm.Message{{Role: llm.RoleUser, Content: "ciao"}},
	})
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
