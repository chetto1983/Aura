package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"hash/fnv"
	"math"
	"regexp"
	"strings"
	"sync"
	"testing"
)

// bagEmbedder is a deterministic, sidecar-free fake Embedder used across the
// tool_search tests. It embeds text as an L2-normalized bag-of-words vector over a
// fixed hashed vocabulary: each lowercase alphanumeric token sets one dimension, so
// the cosine between a query and a tool's searchDocument is high exactly when they
// SHARE tokens. This preserves the keyword-test semantics under the semantic ranker
// (a query word present in a tool's Name/Description ranks that tool high) WITHOUT a
// granite sidecar, keeping CI free and deterministic (RESEARCH Wave-0 fixture). The
// `calls` counter is how Req-3 asserts "exactly N new embeds"; `failQuery`/`failUntil`
// drive the Req-6 error-surface and transient-recovery tests.
type bagEmbedder struct {
	mu        sync.Mutex
	calls     int  // total Embed invocations (per-call, regardless of batch size)
	embedded  int  // total texts embedded across all calls (the "N tools" lever)
	failUntil int  // error for the first failUntil calls (transient-recovery)
	failQuery bool // error on every call (Req-6 hard-down sidecar)
}

const bagEmbedDims = 1024

func (f *bagEmbedder) Embed(_ context.Context, texts []string) ([][]float64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	if f.failQuery || f.calls <= f.failUntil {
		return nil, errors.New("embed sidecar down")
	}
	f.embedded += len(texts)
	out := make([][]float64, len(texts))
	for i, t := range texts {
		out[i] = bagVec(t)
	}
	return out, nil
}

// bagVec is the deterministic bag-of-words vector: one hashed dimension per token,
// L2-normalized (so cosine = shared-token overlap). Token set mirrors bm25.tokenize.
func bagVec(text string) []float64 {
	v := make([]float64, bagEmbedDims)
	for _, tok := range tokenize(text) {
		h := fnv.New32a()
		_, _ = h.Write([]byte(tok))
		v[h.Sum32()%bagEmbedDims] += 1
	}
	var n float64
	for _, x := range v {
		n += x * x
	}
	if n == 0 {
		return v
	}
	n = math.Sqrt(n)
	for i := range v {
		v[i] /= n
	}
	return v
}

// searchableTool is a deferred tool whose Name and Summary share NO common
// substring, so a keyword can match exactly one of the two fields. This makes
// the `name || summary` OR-branch in ToolSearch.match observable: a name-only
// query and a summary-only query each select the tool only if both sides of the
// OR are evaluated.
type searchableTool struct {
	name, summary string
}

func (s searchableTool) Spec() Spec {
	return Spec{
		Name: s.name, Summary: s.summary, Description: "full description of " + s.name,
		Parameters: json.RawMessage(`{"type":"object"}`), Deferred: true,
	}
}
func (searchableTool) Execute(context.Context, json.RawMessage) (ToolResult, error) {
	return ToolResult{}, nil
}

func newSearch(t *testing.T, tools ...Tool) (*ToolSearch, context.Context) {
	t.Helper()
	ts, _, ctx := newSearchWithEmbedder(t, tools...)
	return ts, ctx
}

// newSearchWithEmbedder builds a ToolSearch wired to a deterministic bagEmbedder and
// returns the embedder too, so a test can assert the embed-call count (Req-3) or the
// error surface (Req-6) by flipping its fail flags.
func newSearchWithEmbedder(t *testing.T, tools ...Tool) (*ToolSearch, *bagEmbedder, context.Context) {
	t.Helper()
	reg := NewRegistry()
	for _, tl := range tools {
		reg.Register(tl)
	}
	emb := &bagEmbedder{}
	return &ToolSearch{Registry: reg, Embed: emb}, emb, ctxWith(t, "sess-s", "call-s")
}

func TestToolSearchSpecIsVisibleAndSchemaIsValid(t *testing.T) {
	s := (&ToolSearch{}).Spec()
	if s.Name != "tool_search" {
		t.Fatalf("name = %q, want tool_search", s.Name)
	}
	if s.Deferred {
		t.Fatal("tool_search must stay non-deferred so deferred tools are discoverable")
	}
	var schema map[string]any
	if err := json.Unmarshal(s.Parameters, &schema); err != nil {
		t.Fatalf("parameters are not valid JSON schema: %v", err)
	}
}

