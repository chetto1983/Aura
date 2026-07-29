package agent

import (
	"context"
	"encoding/json"
	"sort"
	"strings"
	"testing"

	"github.com/chetto1983/aura/internal/agent/tools"
	"github.com/chetto1983/aura/internal/llm"
)

// decayTool is a deferred tool whose only job is to exist in the registry so
// deriveActivated can resolve a name to a spec.
type decayTool struct {
	name     string
	deferred bool
}

func (d decayTool) Spec() tools.Spec {
	return tools.Spec{
		Name:       d.name,
		Summary:    "fixture",
		Parameters: json.RawMessage(`{"type":"object"}`),
		Deferred:   d.deferred,
	}
}

func (decayTool) Execute(context.Context, json.RawMessage) (tools.ToolResult, error) {
	return tools.ToolResult{}, nil
}

// searchResult renders a tool_search result the way ToolSearch.Execute does — a
// "## <name>" header with a non-empty Parameters body — so loadedSchemas parses it.
func searchResult(names ...string) string {
	var out strings.Builder
	for _, n := range names {
		out.WriteString("## " + n + "\nParameters:\n  {\"type\":\"object\"}\n\n")
	}
	return out.String()
}

func activatedNames(t *testing.T, hist []llm.Message, reg *tools.Registry) []string {
	t.Helper()
	set := deriveActivated(hist, reg)
	out := make([]string, 0, len(set))
	for n := range set {
		out = append(out, n)
	}
	sort.Strings(out)
	return out
}

func decayRegistry() *tools.Registry {
	reg := tools.NewRegistry()
	for _, n := range []string{"alpha", "beta", "gamma"} {
		reg.Register(decayTool{name: n, deferred: true})
	}
	reg.Register(decayTool{name: "plain", deferred: false})
	return reg
}

// search + result for `names`, as one turn of wire-valid history.
func searchTurn(id string, names ...string) []llm.Message {
	return []llm.Message{
		{Role: llm.RoleAssistant, ToolCalls: []llm.ToolCall{toolCall(id, searchTool, "{}")}},
		{Role: llm.RoleTool, ToolCallID: id, Content: searchResult(names...)},
	}
}

func callTurn(name string) llm.Message {
	return llm.Message{Role: llm.RoleAssistant, ToolCalls: []llm.ToolCall{
		toolCall("call-"+name, name, "{}"),
	}}
}

// A deferred tool's grant is what keeps its FULL schema in every subsequent
// manifest, and an MCP tool definition costs ~368 tokens on this deployment. These
// cases pin the rule that stops the promoted set from growing without bound.
func TestDeriveActivated_UsePromotesLookupDoesNot(t *testing.T) {
	t.Parallel()
	reg := decayRegistry()

	t.Run("a tool that was searched AND called stays granted", func(t *testing.T) {
		t.Parallel()
		hist := append(searchTurn("s1", "alpha"), callTurn("alpha"))
		hist = append(hist, searchTurn("s2", "beta")...)
		got := activatedNames(t, hist, reg)
		// beta rides the most-recent-lookup exemption; alpha is there on its own merit.
		if len(got) != 2 || got[0] != "alpha" || got[1] != "beta" {
			t.Fatalf("got %v, want [alpha beta]", got)
		}
	})

	t.Run("a tool searched and never called is dropped once a newer search lands", func(t *testing.T) {
		t.Parallel()
		// This is the leak: without the rule, alpha would stay in the manifest for the
		// rest of the conversation despite the model showing no interest in it.
		hist := append(searchTurn("s1", "alpha"), searchTurn("s2", "gamma")...)
		got := activatedNames(t, hist, reg)
		if len(got) != 1 || got[0] != "gamma" {
			t.Fatalf("got %v, want only the most recent lookup [gamma]", got)
		}
	})

	t.Run("the most recent lookup survives even uncalled", func(t *testing.T) {
		t.Parallel()
		// Searching at the end of a turn and calling at the start of the next has to
		// work, or the model livelocks: search, turn boundary, grant gone, search again.
		got := activatedNames(t, searchTurn("s1", "alpha"), reg)
		if len(got) != 1 || got[0] != "alpha" {
			t.Fatalf("got %v, want [alpha]", got)
		}
	})

	t.Run("many lookups do not accumulate", func(t *testing.T) {
		t.Parallel()
		var hist []llm.Message
		for _, n := range []string{"alpha", "beta", "gamma"} {
			hist = append(hist, searchTurn("s-"+n, n)...)
		}
		got := activatedNames(t, hist, reg)
		if len(got) != 1 {
			t.Fatalf("got %v, want a single surviving grant — the set must not grow with lookups", got)
		}
	})

	t.Run("calling a non-deferred tool grants nothing", func(t *testing.T) {
		t.Parallel()
		// `plain` is always in the manifest; promoting it would be meaningless, and a
		// call to an unregistered name must not mint a grant either.
		hist := []llm.Message{callTurn("plain"), callTurn("never_registered")}
		if got := activatedNames(t, hist, reg); len(got) != 0 {
			t.Fatalf("got %v, want none", got)
		}
	})

	t.Run("a tool_search call itself is not a tool use", func(t *testing.T) {
		t.Parallel()
		// tool_search is not deferred, but it must never be counted as "used" bookkeeping
		// either — otherwise the discovery hook would promote itself.
		if got := activatedNames(t, searchTurn("s1"), reg); len(got) != 0 {
			t.Fatalf("got %v, want none from an empty search result", got)
		}
	})
}
