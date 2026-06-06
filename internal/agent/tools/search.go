package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"sync"
)

// ToolSearch is the built-in hook tool that lets the LLM fetch full specs of
// deferred tools. The pattern mirrors Claude Code's ToolSearch behavior: the
// model sees only Name+Summary by default, then calls `tool_search` with a
// `select:<name>,<name>` argument or a free-text query to load the full
// Description+Parameters into context.
//
// Free-text queries are ranked by an in-process Okapi BM25 scorer (bm25.go) over
// an expanded per-tool search document, then capped to max_results. The `select:`
// path resolves any registered tool by exact name and stays uncapped.
//
// This is a NON-DEFERRED tool — always visible — so the model can find it
// without recursion.
type ToolSearch struct {
	Registry *Registry

	once     sync.Once
	index    *bm25Index
	deferred []Tool // index-aligned with the bm25Index documents
}

const defaultMaxResults = 5

type toolSearchArgs struct {
	Query      string `json:"query"`
	MaxResults *int   `json:"max_results"`
}

func (ts *ToolSearch) Spec() Spec {
	params := json.RawMessage(`{
  "type": "object",
  "properties": {
    "query": {
      "type": "string",
      "description": "Either 'select:Name1,Name2' to load specific tools by name, or a keyword phrase ranked by BM25 over deferred tool names, descriptions, and parameters."
    },
    "max_results": {
      "type": "integer",
      "description": "Optional cap on the number of keyword matches returned (default 5, must be greater than zero). Ignored for 'select:' queries, which are never capped."
    }
  },
  "required": ["query"]
}`)
	return Spec{
		Name:        "tool_search",
		Summary:     "Load full specifications for deferred tools by name or keyword.",
		Description: toolSearchDescription(ts.Registry),
		Parameters:  params,
		Deferred:    false,
	}
}

const toolSearchLeadIn = "# Tool discovery\n\n" +
	"Searches deferred-tool metadata with BM25 and loads matching tool schemas for the next model call. " +
	"Use 'select:Name1,Name2' for direct selection, or a keyword phrase ranked over deferred tool names, descriptions, and parameters. " +
	"Some tools are not provided upfront; use this tool (`tool_search`) to discover and load them.\n\n" +
	"You have access to deferred tools from the following sources:\n"

// noMatchOrientation is the fixed no-result reply (amendment #49, retargeted #52/D-41).
// A failed tool_search is usually a capability gap; the always-on find-skills skill is the
// designed path for packaged capabilities, so the model is pointed there explicitly
// instead of being left to improvise ad-hoc code. The old `{"action":"catalog",...}`
// routing was removed: that action was deleted in 11-09 (#51/D-40), and discovery+install
// now ride the host terminal (`npx skills find/add`), taught by find-skills-aura.
const noMatchOrientation = "no matching tools. " +
	"If the capability you need is a packaged task family (spreadsheets, documents, file formats, integrations, recurring workflows), " +
	"the always-on find-skills skill teaches how to discover and install skills from the open ecosystem in your terminal — " +
	"installable skills ship tested instructions and bundled scripts that beat ad-hoc code."

// nsDelimiterStr is the "<namespace>__<tool>" delimiter from the mcptools
// namespacing (08.1-03). It is duplicated as a literal here, NOT imported, because
// package tools must not depend on internal/agent/mcptools (import cycle); a 2-char
// delimiter is a stable cross-package contract, not shared state.
const nsDelimiterStr = "__"

// toolSearchDescription builds the registry-derived tool_search Description (D-09):
// a fixed lead-in plus a SORTED, deduped list of the deferred tools' sources
// (grouped by the "__" namespace prefix when present, else "built-in"). Because the
// Registry is immutable for an agent run, the output is byte-stable across calls and
// across turns — tool_search is non-deferred, so this Description ships in every
// manifest and any variance would bust the OpenRouter implicit cache (T-08.1-07).
// Only source names and (sorted) tool names flow in — no raw multi-line tool
// description is concatenated (T-08.1-08). A nil registry yields the "None"-blurb.
func toolSearchDescription(reg *Registry) string {
	return toolSearchLeadIn + sourceOrientation(reg)
}

