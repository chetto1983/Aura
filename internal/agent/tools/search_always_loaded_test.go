package tools

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

// Production shape since 2026-08-31 (39c677d87): the memory server's three
// model-facing tools hold an always-loaded manifest slot, so they mount with
// Deferred=false. The model still discovers by capability — its lead-in orders a
// tool_search for anything absent from the manifest — so tool_search has to answer
// for a tool that is loaded, not report a capability gap.
var alwaysLoadedMemorySpecs = []Spec{
	{
		Name:        "memory__memory_recall",
		Summary:     "Recall a deeper or historical fact from long-term memory.",
		Description: "Recall from the knowledge graph.",
		Parameters:  json.RawMessage(`{"type":"object","properties":{"query":{"type":"string"}}}`),
		Deferred:    false,
	},
	{
		Name:        "memory__memory_upsert_fact",
		Summary:     "Write down a durable fact about the user or the world.",
		Description: "Upsert one fact.",
		Parameters:  json.RawMessage(`{"type":"object","properties":{"fact":{"type":"string"}}}`),
		Deferred:    false,
	},
	{
		Name:        "memory__memory_batch",
		Summary:     "Apply several memory writes atomically.",
		Description: "Atomic batch.",
		Parameters:  json.RawMessage(`{"type":"object","properties":{"writes":{"type":"array"}}}`),
		Deferred:    false,
	},
}

// newProductionShapedSearch mirrors the deployed registry: the dumped deferred
// corpus MINUS the memory namespace (which is no longer deferred), PLUS the three
// always-loaded memory tools.
func newProductionShapedSearch(t *testing.T) (*ToolSearch, context.Context) {
	t.Helper()
	reg := NewRegistry()
	for _, s := range loadManifestFixture(t) {
		if strings.HasPrefix(s.Name, "memory__") {
			continue
		}
		reg.Register(manifestTool{spec: s})
	}
	for _, s := range alwaysLoadedMemorySpecs {
		reg.Register(manifestTool{spec: s})
	}
	ts := &ToolSearch{Registry: reg}
	reg.Register(ts)
	return ts, ctxWith(t, "sess-always-loaded", "call-always-loaded")
}

func execSearch(t *testing.T, ts *ToolSearch, ctx context.Context, query string) ToolResult {
	t.Helper()
	args, err := json.Marshal(map[string]string{"query": query})
	if err != nil {
		t.Fatalf("marshal query: %v", err)
	}
	res, err := ts.Execute(ctx, args)
	if err != nil {
		t.Fatalf("Execute(%q): %v", query, err)
	}
	return res
}

func TestToolSearch_FreeTextFindsAlwaysLoadedTool(t *testing.T) {
	ts, ctx := newProductionShapedSearch(t)
	res := execSearch(t, ts, ctx, "memory")
	if !strings.Contains(res.Preview, "memory__memory_recall") {
		t.Fatalf("free-text %q did not surface the always-loaded memory tools.\nreply: %s", "memory", res.Preview)
	}
}

func TestToolSearch_AlwaysLoadedMatchIsNotReportedAsCapabilityGap(t *testing.T) {
	ts, ctx := newProductionShapedSearch(t)
	res := execSearch(t, ts, ctx, "memory")
	if strings.Contains(res.Preview, "no matching tools") {
		t.Fatalf("a mounted, already-loaded capability was reported as a gap.\nreply: %s", res.Preview)
	}
}

// An always-loaded tool is already in the manifest and already callable, so it must
// not consume one of the bounded deferred-promotion slots (maxPromotedDeferredTools).
func TestToolSearch_AlwaysLoadedMatchIsNotPromoted(t *testing.T) {
	ts, ctx := newProductionShapedSearch(t)
	res := execSearch(t, ts, ctx, "memory")
	if res.Meta == nil {
		return
	}
	names, _ := (*res.Meta)[MetaActivatedTools].([]string)
	for _, n := range names {
		if strings.HasPrefix(n, "memory__") {
			t.Fatalf("always-loaded %q was promoted into the deferred grant: %v", n, names)
		}
	}
}