// TestToolSearch_KeywordMatchesNameOnly: a keyword present in the Name but NOT
// the Summary still selects the tool. Kills the `name-match ||`→`false ||`
// mutant (which would drop name-only matches).
func TestToolSearch_KeywordMatchesNameOnly(t *testing.T) {
	ts, ctx := newSearch(t, searchableTool{name: "weatherbot", summary: "fetches forecasts"})
	res, err := ts.Execute(ctx, []byte(`{"query":"weatherbot"}`))
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !strings.Contains(res.Preview, "weatherbot") {
		t.Fatalf("name-only keyword did not match: %q", res.Preview)
	}
	if strings.Contains(res.Preview, "no matching tools") {
		t.Fatal("name-only keyword wrongly reported no match (the name half of the OR was dropped)")
	}
}

// TestToolSearch_KeywordMatchesDescriptionOnly: a keyword present in the
// Description but NOT the Name still selects the tool. Under D-02 the BM25 search
// document indexes Name + name-spaced + Description + recursive params — NOT the
// Summary field (Codex's default_tool_search_text indexes description, not a
// separate summary). This replaces the prior substring "SummaryOnly" assertion:
// the searchable secondary field is now Description, per the BM25 contract. Kills
// the "drop the Description from the search document" mutant.
func TestToolSearch_KeywordMatchesDescriptionOnly(t *testing.T) {
	ts, ctx := newSearch(t,
		searchableTool{name: "weatherbot", summary: "s1"},
		searchableTool{name: "ledger", summary: "s2"},
	)
	// "weatherbot"'s Description is "full description of weatherbot"; query a
	// distinctive Description word that is NOT in either Name.
	res, err := ts.Execute(ctx, []byte(`{"query":"description weatherbot"}`))
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !strings.Contains(res.Preview, "weatherbot") {
		t.Fatalf("description keyword did not match: %q", res.Preview)
	}
	if strings.Contains(res.Preview, "no matching tools") {
		t.Fatal("description keyword wrongly reported no match (Description dropped from the search document)")
	}
}

// TestToolSearch_SelectTrimsAndAccumulates: the `select:` path trims whitespace
// around each name and appends every resolved tool's full spec. Asserting BOTH
// requested specs land (with surrounding spaces) kills the TrimSpace removal and
// the select-loop append-removal mutants.
func TestToolSearch_SelectTrimsAndAccumulates(t *testing.T) {
	ts, ctx := newSearch(t,
		searchableTool{name: "alpha_tool", summary: "a"},
		searchableTool{name: "beta_tool", summary: "b"},
	)
	// Deliberate surrounding whitespace around each name.
	res, err := ts.Execute(ctx, []byte(`{"query":"select: alpha_tool , beta_tool "}`))
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !strings.Contains(res.Preview, "## alpha_tool") {
		t.Fatalf("select did not resolve the whitespace-padded alpha_tool: %q", res.Preview)
	}
	if !strings.Contains(res.Preview, "## beta_tool") {
		t.Fatalf("select did not accumulate beta_tool: %q", res.Preview)
	}
}

// nonDeferredTool is a registered tool with Deferred=false — keyword search must
// NEVER surface it (D-03), but select: by exact name MUST resolve it (D-05).
type nonDeferredTool struct{ name string }

func (n nonDeferredTool) Spec() Spec {
	return Spec{
		Name: n.name, Summary: "active capability " + n.name, Description: "full desc " + n.name,
		Parameters: json.RawMessage(`{"type":"object"}`), Deferred: false,
	}
}
func (nonDeferredTool) Execute(context.Context, json.RawMessage) (ToolResult, error) {
	return ToolResult{}, nil
}

func countHeadings(preview string) int {
	return strings.Count(preview, "## ")
}

