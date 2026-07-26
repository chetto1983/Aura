package tools

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"sync"

	"github.com/chetto1983/aura/internal/semindex"
)

// searchDocumentHash is the per-tool content fingerprint the bank keys on so an
// MCP reconnect that re-advertises a tool with a CHANGED description is detected
// and re-embedded (AG-020), rather than silently keeping the stale vector.
func searchDocumentHash(s Spec) string {
	sum := sha256.Sum256([]byte(searchDocument(s)))
	return hex.EncodeToString(sum[:8])
}

// ToolSearch is the built-in hook tool that lets the LLM fetch full specs of
// deferred tools. The pattern mirrors Claude Code's ToolSearch behavior: the
// model sees only Name+Summary by default, then calls `tool_search` with a
// `select:<name>,<name>` argument or a free-text query to load the full
// Description+Parameters into context.
//
// Free-text queries are ranked SEMANTICALLY by granite-embedding cosine
// (semindex.Ranker, PerItem) over an expanded per-tool search document (the same
// flattened searchDocument BM25 builds — D-02, spike-056 "bm25doc input"), then
// capped to max_results. BM25 contributes only as a guarded, intersection-gated
// tiebreak (search_fusion.go). The `select:` path resolves any registered tool by
// exact name and stays uncapped — a different mechanism (load-by-name).
//
// This is a NON-DEFERRED tool — always visible — so the model can find it
// without recursion. The embed sidecar is a HARD dependency for free-text ranking
// (Req-6): with it down, Execute returns an explicit model-visible error (never an
// empty/garbage ranking). The `select:` path needs no embedder.
type ToolSearch struct {
	Registry *Registry

	// Embed is the granite-embedding seam used to embed the per-tool search docs
	// and the query (semindex.Embedder; documents.EmbeddingClient satisfies it with
	// no adapter). Wired by the composition root (runner). nil => free-text ranking
	// returns an explicit error (Req-6); the select: path still works.
	Embed semindex.Embedder
	// Adaptive coordinates typed assignment and delivery for free-text discovery.
	// Exact select: calls never cross this port.
	Adaptive ToolDiscoveryControl

	mu     sync.Mutex
	ranker *semindex.Ranker // embedding bank over the deferred searchDocument texts
	bm25   *bm25Index       // kept for the guarded tiebreak (search_fusion.go)
	// banked maps each embedded tool Name to its searchDocument hash, so the
	// MCP-mount refresh (InvalidateIndex) can embed ONLY new tools (D-03) AND detect
	// when an EXISTING tool's description changed on reconnect (AG-020): a changed
	// hash triggers a full ranker rebuild so the stale vector is replaced. New tools
	// stay an incremental append; only an actual description change pays a rebuild.
	banked map[string]string
	specs  []Spec // bm25-aligned deferred specs (sorted by Name)

}

const defaultMaxResults = 5

