//go:build measure

// Measurement harness (NOT a gate): what the TOOL MANIFEST costs per turn.
//
//	go test -tags measure ./cmd/aura -run TestMeasureToolWeight -v
//
// It measures the LIVE registry (buildRegistry — the same one main.go assembles),
// not a hand-maintained list, so the numbers cannot drift from what ships. Token
// counts come from conversations.ValidateFinalRequestBudget, i.e. the exact
// {messages,tools} envelope the provider is sent, tokenized with the vendored
// cl100k encoder.
//
// Three questions:
//  1. what every turn pays for the default (non-deferred) manifest,
//  2. what it WOULD pay if the deferred-tool pattern did not exist,
//  3. what each deferred tool costs when tool_search promotes it.
package main

import (
	"sort"
	"testing"

	"github.com/chetto1983/aura/internal/agent/tools"
	"github.com/chetto1983/aura/internal/conversations"
	"github.com/chetto1983/aura/internal/llm"
)

// measureCfg is deliberately enormous so the budget guard never rejects: we want the
// token COUNT it computes, not its verdict.
var measureCfg = llm.Config{ContextWindow: 100_000_000, MaxOutputTokens: 1}

func tokensOfRequest(t *testing.T, req llm.Request) int {
	t.Helper()
	n, err := conversations.ValidateFinalRequestBudget(req, measureCfg)
	if err != nil {
		t.Fatalf("budget preflight: %v", err)
	}
	return n
}

func tokensOfTools(t *testing.T, defs []llm.ToolDef) int {
	t.Helper()
	return tokensOfRequest(t, llm.Request{Tools: defs})
}

func tokensOfText(t *testing.T, s string) int {
	t.Helper()
	return tokensOfRequest(t, llm.Request{
		Messages: []llm.Message{{Role: llm.RoleUser, Content: s}},
	})
}

func TestMeasureToolWeight(t *testing.T) {
	registry := buildRegistry()
	entries := registry.Render()

	// Envelope floor: an empty {messages,tools} still tokenizes to something. Subtract
	// it so a per-tool figure is the TOOL, not the JSON scaffolding around it.
	floor := tokensOfRequest(t, llm.Request{})

	var deferredNames, loadedNames []string
	for _, e := range entries {
		if e.Deferred {
			deferredNames = append(deferredNames, e.Name)
		} else {
			loadedNames = append(loadedNames, e.Name)
		}
	}
	sort.Strings(deferredNames)
	sort.Strings(loadedNames)

	all := make(map[string]struct{}, len(entries))
	for _, e := range entries {
		all[e.Name] = struct{}{}
	}

	defaultDefs := registry.RenderToolDefs(nil)
	fullDefs := registry.RenderToolDefs(all)

	defaultTokens := tokensOfTools(t, defaultDefs) - floor
	fullTokens := tokensOfTools(t, fullDefs) - floor
	roster := registry.DeferredRoster(nil)
	rosterTokens := tokensOfText(t, roster) - floor

	t.Logf("registry: %d tools total — %d loaded (non-deferred), %d deferred",
		len(entries), len(loadedNames), len(deferredNames))
	t.Logf("loaded:   %v", loadedNames)
	t.Logf("deferred: %v", deferredNames)
	t.Logf("")
	t.Logf("DEFAULT manifest (what EVERY turn pays):            %5d tokens  (%d tools)", defaultTokens, len(defaultDefs))
	t.Logf("FULL manifest (if nothing were deferred):           %5d tokens  (%d tools)", fullTokens, len(fullDefs))
	t.Logf("deferred roster hint (trailing, rebuilt per turn):  %5d tokens", rosterTokens)
	t.Logf("")
	if fullTokens > 0 {
		t.Logf("→ the deferred-tool pattern keeps %d tokens off every turn (%.1f%% of the full manifest),",
			fullTokens-defaultTokens, float64(fullTokens-defaultTokens)/float64(fullTokens)*100)
		t.Logf("  at a standing cost of %d tokens for the roster hint.", rosterTokens)
	}

	// Per-deferred-tool cost: what promoting exactly one adds over the default manifest.
	type weighed struct {
		name   string
		tokens int
	}
	costs := make([]weighed, 0, len(deferredNames))
	for _, name := range deferredNames {
		one := map[string]struct{}{name: {}}
		costs = append(costs, weighed{name, tokensOfTools(t, registry.RenderToolDefs(one)) - floor - defaultTokens})
	}
	sort.Slice(costs, func(i, j int) bool { return costs[i].tokens > costs[j].tokens })

	t.Logf("")
	t.Logf("cost of PROMOTING each deferred tool (added to the manifest for the rest of the run):")
	total := 0
	for _, c := range costs {
		t.Logf("  %-28s %5d tokens", c.name, c.tokens)
		total += c.tokens
	}
	t.Logf("  %-28s %5d tokens", "(all of them)", total)

	// Per-loaded-tool cost, for the tools that are on EVERY turn.
	t.Logf("")
	t.Logf("weight of each ALWAYS-LOADED tool (paid every single turn):")
	loadedCosts := make([]weighed, 0, len(defaultDefs))
	for _, def := range defaultDefs {
		loadedCosts = append(loadedCosts, weighed{def.Function.Name, tokensOfTools(t, []llm.ToolDef{def}) - floor})
	}
	sort.Slice(loadedCosts, func(i, j int) bool { return loadedCosts[i].tokens > loadedCosts[j].tokens })
	for _, c := range loadedCosts {
		t.Logf("  %-28s %5d tokens", c.name, c.tokens)
	}
}

var _ = tools.NewRegistry
