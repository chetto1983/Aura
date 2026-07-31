package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
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
// Resolution is exact names first, lexical ranking second (see match). Both are
// in-process and allocation-cheap: discovery costs no network call, so it cannot
// fail, cannot go stale against a sidecar, and cannot vary between two identical
// queries. It replaced an embedding ranker that cost 330-440 ms per query and
// answered 50% of the measured production queries correctly against this one's 96%
// — dense retrieval wins on corpora of hundreds of tools, and this one holds 54.
//
// This is a NON-DEFERRED tool — always visible — so the model can find it without
// recursion.
type ToolSearch struct {
	Registry *Registry

	mu     sync.Mutex
	bm25   *bm25Index      // lexical index over the deferred retrieval documents
	specs  []Spec          // bm25-aligned deferred specs (sorted by Name)
	byName map[string]Tool // bm25-aligned name -> tool
}

const defaultMaxResults = 5

// InvalidateIndex signals that the deferred tool set may have changed (an MCP
// reconnect advertised new specs). It stays a ZERO-ARG signal (the bridge seam is
// unchanged) and now costs one dropped pointer: the next search rebuilds the whole
// index over the current deferred set, which is a few hundred microseconds of pure
// CPU. The incremental-delta bookkeeping it used to carry existed only because a
// rebuild meant re-embedding every tool over the network.
func (ts *ToolSearch) InvalidateIndex() {
	ts.mu.Lock()
	defer ts.mu.Unlock()
	ts.bm25 = nil
	ts.specs = nil
	ts.byName = nil
}

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
      "description": "Either 'select:Name1,Name2' to load specific tools by name, or a natural-language phrase matched by keyword against deferred tool names, summaries, and argument names. Example: a phrase like 'search the current web for prices or news' or 'send a file to the user'; or 'select:web_fetch,web_search' to load specific tools by exact name."
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
	"Searches deferred-tool metadata by keyword and loads matching tool schemas for the next model call. " +
	"Use 'select:Name1,Name2' for direct selection — several names at once, uncapped — or a natural-language phrase ranked over deferred tool names, summaries, and argument names. " +
	"Some tools are not provided upfront; use this tool (`tool_search`) to discover and load them.\n\n" +
	"You have access to deferred tools from the following sources:\n"

// noMatchOrientation is the fixed no-result reply (amendment #49, retargeted #52/D-41).
// A failed tool_search is usually a capability gap; the always-on find-skills skill is the
// designed path for packaged capabilities, so the model is pointed there explicitly
// instead of being left to improvise ad-hoc code. The old `{"action":"catalog",...}`
// routing was removed: that action was deleted in 11-09 (#51/D-40), and discovery+install
// now ride the host terminal (`npx skills find/add`), taught by find-skills-aura.
//
// This is the CAPABILITY-GAP path (no tool matched the query) — distinct from the
// NAMING path (a select: named a tool that is not registered), which reports the bad
// name and the closest registered ones.
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
// Per-query ranking happens INSIDE Execute only and NEVER touches this Description
// (Req-7 cache invariant).
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

