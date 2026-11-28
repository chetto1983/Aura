package tools

import (
	"reflect"
	"testing"

	"github.com/chetto1983/aura/internal/llm"
)

func referencesRegistry() *Registry {
	reg := NewRegistry()
	reg.Register(&ToolSearch{Registry: reg})
	reg.Register(deferredOnlyTool{name: "alpha"})
	reg.Register(deferredOnlyTool{name: "beta"})
	reg.Register(activeTool{name: "plain"})
	return reg
}

func referencesCall(id, name string) llm.ToolCall {
	tc := llm.ToolCall{ID: id, Type: "function"}
	tc.Function.Name = name
	return tc
}

func specOf(t *testing.T, reg *Registry, name string) string {
	t.Helper()
	tool, ok := reg.Get(name)
	if !ok {
		t.Fatalf("%s is not registered", name)
	}
	return RenderSpec(tool.Spec())
}

// refusal renders a gate refusal the way the dispatch gate writes it: the prefix, the
// sentence, then the refused tool's spec.
func refusal(t *testing.T, reg *Registry, name string) string {
	t.Helper()
	return NotLoadedPrefix + `"` + name + `" was called before its schema was read.` + "\n\n" + specOf(t, reg, name)
}

func TestLoadedByAnchorsEachGrantToItsCall(t *testing.T) {
	t.Parallel()
	reg := referencesRegistry()
	search := specOf(t, reg, "alpha") + specOf(t, reg, "plain") + specOf(t, reg, "beta") + specOf(t, reg, "alpha")
	cases := []struct {
		name, called, content string
		want                  []string
	}{
		{"a search loads its deferred specs, once each, in order", toolSearchName, search, []string{"alpha", "beta"}},
		{"a refusal loads the refused tool", "alpha", refusal(t, reg, "alpha"), []string{"alpha"}},
		{"a refusal answering another tool loads nothing", "beta", refusal(t, reg, "alpha"), nil},
		{"a refusal of a non-deferred tool loads nothing", "plain", refusal(t, reg, "plain"), nil},
		{"a refusal carrying a second spec loads nothing", "alpha", refusal(t, reg, "alpha") + specOf(t, reg, "beta"), nil},
		{"a spec block without the refusal prefix loads nothing", "alpha", specOf(t, reg, "alpha"), nil},
		{"a spec printed by an ordinary tool loads nothing", "plain", specOf(t, reg, "alpha"), nil},
		{"a refusal cut before its Parameters body loads nothing", "alpha", NotLoadedPrefix + "\n\n## alpha\nfull desc alpha\n\nParameters:\n", nil},
		{"an unpaired result loads nothing", "", refusal(t, reg, "alpha"), nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := reg.LoadedBy(tc.called, tc.content); !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("LoadedBy(%q) = %v, want %v", tc.called, got, tc.want)
			}
		})
	}
}

func TestReferencesIndexesEveryLoadingResult(t *testing.T) {
	t.Parallel()
	reg := referencesRegistry()
	history := []llm.Message{
		{Role: llm.RoleUser, Content: "ciao"},
		{Role: llm.RoleAssistant, ToolCalls: []llm.ToolCall{referencesCall("s1", toolSearchName), referencesCall("c1", "beta")}},
		{Role: llm.RoleTool, ToolCallID: "s1", Content: specOf(t, reg, "alpha")},
		{Role: llm.RoleTool, ToolCallID: "c1", Content: refusal(t, reg, "beta")},
		{Role: llm.RoleAssistant, ToolCalls: []llm.ToolCall{referencesCall("p1", "plain")}},
		{Role: llm.RoleTool, ToolCallID: "p1", Content: refusal(t, reg, "alpha")},
	}
	want := map[int][]string{2: {"alpha"}, 3: {"beta"}}
	if got := reg.References(history); !reflect.DeepEqual(got, want) {
		t.Fatalf("References = %v, want %v", got, want)
	}
	if got := ResultCalls(history); !reflect.DeepEqual(got, map[int]string{2: toolSearchName, 3: "beta", 5: "plain"}) {
		t.Fatalf("ResultCalls = %v", got)
	}
}

// Some routes number calls per response (call_0, call_1, ...), so an id comes back in a
// later turn. Each result pairs with the most recent call before it: the early search still
// loads, and the later echo under the same id does not.
func TestReferencesPairEachResultWithItsOwnCallWhenIdsRepeat(t *testing.T) {
	t.Parallel()
	reg := referencesRegistry()
	history := []llm.Message{
		{Role: llm.RoleAssistant, ToolCalls: []llm.ToolCall{referencesCall("call_0", toolSearchName)}},
		{Role: llm.RoleTool, ToolCallID: "call_0", Content: specOf(t, reg, "alpha")},
		{Role: llm.RoleAssistant, ToolCalls: []llm.ToolCall{referencesCall("call_0", "plain")}},
		{Role: llm.RoleTool, ToolCallID: "call_0", Content: specOf(t, reg, "beta")},
		{Role: llm.RoleTool, ToolCallID: "never_called", Content: specOf(t, reg, "beta")},
	}
	if got, want := reg.References(history), map[int][]string{1: {"alpha"}}; !reflect.DeepEqual(got, want) {
		t.Fatalf("References = %v, want %v", got, want)
	}
}

// The gate writes NotLoadedPrefix and RenderSpec together; the parser must read that back.
func TestLoadedSchemasReadsWhatRenderSpecWrites(t *testing.T) {
	t.Parallel()
	reg := referencesRegistry()
	if got := LoadedSchemas(refusal(t, reg, "alpha")); !reflect.DeepEqual(got, []string{"alpha"}) {
		t.Fatalf("LoadedSchemas = %v, want [alpha]", got)
	}
}
