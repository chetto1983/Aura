package tools

import (
	"context"
	"encoding/json"
	"strings"
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

// TestToolSearch_KeywordMatchesSummaryOnly: a keyword present in the Summary but
// NOT the Name still selects the tool. Kills the `|| summary-match`→`|| false`
// mutant (which would drop summary-only matches).
func TestToolSearch_KeywordMatchesSummaryOnly(t *testing.T) {
	ts, ctx := newSearch(t, searchableTool{name: "weatherbot", summary: "fetches forecasts"})
	res, err := ts.Execute(ctx, []byte(`{"query":"forecasts"}`))
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !strings.Contains(res.Preview, "weatherbot") {
		t.Fatalf("summary-only keyword did not match: %q", res.Preview)
	}
	if strings.Contains(res.Preview, "no matching tools") {
		t.Fatal("summary-only keyword wrongly reported no match (the summary half of the OR was dropped)")
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