// TestMaxResults: a free-text query over >5 matching deferred tools returns
// exactly 5 by default; max_results=2 returns 2; max_results=0 is rejected with a
// model-facing "greater than zero" error; select: is NOT capped (D-05). Kills the
// default-K, the zero-rejection, and the select-also-capped mutants.
func TestMaxResults(t *testing.T) {
	mk := func(i int) Tool {
		return bm25Tool{
			name: fmt.Sprintf("widget_%d", i),
			desc: "shared widget gadget capability",
		}
	}
	all := make([]Tool, 0, 8)
	for i := range 8 {
		all = append(all, mk(i))
	}
	ts, ctx := newSearch(t, all...)

	// Default cap = 5.
	res, err := ts.Execute(ctx, []byte(`{"query":"widget gadget"}`))
	if err != nil {
		t.Fatalf("Execute default: %v", err)
	}
	if got := countHeadings(res.Preview); got != 5 {
		t.Errorf("default max_results returned %d specs, want 5", got)
	}

	// Explicit cap = 2.
	res, err = ts.Execute(ctx, []byte(`{"query":"widget gadget","max_results":2}`))
	if err != nil {
		t.Fatalf("Execute max_results=2: %v", err)
	}
	if got := countHeadings(res.Preview); got != 2 {
		t.Errorf("max_results=2 returned %d specs, want 2", got)
	}

	// Zero is rejected with a model-facing error.
	if _, err := ts.Execute(ctx, []byte(`{"query":"widget gadget","max_results":0}`)); err == nil {
		t.Error("max_results=0 must be rejected, got nil error")
	} else if !strings.Contains(err.Error(), "greater than zero") {
		t.Errorf("max_results=0 error = %q, want it to contain 'greater than zero'", err.Error())
	}

	// Negative is rejected too (never index a negative slice).
	if _, err := ts.Execute(ctx, []byte(`{"query":"widget gadget","max_results":-3}`)); err == nil {
		t.Error("negative max_results must be rejected, got nil error")
	}

	// select: with >5 names stays UNCAPPED.
	sel := "select:"
	for i := range 8 {
		if i > 0 {
			sel += ","
		}
		sel += fmt.Sprintf("widget_%d", i)
	}
	res, err = ts.Execute(ctx, []byte(fmt.Sprintf(`{"query":%q,"max_results":2}`, sel)))
	if err != nil {
		t.Fatalf("Execute select: %v", err)
	}
	if got := countHeadings(res.Preview); got != 8 {
		t.Errorf("select: returned %d specs, want 8 (uncapped despite max_results=2)", got)
	}
}

// TestDeferredOnlyFilter: a keyword query never surfaces a non-deferred tool
// (D-03); a select: query DOES resolve a non-deferred tool by exact name (D-05).
func TestDeferredOnlyFilter(t *testing.T) {
	ts, ctx := newSearch(t,
		bm25Tool{name: "deferred_search_target", desc: "shared keyword token apples"},
		nonDeferredTool{name: "active_apples_tool"},
	)

	// Keyword "apples" matches both descriptions textually, but only the deferred
	// one may be returned.
	res, err := ts.Execute(ctx, []byte(`{"query":"apples"}`))
	if err != nil {
		t.Fatalf("Execute keyword: %v", err)
	}
	if strings.Contains(res.Preview, "active_apples_tool") {
		t.Errorf("keyword search surfaced a non-deferred tool: %q", res.Preview)
	}
	if !strings.Contains(res.Preview, "deferred_search_target") {
		t.Errorf("keyword search dropped the deferred match: %q", res.Preview)
	}

	// select: resolves the non-deferred tool by name.
	res, err = ts.Execute(ctx, []byte(`{"query":"select:active_apples_tool"}`))
	if err != nil {
		t.Fatalf("Execute select: %v", err)
	}
	if !strings.Contains(res.Preview, "## active_apples_tool") {
		t.Errorf("select: failed to resolve non-deferred tool: %q", res.Preview)
	}
}

