package tools

import (
	"slices"
	"strings"
	"testing"
)

// A key that names no tool indexes nothing and fails silently: a rename of the tool
// would quietly drop its vocabulary. Every built-in key must be a live Spec name.
func TestRetrievalKeywordsNameRealTools(t *testing.T) {
	builtins := []string{
		CurrentTime{}.Spec().Name, (&TaskTool{}).Spec().Name, (&TodoTool{}).Spec().Name,
		(&WebSearch{}).Spec().Name, (&WebFetch{}).Spec().Name, (&SwarmSpawn{}).Spec().Name,
		(&SwarmStatus{}).Spec().Name, (&SkillManageTool{}).Spec().Name, (&PackTool{}).Spec().Name,
		(&ImageGenerate{}).Spec().Name, (&VideoGenerate{}).Spec().Name, (&ShellPoll{}).Spec().Name,
		(&ShellKill{}).Spec().Name,
	}
	for name := range retrievalKeywords {
		if strings.HasPrefix(name, "memory"+nsDelimiterStr) {
			continue
		}
		if !slices.Contains(builtins, name) {
			t.Errorf("retrievalKeywords key %q is not a built-in tool name", name)
		}
	}
	for _, name := range builtins {
		if _, ok := retrievalKeywords[name]; !ok {
			t.Errorf("built-in %q has no retrieval keywords", name)
		}
	}
}

// A keyword is matched only as the single token tokenize makes of it. An accent
// splits it, a stop word drops it, and a plural folds it, so each must come out of
// the tokenizer as exactly itself.
func TestRetrievalKeywordsSurviveTokenize(t *testing.T) {
	for name, words := range retrievalKeywords {
		for _, w := range words {
			if got := tokenize(w); len(got) != 1 || got[0] != w {
				t.Errorf("%s keyword %q tokenizes to %q, want itself", name, w, got)
			}
		}
	}
}

func TestSearchDocumentIndexesKeywordsByName(t *testing.T) {
	doc := searchDocument(Spec{Name: "task", Summary: "scheduler"})
	if !strings.Contains(doc, "promemoria") {
		t.Fatalf("task retrieval document lacks its keywords: %q", doc)
	}
	if strings.Contains(searchDocument(Spec{Name: "unlisted_tool"}), "promemoria") {
		t.Fatal("a tool without keywords borrowed another tool's vocabulary")
	}
}

// The two discovery queries that failed on the live stack on 2026-10-06, with Claude
// standing in as the model through a synthetic endpoint. They are NOT production traffic
// and so stay out of gateCases; they pin the misses this vocabulary was written to close.
func TestRetrievalKeywordsRecoverLiveStackMisses(t *testing.T) {
	specs := loadManifestFixture(t)
	idx := newBM25Index(specs)
	for _, c := range []gateCase{
		{"programmare un promemoria per domani mattina", []string{"task"}}, //nolint:misspell // Italian "to schedule".
		{"look up the current price of something online", []string{"web_search"}},
	} {
		ranked := idx.rank(c.query)
		if len(ranked) == 0 || !isGold(specs[ranked[0].doc].Name, c.gold) {
			got := "(nothing)"
			if len(ranked) > 0 {
				got = specs[ranked[0].doc].Name
			}
			t.Errorf("%q ranked %s first, want %v", c.query, got, c.gold)
		}
	}
}

// An empty ranking is a vocabulary miss before it is a capability gap: the reply must
// send the model to the roster of deferred names before it suggests installing a skill.
func TestNoMatchOrientationPointsAtRosterBeforeSkills(t *testing.T) {
	roster := strings.Index(noMatchOrientation, "<deferred_tools>")
	skills := strings.Index(noMatchOrientation, "find-skills-aura")
	if roster < 0 || skills < 0 || roster > skills {
		t.Fatalf("no-match reply must name <deferred_tools> before find-skills-aura: %q", noMatchOrientation)
	}
	if strings.Contains(noMatchOrientation, "recurring workflows") {
		t.Fatal("no-match reply routes recurring work to skill install; that is the task tool")
	}
}