// Execute resolves the query to matching deferred specs and returns their full
// Description + Parameters as the result text. On a successful match it ALSO promotes
// the loaded tools: the matched names ride the result Meta under MetaActivatedTools,
// which the runner reads to make them callable (with their schema) on the next turn —
// the schema-load-before-call parity (a deferred tool is hidden from the callable set
// until tool_search loads it here). The no-match orientation and unknown-name paths
// load nothing and therefore set no promotion Meta.
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

	matches, unknown := ts.match(q, limit)
	if len(matches) == 0 {
		if len(unknown) > 0 {
			// NAMING error, not a capability gap: the tools the model asked for do
			// not exist under those names. Say which, and what does exist.
			return NewResult(ctx, ts.unknownNameReport(unknown))
		}
		// Orientation tail (amendment #49): the no-result moment IS the capability
		// gap — route the model into the skills system instead of ad-hoc code. The
		// string is a fixed literal (per-turn result, not a manifest surface; no
		// cache invariant rides on it).
		return NewResult(ctx, noMatchOrientation)
	}

	var b strings.Builder
	// A partially-good select: loads what exists AND reports what does not, FIRST:
	// several full specs overflow the preview cap and page out to the sidecar, so a
	// report appended after them is the part the model never reads.
	if len(unknown) > 0 {
		b.WriteString(ts.unknownNameReport(unknown))
		b.WriteString("\n")
	}
	names := make([]string, 0, len(matches))
	for _, t := range matches {
		s := t.Spec()
		names = append(names, s.Name)
		b.WriteString(RenderSpec(s))
	}
	// A select of many large deferred specs can exceed the preview cap; route
	// through the shared spillover helper so big manifests page via the sidecar.
	res, err := NewResult(ctx, b.String())
	if err != nil {
		return res, err
	}
	// Full-promotion parity: attach the loaded tool names on the result Meta so the
	// runner promotes them into the callable set (MetaActivatedTools). Set on BOTH the
	// select: and free-text success paths (they share this tail); NOT on the no-match
	// orientation or unknown-name paths, which return earlier having loaded nothing.
	// This is the schema-load-before-call parity: a deferred tool becomes callable next
	// turn only after tool_search has loaded its schema here.
	if res.Meta == nil {
		m := ToolResultMeta{}
		res.Meta = &m
	}
	(*res.Meta)[MetaActivatedTools] = names
	return res, nil
}

// RenderSpec is the one rendering of a deferred tool's schema for the model. It
// is shared because tool_search is no longer the only place a schema is handed
// over: the dispatch gate, which refuses a deferred tool called before its schema
// was read, answers with the schema itself rather than with instructions to go
// and fetch it — so both surfaces must produce the identical shape, or the model
// learns two formats for one thing.
func RenderSpec(s Spec) string {
	return fmt.Sprintf("## %s\n%s\n\nParameters:\n%s\n\n", s.Name, s.Description, string(s.Parameters))
}

// match resolves a query to tools, in two layers.
//
// Layer 1 is exact name resolution and is uncapped: `select:a,b`, and equally a
// bare phrase whose every token is a registered tool name — the model reads the
// full name list off this tool's own description, so it routinely writes the four
// names it wants as free text and must get those four tools, not a top-5 ranking.
// Names it got wrong come back as `unknown`, never silently dropped.
//
// Layer 2 is the BM25 ranking over deferred tools, capped to limit. There is no
// layer 3, and no error: discovery is in-process, so the only two outcomes are
// tools and an empty result, and an empty result is a capability gap.
func (ts *ToolSearch) match(q string, limit int) (matches []Tool, unknown []string) {
	if sel, ok := strings.CutPrefix(q, "select:"); ok {
		return ts.resolveNames(strings.Split(sel, ","))
	}
	if names, ok := ts.asNameList(q); ok {
		return ts.resolveNames(names)
	}
	return ts.rankFreeText(q, limit), nil
}

// asNameList reports whether every token of a free-text query is a registered tool
// name. It requires ALL of them: one ordinary word means the model wrote a
// capability phrase that happens to mention a tool, which is a ranking query.
func (ts *ToolSearch) asNameList(q string) ([]string, bool) {
	fields := strings.FieldsFunc(q, func(r rune) bool {
		return r == ',' || r == ' ' || r == '\t' || r == '\n'
	})
	if len(fields) == 0 {
		return nil, false
	}
	for _, f := range fields {
		if _, ok := ts.Registry.Get(f); !ok {
			return nil, false
		}
	}
	return fields, true
}