// TestToolSearchDescriptionDeterministic asserts the registry-derived
// tool_search Description (D-09) is byte-stable across calls (cache-load-bearing:
// tool_search is non-deferred, so its full Description ships in the manifest every
// turn — any per-call variance busts the OpenRouter implicit cache, T-08.1-07),
// lists the deferred tools' sources SORTED and grouped by the "__" namespace prefix
// (else "built-in"), and falls back to a deterministic "None currently enabled."
// blurb when no deferred tools exist. Kills the "static Description" and the
// "unsorted source list" mutants.
func TestToolSearchDescriptionDeterministic(t *testing.T) {
	ts, _ := newSearch(t,
		bm25Tool{name: "web_fetch", desc: "retrieve a url"},
		bm25Tool{name: "github__create_issue", desc: "open an issue"},
		bm25Tool{name: "github__list_prs", desc: "list pull requests"},
		bm25Tool{name: "calculator", desc: "evaluate arithmetic"},
		nonDeferredTool{name: "active_cap"}, // non-deferred → must NOT appear as a source
	)

	first := ts.Spec().Description
	second := ts.Spec().Description
	if first != second {
		t.Fatalf("Description not byte-stable across calls:\n1: %q\n2: %q", first, second)
	}

	// Sources are grouped by the "__" prefix when present, else "built-in".
	for _, want := range []string{"built-in", "github"} {
		if !strings.Contains(first, want) {
			t.Errorf("Description missing source %q:\n%s", want, first)
		}
	}
	// The non-deferred tool's source name must NOT leak in (search covers deferred
	// tools only — D-03).
	if strings.Contains(first, "active_cap") {
		t.Errorf("Description leaked a non-deferred tool:\n%s", first)
	}

	// "built-in" sorts before "github": assert the source list ordering is
	// alphabetical, not registry/map order.
	if bi, gh := strings.Index(first, "built-in"), strings.Index(first, "github"); bi < 0 || gh < 0 || bi > gh {
		t.Errorf("sources not sorted (built-in must precede github):\n%s", first)
	}

	// No deferred tools → deterministic "None currently enabled." blurb.
	empty, _ := newSearch(t, nonDeferredTool{name: "only_active"})
	desc := empty.Spec().Description
	if !strings.Contains(desc, "None currently enabled.") {
		t.Errorf("empty-deferred Description missing 'None currently enabled.':\n%s", desc)
	}
	if desc != empty.Spec().Description {
		t.Error("empty-deferred Description not byte-stable")
	}

	// No clock/date shape may appear in the Description (cache-poisoning guard,
	// same rule as prompt.go).
	if loc := descClockShape.FindString(first); loc != "" {
		t.Errorf("Description contains a timestamp-shaped substring %q (cache-poisoning)", loc)
	}
}

// TestToolSearchDescription_SelfReferentialNoRecursion guards the production
// registry shape: tool_search registers ITSELF, and its Description is built by
// enumerating the registry — so sourceOrientation must skip tool_search before
// calling its Spec(), or Spec()→Description→sourceOrientation→Spec() recurses to a
// stack overflow. Registering the tool_search hook into the same registry it
// describes reproduces the boot path. Kills the "drop the *ToolSearch skip" mutant.
func TestToolSearchDescription_SelfReferentialNoRecursion(t *testing.T) {
	reg := NewRegistry()
	ts := &ToolSearch{Registry: reg}
	reg.Register(ts)
	reg.Register(bm25Tool{name: "github__create_issue", desc: "open an issue"})

	desc := ts.Spec().Description // must not recurse
	// tool_search must not appear in the SOURCE list (the "- " lines); the lead-in
	// legitimately mentions the `tool_search` hook by name, so scope the check.
	for _, line := range strings.Split(desc, "\n") {
		if strings.HasPrefix(line, "- ") && strings.Contains(line, "tool_search") {
			t.Errorf("Description listed tool_search as a source (it must be excluded): %q", line)
		}
	}
	if !strings.Contains(desc, "- github:") {
		t.Errorf("Description dropped the deferred source while excluding tool_search:\n%s", desc)
	}
}

// descClockShape matches a date/clock substring that would poison the cached
// manifest prefix if it crept into the tool_search Description.
var descClockShape = regexp.MustCompile(`\d{4}-\d{2}-\d{2}|\d{2}:\d{2}:\d{2}`)

// TestToolSearchConcurrentExecute exercises the sync.Once index memoization from
// many goroutines (run under -race). A data race in the lazy build trips here.
func TestToolSearchConcurrentExecute(t *testing.T) {
	ts, ctx := newSearch(t,
		bm25Tool{name: "web_fetch", desc: "retrieve a url"},
		bm25Tool{name: "calculator", desc: "evaluate arithmetic"},
	)
	var wg sync.WaitGroup
	for range 16 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := ts.Execute(ctx, []byte(`{"query":"fetch url"}`)); err != nil {
				t.Errorf("concurrent Execute: %v", err)
			}
		}()
	}
	wg.Wait()
}