func sourceOrientation(reg *Registry) string {
	const builtin = "built-in"
	sources := map[string][]string{}
	if reg != nil {
		for _, t := range reg.All() {
			// Skip tool_search itself BEFORE calling Spec(): tool_search's Spec()
			// re-enters sourceOrientation (this function builds its Description), so
			// materializing its Spec here recurses unboundedly. It is non-deferred
			// anyway, so it would be filtered out regardless.
			if _, ok := t.(*ToolSearch); ok {
				continue
			}
			s := t.Spec()
			if !s.Deferred {
				continue
			}
			src := builtin
			if pre, _, ok := strings.Cut(s.Name, nsDelimiterStr); ok && pre != "" {
				src = pre
			}
			sources[src] = append(sources[src], s.Name)
		}
	}
	if len(sources) == 0 {
		return "None currently enabled.\n"
	}
	names := make([]string, 0, len(sources))
	for src := range sources {
		names = append(names, src)
	}
	sort.Strings(names)
	var b strings.Builder
	for _, src := range names {
		tools := sources[src]
		sort.Strings(tools)
		fmt.Fprintf(&b, "- %s: %s\n", src, strings.Join(tools, ", "))
	}
	return b.String()
}

func (ts *ToolSearch) Execute(ctx context.Context, raw json.RawMessage) (ToolResult, error) {
	var args toolSearchArgs
	if err := json.Unmarshal(raw, &args); err != nil {
		return ToolResult{}, fmt.Errorf("tool_search args: %w", err)
	}
	q := strings.TrimSpace(args.Query)
	if q == "" {
		return ToolResult{}, fmt.Errorf("tool_search: query is required")
	}

	limit := defaultMaxResults
	if args.MaxResults != nil {
		if *args.MaxResults <= 0 {
			return ToolResult{}, fmt.Errorf("tool_search: max_results must be greater than zero")
		}
		limit = *args.MaxResults
	}

	matches := ts.match(q, limit)
	if len(matches) == 0 {
		// Orientation tail (amendment #49): the no-result moment IS the capability
		// gap — route the model into the skills system instead of ad-hoc code. The
		// string is a fixed literal (per-turn result, not a manifest surface; no
		// cache invariant rides on it).
		return NewResult(ctx, noMatchOrientation)
	}

	var b strings.Builder
	for _, t := range matches {
		s := t.Spec()
		fmt.Fprintf(&b, "## %s\n%s\n\nParameters:\n%s\n\n", s.Name, s.Description, string(s.Parameters))
	}
	// A select of many large deferred specs can exceed the preview cap; route
	// through the shared spillover helper so big manifests page via the sidecar.
	return NewResult(ctx, b.String())
}

// match resolves a query to tools. The `select:` path resolves any registered
// tool by exact name and ignores limit (uncapped). The keyword path ranks
// deferred-only tools by BM25 and returns the top-limit positive-scoring matches.
func (ts *ToolSearch) match(q string, limit int) []Tool {
	if sel, ok := strings.CutPrefix(q, "select:"); ok {
		names := strings.Split(sel, ",")
		out := make([]Tool, 0, len(names))
		for _, n := range names {
			n = strings.TrimSpace(n)
			if t, ok := ts.Registry.Get(n); ok {
				out = append(out, t)
			}
		}
		return out
	}

	ts.buildIndex()
	ranked := ts.index.rank(q)
	if len(ranked) > limit {
		ranked = ranked[:limit]
	}
	out := make([]Tool, 0, len(ranked))
	for _, r := range ranked {
		out = append(out, ts.deferred[r.doc])
	}
	return out
}

// buildIndex memoizes the BM25 index over the registry's deferred tools. The
// Registry is immutable for the run (spec.go), so the index is built once and
// read lock-free thereafter; sync.Once makes the lazy build race-safe.
func (ts *ToolSearch) buildIndex() {
	ts.once.Do(func() {
		all := ts.Registry.All()
		// Registry.All iterates a map (random order); sort by Name so equal-score
		// ties break deterministically and top-K selection is stable per run.
		sort.Slice(all, func(i, j int) bool { return all[i].Spec().Name < all[j].Spec().Name })
		var specs []Spec
		for _, t := range all {
			s := t.Spec()
			if !s.Deferred {
				continue
			}
			ts.deferred = append(ts.deferred, t)
			specs = append(specs, s)
		}
		ts.index = newBM25Index(specs)
	})
}
