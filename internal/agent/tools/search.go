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
		Description: "Fetches deferred-tool schemas so they become callable. Use 'select:Name1,Name2' for direct selection, or a keyword phrase to discover relevant tools ranked by BM25 over their names, descriptions, and parameters.",
		Parameters:  params,
		Deferred:    false,
	}
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
		return NewResult(ctx, "no matching tools")
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
