package tools

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/aura/aura/internal/storage/memoryindex"
	"github.com/aura/aura/internal/storage/search"
	"github.com/aura/aura/internal/wiki"
)

// TestSearch_ActionSearch_HybridReturns verifies that action=search delegates
// to the search_memory backend and returns a non-empty result string.
func TestSearch_ActionSearch_HybridReturns(t *testing.T) {
	ctx := context.Background()
	index := newTestMemoryIndex(t)
	mem := NewSearchMemoryTool(nil, index)
	tool := NewSearchTool(mem, newTestWikiStore(t), nil)
	if tool == nil {
		t.Fatal("NewSearchTool returned nil")
	}
	out, err := tool.Execute(ctx, map[string]any{
		"action": "search",
		"query":  "contract deadline",
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	// Empty index → "No memory found" is a valid delegated result.
	if out == "" {
		t.Fatal("expected non-empty result from search delegation")
	}
}

// TestSearch_ActionList_PrefixMatch verifies that action=list with slug_prefix
// returns only wiki slugs matching the prefix.
func TestSearch_ActionList_PrefixMatch(t *testing.T) {
	ctx := context.Background()
	s := newTestWikiStore(t)
	seedPage(t, s, "Alpha One", "body alpha", nil, nil)
	seedPage(t, s, "Alpha Two", "body alpha2", nil, nil)
	seedPage(t, s, "Beta One", "body beta", nil, nil)

	tool := NewSearchTool(nil, s, nil)
	out, err := tool.Execute(ctx, map[string]any{
		"action":      "list",
		"slug_prefix": "alpha",
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}

	var result struct {
		Matches []struct {
			Slug string `json:"slug"`
			Kind string `json:"kind"`
		} `json:"matches"`
	}
	if err := json.Unmarshal([]byte(out), &result); err != nil {
		t.Fatalf("not JSON: %v\n%s", err, out)
	}
	if len(result.Matches) != 2 {
		t.Errorf("expected 2 alpha matches, got %d: %s", len(result.Matches), out)
	}
	for _, m := range result.Matches {
		if !strings.HasPrefix(m.Slug, "alpha") {
			t.Errorf("slug %q does not match prefix alpha", m.Slug)
		}
		if m.Kind != "wiki" {
			t.Errorf("slug %q has kind %q, want wiki", m.Slug, m.Kind)
		}
	}
}

// TestSearch_ActionRead_MarkdownCapsuleReturned verifies that action=read
// returns the wiki page as a compact markdown capsule for LLM-facing RAG.
func TestSearch_ActionRead_MarkdownCapsuleReturned(t *testing.T) {
	ctx := context.Background()
	s := newTestWikiStore(t)
	seedPage(t, s, "My Topic", "Body content here.", nil, nil)

	tool := NewSearchTool(nil, s, nil)
	out, err := tool.Execute(ctx, map[string]any{
		"action": "read",
		"slug":   "my-topic",
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}

	for _, want := range []string{
		"PAGE [[my-topic]] - My Topic",
		"updated_at=",
		"\n\nBody content here.",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("output missing %q:\n%s", want, out)
		}
	}
	if json.Valid([]byte(out)) {
		t.Fatalf("search(action=read) should return markdown, got JSON: %s", out)
	}
}

func TestSearch_ActionRead_FrontmatterLine(t *testing.T) {
	ctx := context.Background()
	s := newTestWikiStore(t)
	now := "2026-05-25T10:00:00Z"
	page := &wiki.Page{
		Title:         "Rich Topic",
		Body:          "Rich body.",
		Category:      "concept",
		Tags:          []string{"safety", "robot"},
		Related:       wiki.RelatedFromSlugs([]string{"alpha", "beta"}),
		Sources:       []string{"src_demo"},
		SchemaVersion: wiki.CurrentSchemaVersion,
		PromptVersion: "v1",
		CreatedAt:     now,
		UpdatedAt:     now,
	}
	if err := s.WritePage(ctx, page); err != nil {
		t.Fatalf("WritePage: %v", err)
	}
	tool := NewSearchTool(nil, s, nil)
	out, err := tool.Execute(ctx, map[string]any{
		"action": "read",
		"slug":   "rich-topic",
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}

	for _, want := range []string{
		"category=concept",
		"tags=safety,robot",
		"updated_at=2026-05-25T10:00:00Z",
		"related=alpha,beta",
		"sources=src_demo",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("metadata missing %q:\n%s", want, out)
		}
	}
}

func TestRenderWikiReadCapsuleOmitsEmptyFrontmatterLine(t *testing.T) {
	out := renderWikiReadCapsule("bare", &wiki.Page{Title: "Bare", Body: "Body."})
	if out != "PAGE [[bare]] - Bare\n\nBody." {
		t.Fatalf("out = %q, want no metadata line", out)
	}
}

func TestRenderWikiReadCapsuleIsSmallerThanOldJSON(t *testing.T) {
	page := &wiki.Page{
		Title:         "Quoted Topic",
		Body:          strings.Repeat("\"quoted\"\n", 80),
		Category:      "concept",
		Tags:          []string{"rag", "wiki"},
		Related:       wiki.RelatedFromSlugs([]string{"alpha", "beta"}),
		SchemaVersion: wiki.CurrentSchemaVersion,
		PromptVersion: "v1",
		CreatedAt:     "2026-05-25T10:00:00Z",
		UpdatedAt:     "2026-05-25T10:00:00Z",
	}
	capsule := renderWikiReadCapsule("quoted-topic", page)
	oldJSON := oldWikiReadJSONForTest(t, "quoted-topic", page)
	if len(capsule) > len(oldJSON)*80/100 {
		t.Fatalf("capsule len=%d old JSON len=%d, want at least 20%% smaller", len(capsule), len(oldJSON))
	}
}

func oldWikiReadJSONForTest(t *testing.T, slug string, page *wiki.Page) string {
	t.Helper()
	type fmResult struct {
		Category      string   `json:"category,omitempty"`
		Tags          []string `json:"tags,omitempty"`
		UpdatedAt     string   `json:"updated_at,omitempty"`
		SchemaVersion int      `json:"schema_version,omitempty"`
	}
	result := struct {
		Slug        string   `json:"slug"`
		Title       string   `json:"title"`
		Body        string   `json:"body"`
		Frontmatter fmResult `json:"frontmatter"`
	}{
		Slug:  slug,
		Title: page.Title,
		Body:  page.Body,
		Frontmatter: fmResult{
			Category:      page.Category,
			Tags:          page.Tags,
			UpdatedAt:     page.UpdatedAt,
			SchemaVersion: page.SchemaVersion,
		},
	}
	b, err := json.Marshal(result)
	if err != nil {
		t.Fatalf("marshal old JSON: %v", err)
	}
	return string(b)
}

// TestSearch_ZoneFilter verifies that zone=wiki only searches the wiki backend
// and does not surface source hits from the compact index.
func TestSearch_ZoneFilter(t *testing.T) {
	ctx := context.Background()
	// SearchMemoryTool with compact index only (wiki searcher is nil).
	// zone=wiki should route only to wiki search → unavailable warning, no source hits.
	index := newTestMemoryIndex(t)
	mem := NewSearchMemoryTool(nil, index)
	tool := NewSearchTool(mem, newTestWikiStore(t), nil)

	out, err := tool.Execute(ctx, map[string]any{
		"action": "search",
		"query":  "anything",
		"zone":   "wiki",
	})
	if err != nil {
		t.Fatalf("zone=wiki execute: %v", err)
	}
	// wiki searcher is nil → no wiki hits; result must not contain source kind hits.
	if strings.Contains(out, "[source]") {
		t.Errorf("zone=wiki result should not contain source hits:\n%s", out)
	}
}

// TestSearch_SearchMemoryInternalExecution verifies that SearchMemoryTool still
// executes without error when used as the internal delegate of SearchTool.
// search_memory is no longer registered in the LLM-facing tool catalog; it is
// an internal implementation detail of the unified "search" tool.
func TestSearch_SearchMemoryInternalExecution(t *testing.T) {
	ctx := context.Background()
	index := newTestMemoryIndex(t)
	tool := NewSearchMemoryTool(nil, index)

	out, err := tool.Execute(ctx, map[string]any{"query": "anything"})
	if err != nil {
		t.Fatalf("SearchMemoryTool.Execute (internal): %v", err)
	}
	if strings.Contains(out, "DEPRECATED") {
		t.Errorf("SearchMemoryTool output must not contain DEPRECATED hint (it is now internal-only):\n%s", out)
	}
}

// TestSearch_FoldedRecallActions verifies that the operational + user memory
// recall variants still serve through search(action="lessons"|"user_facts").
// Graph primitives (god_nodes, subgraph, path) are NOT folded — they live
// as standalone tools (recall_god_nodes, wiki_subgraph, wiki_path) per the
// graphify pattern. See those tools' own tests for coverage.
func TestSearch_FoldedRecallAndGraphActions(t *testing.T) {
	ctx := context.Background()
	now := time.Now().UTC()
	compact := fakeCompactMemorySearch{docs: []memoryindex.Document{
		operationalLesson("web-timeout", "web", "timeout", now),
		userFact("user_memory:pref", "preference", "Prefers dark mode", now),
	}}
	store := newSubgraphTestStore(t, map[string]*wiki.Page{
		"robot":  {Title: "Robot", Body: "Robot connects to [[frame]]."},
		"frame":  {Title: "Frame", Body: "Frame supports [[robot]]."},
		"island": {Title: "Island", Body: "Island has no graph links."},
	})
	tool := NewSearchTool(NewSearchMemoryTool(nil, compact), store, nil).
		WithRecallAndGraphActions(
			NewRecallOperationalTool(compact),
			NewRecallUserMemoryTool(compact),
			NewRecallGodNodesTool(store),
			NewWikiPathTool(store),
			NewWikiSubgraphTool(store, &fakeSubgraphSearcher{results: []search.Result{
				{Slug: "robot", Title: "Robot", Score: 0.95},
			}}),
			NewWikiDiffTool(store),
			NewWikiGapsTool(store),
			NewWikiSurprisesTool(store),
			NewWikiSuggestQuestionsTool(store),
		)

	cases := []struct {
		name string
		args map[string]any
		want string
	}{
		{name: "lessons", args: map[string]any{"action": "lessons", "tool_name": "web"}, want: "operational:web-timeout"},
		{name: "user facts", args: map[string]any{"action": "user_facts", "category": "preference"}, want: "user_memory:pref"},
		{name: "god nodes", args: map[string]any{"action": "god_nodes", "top_k": 2}, want: "robot"},
		{name: "path", args: map[string]any{"action": "path", "from_slug": "robot", "to_slug": "frame"}, want: `"path":["robot","frame"]`},
		{name: "subgraph", args: map[string]any{"action": "subgraph", "query": "robot", "depth": 1, "budget_tokens": 500}, want: "CAPSULE wiki_subgraph"},
		{name: "diff", args: map[string]any{"action": "diff", "since": "HEAD"}, want: `"summary":"no changes"`},
		{name: "gaps", args: map[string]any{"action": "gaps", "top_k": 2}, want: `"slugs":["island"]`},
		{name: "surprises", args: map[string]any{"action": "surprises", "top_k": 2}, want: `"confidence":"EXTRACTED"`},
		{name: "suggest questions", args: map[string]any{"action": "suggest_questions", "top_k": 2}, want: "suggested question(s)"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			out, err := tool.Execute(ctx, tc.args)
			if err != nil {
				t.Fatalf("Execute: %v", err)
			}
			if !strings.Contains(out, tc.want) {
				t.Fatalf("output missing %q:\n%s", tc.want, out)
			}
		})
	}
}

func TestSearchTool_SuggestQuestionsAction(t *testing.T) {
	ctx := context.Background()
	store := newSubgraphTestStore(t, map[string]*wiki.Page{
		"alpha": {
			Title:    "Alpha",
			Category: "project",
			Body:     "Alpha.",
			Related:  []wiki.RelatedRef{{Slug: "beta", Confidence: wiki.ConfidenceAmbiguous}},
		},
		"beta":   {Title: "Beta", Category: "person", Body: "Beta."},
		"bridge": {Title: "Bridge", Category: "system", Body: "[[entity-a]] [[concept-a]]"},
		"entity-a": {
			Title:    "Entity A",
			Category: "entity",
			Body:     "Entity.",
		},
		"concept-a": {
			Title:    "Concept A",
			Category: "concept",
			Body:     "Concept.",
		},
	})
	tool := NewSearchTool(nil, store, nil).
		WithRecallAndGraphActions(nil, nil, nil, nil, nil, nil, nil, nil, NewWikiSuggestQuestionsTool(store))

	out, err := tool.Execute(ctx, map[string]any{"action": "suggest_questions", "top_k": 4})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !strings.Contains(out, "suggested question(s) for the current wiki:") {
		t.Fatalf("unexpected capsule header:\n%s", out)
	}
	if !strings.Contains(out, "Use these question texts as the final answer") {
		t.Fatalf("output missing final-answer guidance:\n%s", out)
	}
	for _, want := range []string{
		"- [ambiguous_edge] What is the exact relationship between [[alpha]] and [[beta]]?",
		"- [bridge_node] Why does [[bridge]] connect concept to entity?",
		"why:",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("output missing %q:\n%s", want, out)
		}
	}
}

// TestSearch_GraphActionsErrorWhenDelegateMissing verifies that
// search(action="god_nodes"|"subgraph"|"path") returns an explicit error
// when the SearchTool was wired without the graph delegates (the
// production wiring path after 2026-05-24: graph primitives are
// standalone tools, NOT folded into search). Probes confirm the LLM
// reaches for the standalone recall_god_nodes / wiki_subgraph /
// wiki_path tools first; this test guards against silently succeeding
// against a half-wired SearchTool.
func TestSearch_GraphActionsErrorWhenDelegateMissing(t *testing.T) {
	ctx := context.Background()
	tool := &SearchTool{}
	for _, action := range []string{"god_nodes", "subgraph", "path", "diff", "gaps", "surprises", "suggest_questions"} {
		_, err := tool.Execute(ctx, map[string]any{"action": action})
		if err == nil {
			t.Fatalf("action=%q returned nil error, want unavailable error", action)
		}
		if !strings.Contains(err.Error(), "unavailable") {
			t.Fatalf("action=%q error should mention unavailable delegate: %v", action, err)
		}
	}
}
