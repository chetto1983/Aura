package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"sync"
	"testing"
)

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
	reg := NewRegistry()
	for _, tl := range tools {
		reg.Register(tl)
	}
	return &ToolSearch{Registry: reg}, ctxWith(t, "sess-s", "call-s")
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