// TestToolSearch_SemanticRanksByEmbedding: a free-text query ranks the tool whose
// searchDocument shares its tokens #1 under the semantic ranker (semindex over the
// bag-of-words embedding). Proves the keyword branch now routes through embedding,
// not BM25 — the headline behavior change.
func TestToolSearch_SemanticRanksByEmbedding(t *testing.T) {
	ts, ctx := newSearch(t,
		bm25Tool{name: "web_fetch", desc: "retrieve a url and render markdown"},
		bm25Tool{name: "calculator", desc: "evaluate arithmetic expressions"},
		bm25Tool{name: "calendar", desc: "list meetings and appointments"},
	)
	res, err := ts.Execute(ctx, []byte(`{"query":"arithmetic expressions","max_results":1}`))
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !strings.Contains(res.Preview, "## calculator") {
		t.Fatalf("semantic ranker did not put calculator #1 for 'arithmetic expressions': %q", res.Preview)
	}
}

// TestToolSearch_EmbedSidecarDownReturnsExplicitError (Req-6 INFRA-ERROR): with the
// embedder erroring, Execute returns a NON-NIL error mentioning the embed sidecar —
// NEVER the noMatchOrientation capability-gap text, NEVER an empty match list. The
// hard dependency must be model-visible and distinct from "no matching tools".
func TestToolSearch_EmbedSidecarDownReturnsExplicitError(t *testing.T) {
	ts, emb, ctx := newSearchWithEmbedder(t,
		bm25Tool{name: "web_fetch", desc: "retrieve a url"},
	)
	emb.failQuery = true

	res, err := ts.Execute(ctx, []byte(`{"query":"fetch a web page"}`))
	if err == nil {
		t.Fatalf("Execute with a down embedder returned nil error; got result %q", res.Preview)
	}
	if strings.Contains(res.Preview, "no matching tools") {
		t.Fatal("a down sidecar masqueraded as the capability-gap (noMatchOrientation) path")
	}
	if !strings.Contains(err.Error(), "embed") {
		t.Errorf("error does not name the embed dependency: %v", err)
	}
}

// TestToolSearch_NilEmbedderErrorsFreeTextButSelectWorks (Req-6 + D-02): with NO
// embedder wired, the free-text path returns an explicit error (it has no ranker),
// while the `select:` exact-name path still resolves tools (it needs no embedder).
func TestToolSearch_NilEmbedderErrorsFreeTextButSelectWorks(t *testing.T) {
	reg := NewRegistry()
	reg.Register(bm25Tool{name: "web_fetch", desc: "retrieve a url"})
	ts := &ToolSearch{Registry: reg} // no Embed
	ctx := ctxWith(t, "sess-nil", "call-nil")

	if _, err := ts.Execute(ctx, []byte(`{"query":"fetch a web page"}`)); err == nil {
		t.Error("free-text query with no embedder must error (hard dependency), got nil")
	}

	res, err := ts.Execute(ctx, []byte(`{"query":"select:web_fetch"}`))
	if err != nil {
		t.Fatalf("select: with no embedder must still work: %v", err)
	}
	if !strings.Contains(res.Preview, "## web_fetch") {
		t.Errorf("select: failed to resolve by exact name without an embedder: %q", res.Preview)
	}
}

// TestToolSearch_FusionKillSwitchOff (D-02a): AURA_TOOLSEARCH_FUSION=off falls back
// to pure embedding (the guard never fires). Ranking still works — the env only
// controls whether the BM25 tiebreak participates, never whether search functions.
func TestToolSearch_FusionKillSwitchOff(t *testing.T) {
	t.Setenv("AURA_TOOLSEARCH_FUSION", "off")
	ts, ctx := newSearch(t,
		bm25Tool{name: "web_fetch", desc: "retrieve a url and render markdown"},
		bm25Tool{name: "calculator", desc: "evaluate arithmetic"},
	)
	res, err := ts.Execute(ctx, []byte(`{"query":"render markdown","max_results":1}`))
	if err != nil {
		t.Fatalf("Execute with fusion off: %v", err)
	}
	if !strings.Contains(res.Preview, "## web_fetch") {
		t.Fatalf("pure-embedding (fusion off) did not rank web_fetch #1: %q", res.Preview)
	}
}