// InvalidateIndex signals that the deferred tool set may have changed (an MCP
// reconnect advertised new specs). It stays a ZERO-ARG signal (the bridge seam is
// unchanged): ToolSearch detects the delta itself on the next search by diffing the
// registry's deferred names against the embedded bank, and embeds ONLY the additions
// (D-03 incremental re-embed — exactly N new Embed calls per N-tool mount). The BM25
// tiebreak index is dropped so it rebuilds over the current deferred set.
func (ts *ToolSearch) InvalidateIndex() {
	ts.mu.Lock()
	defer ts.mu.Unlock()
	// The embedding bank survives invalidation: the next search embeds only the
	// newly-advertised tools and appends them. Only the BM25 index (cheap to rebuild)
	// and the bm25-aligned spec slice are dropped.
	//
	// WR-01 FIX (AG-020): the bank now keys each tool Name to its searchDocument
	// HASH. ensureBank compares the live hash against the banked hash on the next
	// search; a tool whose description CHANGED on reconnect (same Name, new doc)
	// triggers a one-time full ranker rebuild so its stale vector is replaced. New
	// tools (new Name) still embed incrementally (D-03); only an actual
	// description-change pays the rebuild, which is reconnect-rare.
	ts.bm25 = nil
	ts.specs = nil
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
      "description": "Either 'select:Name1,Name2' to load specific tools by name, or a natural-language phrase ranked by semantic similarity over deferred tool names, descriptions, and parameters. Example: a phrase like 'search the current web for prices or news' or 'send a file to the user'; or 'select:web_fetch,web_search' to load specific tools by exact name."
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
	"Searches deferred-tool metadata by semantic similarity and loads matching tool schemas for the next model call. " +
	"Use 'select:Name1,Name2' for direct selection, or a natural-language phrase ranked over deferred tool names, descriptions, and parameters. " +
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
// INFRA-ERROR path (embed sidecar down), which returns an explicit error from Execute.
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
// until tool_search loads it here). The no-match orientation path and the embed-sidecar
// infra-error path load nothing and therefore set no promotion Meta.
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

	var matches []Tool
	var err error
	if strings.HasPrefix(q, "select:") {
		matches, err = ts.match(ctx, q, limit)
	} else {
		matches, err = ts.orderFreeText(ctx, q, limit)
	}
	if err != nil {
		// Req-6 INFRA-ERROR path: the embed sidecar is a hard dependency for free-text
		// ranking — surface an explicit, model-visible, retryable error. This is
		// DISTINCT from the capability-gap path below (no tool matched): a down sidecar
		// must never masquerade as "no matching tools" or an empty/garbage ranking.
		return ToolResult{}, fmt.Errorf("tool_search: semantic ranking unavailable (embed sidecar): %w", err)
	}
	if len(matches) == 0 {
		// Orientation tail (amendment #49): the no-result moment IS the capability
		// gap — route the model into the skills system instead of ad-hoc code. The
		// string is a fixed literal (per-turn result, not a manifest surface; no
		// cache invariant rides on it).
		return NewResult(ctx, noMatchOrientation)
	}

	var b strings.Builder
	names := make([]string, 0, len(matches))
	for _, t := range matches {
		s := t.Spec()
		names = append(names, s.Name)
		fmt.Fprintf(&b, "## %s\n%s\n\nParameters:\n%s\n\n", s.Name, s.Description, string(s.Parameters))
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
	// orientation path (nothing loaded) nor the infra-error path (they return earlier).
	// This is the schema-load-before-call parity: a deferred tool becomes callable next
	// turn only after tool_search has loaded its schema here.
	if res.Meta == nil {
		m := ToolResultMeta{}
		res.Meta = &m
	}
	(*res.Meta)[MetaActivatedTools] = names
	return res, nil
}

// match resolves a query to tools. The `select:` path resolves any registered tool
// by exact name and ignores limit (uncapped) — UNCHANGED, needs no embedder. The
// free-text path ranks deferred-only tools by granite-embedding cosine (semindex)
// with a guarded BM25 tiebreak and returns the top-limit matches. A non-nil error
// is the embed-sidecar-down INFRA error (Req-6), distinct from an empty result
// (capability gap).
func (ts *ToolSearch) match(ctx context.Context, q string, limit int) ([]Tool, error) {
	if sel, ok := strings.CutPrefix(q, "select:"); ok {
		names := strings.Split(sel, ",")
		out := make([]Tool, 0, len(names))
		for _, n := range names {
			n = strings.TrimSpace(n)
			if t, ok := ts.Registry.Get(n); ok {
				out = append(out, t)
			}
		}
		return out, nil
	}

	frozen := ts.freezeDeferredTools()
	return ts.rankFrozen(ctx, frozen, q, limit, ToolDiscoveryStatic)
}

// embedQuery embeds a single free-text query into one vector, surfacing the embedder
// error verbatim (Req-6). It is the single embed per tool_search call — reused by both
// ranking stages.
func (ts *ToolSearch) embedQuery(ctx context.Context, q string) ([]float64, error) {
	vecs, err := ts.Embed.Embed(ctx, []string{q})
	if err != nil {
		return nil, err
	}
	if len(vecs) != 1 || len(vecs[0]) == 0 {
		return nil, nil
	}
	return vecs[0], nil
}

// ensureBank lazily builds (and incrementally extends) the embedding bank over the
// frozen deferred tools, then returns the ranker and BM25-ranked tool names for
// the query (the guarded-tiebreak signal). The embedding
// input is searchDocument(spec) (reused VERBATIM from bm25.go — D-02, NOT the raw
// description). The bank is keyed by tool Name. On the first call it embeds every
// deferred tool; on later calls (after an MCP-mount InvalidateIndex) it embeds ONLY
// the delta — tools whose Name is not yet banked — and appends them (D-03: exactly N
// new Embed calls per N-tool mount). The BM25 tiebreak index is (re)built over the
// current deferred set. A nil embedder or an embed error returns an error (Req-6 hard
// dependency), never a silent empty bank. The BM25 query rank is computed under the
// lock so it stays aligned with the (sorted) spec slice.
func (ts *ToolSearch) ensureBank(
	ctx context.Context,
	query string,
	frozen frozenToolCatalog,
) (*semindex.Ranker, []string, error) {
	ts.mu.Lock()
	defer ts.mu.Unlock()

	// Deferred tools, sorted by Name so equal-score ties break deterministically and
	// the BM25 index alignment is stable per run (mirror of the prior indexSnapshot).
	specs := frozen.specs
	// Empty corpus is a CAPABILITY GAP, not an infra error: there is nothing to embed
	// or rank regardless of the embedder, so do not require the sidecar here (a
	// nil ranker drives match's empty -> noMatchOrientation path).
	if len(specs) == 0 {
		return nil, nil, nil
	}

	// With deferred tools present, the embed sidecar IS required (Req-6 hard dep).
	if ts.Embed == nil {
		return nil, nil, fmt.Errorf("no embedder wired")
	}
	if ts.ranker == nil {
		ts.ranker = semindex.NewRanker(ts.Embed)
		ts.banked = make(map[string]string)
	}

	// AG-020: detect an EXISTING tool whose description (searchDocument) changed
	// since it was banked — an MCP reconnect can re-advertise the same Name with a
	// new description. Because the shared semindex.Ranker is append-only (no replace),
	// a changed hash forces a full rebuild of the bank so the stale vector is dropped.
	// This pays a full re-embed only on an actual description change (rare, reconnect
	// only); the common new-tool path stays the incremental append below.
	changed := false
	for _, s := range specs {
		if h, ok := ts.banked[s.Name]; ok && h != searchDocumentHash(s) {
			changed = true
			break
		}
	}
	if changed {
		ts.ranker = semindex.NewRanker(ts.Embed)
		ts.banked = make(map[string]string)
	}

	// Embed ONLY the delta — tools not yet in the bank by Name (D-03 incremental append).
	var newSpecs []Spec
	for _, s := range specs {
		if _, ok := ts.banked[s.Name]; !ok {
			newSpecs = append(newSpecs, s)
		}
	}
	if len(newSpecs) > 0 {
		items := make([]semindex.Item, 0, len(newSpecs))
		texts := make([]string, 0, len(newSpecs))
		for _, s := range newSpecs {
			texts = append(texts, searchDocument(s))
		}
		vecs, err := ts.Embed.Embed(ctx, texts)
		if err != nil {
			return nil, nil, err // Req-6: not cached — a retry re-embeds the delta
		}
		for i, s := range newSpecs {
			if i < len(vecs) {
				items = append(items, semindex.Item{Label: s.Name, Vec: vecs[i]})
			}
		}
		ts.ranker.AddVecs(items...)
		for _, s := range newSpecs {
			ts.banked[s.Name] = searchDocumentHash(s)
		}
	}

	if ts.bm25 == nil {
		ts.bm25 = newBM25Index(specs)
		ts.specs = specs
	}
	// BM25-ranked NAMES for the query (the guarded-tiebreak signal). doc indices map
	// back to ts.specs (bm25-aligned). Computed here, under the lock, so the index and
	// the spec slice can never drift apart.
	scored := ts.bm25.rank(query)
	bm25Names := make([]string, 0, len(scored))
	for _, s := range scored {
		if s.doc < len(ts.specs) {
			bm25Names = append(bm25Names, ts.specs[s.doc].Name)
		}
	}
	return ts.ranker, bm25Names, nil
}