// resolveNames splits requested names into the tools that exist and the names that
// do not. Order follows the request so the model reads back what it asked for.
func (ts *ToolSearch) resolveNames(names []string) (found []Tool, unknown []string) {
	for _, n := range names {
		n = strings.TrimSpace(n)
		if n == "" {
			continue
		}
		if t, ok := ts.Registry.Get(n); ok {
			found = append(found, t)
			continue
		}
		unknown = append(unknown, n)
	}
	return found, unknown
}

// unknownNameReport explains that a requested name is not registered and offers the
// registered names closest to it. A misspelled name is a naming error the model can
// fix on the next call; routing it to the skills-install orientation instead is
// wrong advice, and it was given in production for two tools that existed.
func (ts *ToolSearch) unknownNameReport(unknown []string) string {
	var b strings.Builder
	for _, n := range unknown {
		fmt.Fprintf(&b, "%q is not a registered tool.", n)
		if near := ts.nearestNames(n); len(near) > 0 {
			fmt.Fprintf(&b, " Closest registered names: %s.", strings.Join(near, ", "))
		}
		b.WriteString("\n")
	}
	return b.String()
}

const nearestNameSuggestions = 3

// nearestNames ranks registered names by how many tokens they share with the
// requested one, reusing the ranker's own tokenizer — a misspelling usually keeps
// the namespace and the verb ("memory__memory_add_entty"), and that is exactly what
// token overlap recovers. Names sharing nothing are not suggested.
func (ts *ToolSearch) nearestNames(want string) []string {
	if ts.Registry == nil {
		return nil
	}
	wantTokens := tokenize(want)
	type scored struct {
		name    string
		overlap int
	}
	var candidates []scored
	for _, t := range ts.Registry.All() {
		name := t.Spec().Name
		if name == toolSearchName {
			continue
		}
		have := tokenize(name)
		overlap := 0
		for _, w := range wantTokens {
			if slices.Contains(have, w) {
				overlap++
			}
		}
		if overlap > 0 {
			candidates = append(candidates, scored{name: name, overlap: overlap})
		}
	}
	sort.SliceStable(candidates, func(a, b int) bool {
		if candidates[a].overlap != candidates[b].overlap {
			return candidates[a].overlap > candidates[b].overlap
		}
		return candidates[a].name < candidates[b].name
	})
	out := make([]string, 0, nearestNameSuggestions)
	for _, c := range candidates[:min(nearestNameSuggestions, len(candidates))] {
		out = append(out, c.name)
	}
	return out
}

// rankFreeText ranks the deferred tools lexically and returns the top-limit. The
// index is built once and reused until an MCP mount invalidates it, so a query
// costs a scan of 54 short documents and nothing else.
func (ts *ToolSearch) rankFreeText(query string, limit int) []Tool {
	ts.mu.Lock()
	defer ts.mu.Unlock()
	ts.ensureIndexLocked()
	if len(ts.specs) == 0 {
		return nil
	}
	ranked := ts.bm25.rank(query)
	if len(ranked) > limit {
		ranked = ranked[:limit]
	}
	out := make([]Tool, 0, len(ranked))
	for _, score := range ranked {
		if tool, ok := ts.byName[ts.specs[score.doc].Name]; ok {
			out = append(out, tool)
		}
	}
	return out
}

// ensureIndexLocked builds the deferred-tool index if it is missing. Specs are
// sorted by Name so equal scores break deterministically: the same query returns
// the same order for the lifetime of a registry.
func (ts *ToolSearch) ensureIndexLocked() {
	if ts.bm25 != nil {
		return
	}
	all := ts.Registry.All()
	sort.Slice(all, func(i, j int) bool {
		return all[i].Spec().Name < all[j].Spec().Name
	})
	ts.specs = nil
	ts.byName = make(map[string]Tool, len(all))
	for _, tool := range all {
		spec := tool.Spec()
		if !spec.Deferred {
			continue
		}
		ts.specs = append(ts.specs, spec)
		ts.byName[spec.Name] = tool
	}
	ts.bm25 = newBM25Index(ts.specs)
}
