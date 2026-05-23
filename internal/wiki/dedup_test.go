package wiki

import (
	"context"
	"slices"
	"testing"
)

// ─── helpers ─────────────────────────────────────────────────────────────────

// fakeDedupSearcher returns pre-seeded DedupHit lists keyed by the entity
// text that DeduplicateEntities will pass to SearchNeighbors (title when body
// is empty, otherwise title+" "+first200chars(body)).
type fakeDedupSearcher struct {
	byText map[string][]DedupHit
}

func (f *fakeDedupSearcher) SearchNeighbors(_ context.Context, entityText string, _ int) ([]DedupHit, error) {
	return f.byText[entityText], nil
}

// fakeDedupLLM records calls and returns a configurable response.
type fakeDedupLLM struct {
	response string
	calls    int
}

func (f *fakeDedupLLM) Ask(_ context.Context, _ string) (string, error) {
	f.calls++
	return f.response, nil
}

func makeEntityPage(title, category string) *Page {
	return &Page{
		Title:         title,
		Category:      category,
		Body:          title + " is a wiki entity used in dedup tests.",
		SchemaVersion: CurrentSchemaVersion,
		PromptVersion: "v1",
		CreatedAt:     "2026-01-01T00:00:00Z",
		UpdatedAt:     "2026-01-01T00:00:00Z",
	}
}

// ─── Union-Find unit test ─────────────────────────────────────────────────────

func TestDedupUnionFind_BasicClustering(t *testing.T) {
	u := newUF()
	for _, s := range []string{"a", "b", "c", "d"} {
		u.add(s)
	}
	u.union("a", "b")
	u.union("b", "c")
	// a, b, c in one cluster; d alone.
	if u.find("a") != u.find("b") || u.find("b") != u.find("c") {
		t.Error("a/b/c should be in same cluster")
	}
	if u.find("a") == u.find("d") {
		t.Error("d should be in its own cluster")
	}
	groups := u.groups()
	clusterSizes := make(map[int]int)
	for _, members := range groups {
		clusterSizes[len(members)]++
	}
	if clusterSizes[3] != 1 || clusterSizes[1] != 1 {
		t.Errorf("expected one 3-member and one 1-member cluster, got %v", groups)
	}
}

func TestDedupUnionFind_PathCompression(t *testing.T) {
	u := newUF()
	for _, s := range []string{"a", "b", "c", "d", "e"} {
		u.add(s)
	}
	u.union("a", "b")
	u.union("c", "d")
	u.union("a", "c")
	u.union("d", "e")
	// All five should be in one cluster.
	root := u.find("a")
	for _, s := range []string{"b", "c", "d", "e"} {
		if u.find(s) != root {
			t.Errorf("%s not in same cluster as a", s)
		}
	}
}

// ─── mergeAliases ─────────────────────────────────────────────────────────────

func TestMergeAliases_DeduplicatesOnCaseFold(t *testing.T) {
	got := mergeAliases([]string{"Siemens AG", "siemens"}, []string{"SIEMENS AG", "Siemens S.p.A."})
	// "SIEMENS AG" folds to "siemens ag" which equals "Siemens AG" (fold) → dropped.
	// "Siemens S.p.A." is new.
	want := map[string]bool{"Siemens AG": true, "siemens": true, "Siemens S.p.A.": true}
	if len(got) != len(want) {
		t.Errorf("mergeAliases returned %v, want keys %v", got, want)
	}
	for _, a := range got {
		if !want[a] {
			t.Errorf("unexpected alias %q in result", a)
		}
	}
}

func TestMergeAliases_DropsEmpty(t *testing.T) {
	got := mergeAliases([]string{"Alpha"}, []string{"", "  ", "Beta"})
	if len(got) != 2 {
		t.Errorf("expected 2 aliases, got %v", got)
	}
}

// ─── dry-run probe ────────────────────────────────────────────────────────────

// TestDeduplicateEntities_DryRunProbe exercises the 4-fixture-page scenario
// described in the acceptance criteria:
//
//	p1+p2 → obvious duplicate (score 0.95 ≥ 0.92 = auto-merge)
//	p3    → distinct (all scores < 0.78)
//	p1+p4 → borderline (score 0.80 ∈ [0.78, 0.92) = LLM range)
//
// With DryRun=true → 0 writes; report documents the clusters.
func TestDeduplicateEntities_DryRunProbe(t *testing.T) {
	dir := t.TempDir()
	s, err := NewStore(dir, nil)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	ctx := context.Background()

	// Write 4 fixture entity pages.
	pageMap := map[string]*Page{
		"alpha-corp":    makeEntityPage("Alpha Corp", "entity"),
		"alpha-inc":     makeEntityPage("Alpha Inc", "entity"),
		"beta-holdings": makeEntityPage("Beta Holdings", "entity"),
		"gamma-ltd":     makeEntityPage("Gamma Ltd", "entity"),
	}
	for _, pg := range pageMap {
		if err := s.WritePage(ctx, pg); err != nil {
			t.Fatalf("WritePage %s: %v", pg.Title, err)
		}
	}

	// Entity texts as computed by dedupEntityText so the fake searcher keys
	// match exactly what DeduplicateEntities will pass to SearchNeighbors.
	textAlphaCorp := dedupEntityText(pageMap["alpha-corp"])
	textAlphaInc := dedupEntityText(pageMap["alpha-inc"])
	textBeta := dedupEntityText(pageMap["beta-holdings"])
	textGamma := dedupEntityText(pageMap["gamma-ltd"])

	searcher := &fakeDedupSearcher{byText: map[string][]DedupHit{
		textAlphaCorp: {
			{Slug: "alpha-inc", Category: "entity", Score: 0.95},     // obvious dup
			{Slug: "gamma-ltd", Category: "entity", Score: 0.80},     // borderline
			{Slug: "beta-holdings", Category: "entity", Score: 0.50}, // distinct
		},
		textAlphaInc: {
			{Slug: "alpha-corp", Category: "entity", Score: 0.95}, // symmetric
			{Slug: "gamma-ltd", Category: "entity", Score: 0.79},
		},
		textBeta: {
			{Slug: "alpha-corp", Category: "entity", Score: 0.50},
		},
		textGamma: {
			{Slug: "alpha-corp", Category: "entity", Score: 0.80},
			{Slug: "alpha-inc", Category: "entity", Score: 0.79},
		},
	}}
	s.SetDedupBackend(searcher)

	report, err := s.DeduplicateEntities(ctx, DedupOptions{DryRun: true})
	if err != nil {
		t.Fatalf("DeduplicateEntities: %v", err)
	}

	// Dry run: no writes.
	if report.MergesApplied != 0 {
		t.Errorf("dry run: MergesApplied = %d, want 0", report.MergesApplied)
	}
	if report.EdgesRewired != 0 {
		t.Errorf("dry run: EdgesRewired = %d, want 0", report.EdgesRewired)
	}

	// Find the auto-merge cluster (alpha-corp + alpha-inc).
	var autoMergeCluster *DedupCluster
	var ambiguousClusters []DedupCluster
	for i, c := range report.Clusters {
		if c.Method == "cosine_auto" {
			autoMergeCluster = &report.Clusters[i]
		}
		if c.Method == "ambiguous" {
			ambiguousClusters = append(ambiguousClusters, c)
		}
	}

	if autoMergeCluster == nil {
		t.Fatal("expected one cosine_auto cluster, got none")
	}
	// Cluster must contain alpha-corp and alpha-inc (order varies).
	members := map[string]bool{
		autoMergeCluster.Survivor:  true,
		autoMergeCluster.Losers[0]: true,
	}
	if !members["alpha-corp"] || !members["alpha-inc"] {
		t.Errorf("auto-merge cluster has wrong members: survivor=%s losers=%v",
			autoMergeCluster.Survivor, autoMergeCluster.Losers)
	}

	// Beta-holdings must NOT appear in any merge cluster.
	for _, c := range report.Clusters {
		if c.Method == "cosine_auto" || c.Method == "llm_yes" {
			if c.Survivor == "beta-holdings" {
				t.Errorf("beta-holdings (distinct) wrongly merged as survivor")
			}
			for _, l := range c.Losers {
				if l == "beta-holdings" {
					t.Errorf("beta-holdings (distinct) wrongly merged as loser")
				}
			}
		}
	}

	// Borderline pair (alpha-corp/alpha-inc and gamma-ltd) should be AMBIGUOUS
	// (no LLM wired).
	if len(ambiguousClusters) == 0 {
		t.Error("expected at least one ambiguous cluster (no LLM wired), got none")
	}
}

// TestDeduplicateEntities_NilSearcher returns the required error.
func TestDeduplicateEntities_NilSearcher(t *testing.T) {
	dir := t.TempDir()
	s, _ := NewStore(dir, nil)
	_, err := s.DeduplicateEntities(context.Background(), DedupOptions{})
	if err == nil || err.Error() != "embedding backend required for dedup" {
		t.Errorf("expected 'embedding backend required for dedup', got %v", err)
	}
}

// TestDeduplicateEntities_LLMTiebreaker exercises the YES/NO path.
func TestDeduplicateEntities_LLMTiebreaker(t *testing.T) {
	dir := t.TempDir()
	s, _ := NewStore(dir, nil)
	ctx := context.Background()

	xCorpPage := makeEntityPage("X Corp", "entity")
	xIncPage := makeEntityPage("X Inc", "entity")
	for _, pg := range []*Page{xCorpPage, xIncPage} {
		_ = s.WritePage(ctx, pg)
	}

	// Compute entity texts matching what DeduplicateEntities will send.
	textXCorp := dedupEntityText(xCorpPage)
	textXInc := dedupEntityText(xIncPage)

	searcher := &fakeDedupSearcher{byText: map[string][]DedupHit{
		textXCorp: {{Slug: "x-inc", Category: "entity", Score: 0.85}},
		textXInc:  {{Slug: "x-corp", Category: "entity", Score: 0.85}},
	}}
	llm := &fakeDedupLLM{response: "YES"}
	s.SetDedupBackend(searcher)
	s.SetDedupLLM(llm)

	report, err := s.DeduplicateEntities(ctx, DedupOptions{DryRun: true})
	if err != nil {
		t.Fatalf("DeduplicateEntities: %v", err)
	}
	if llm.calls == 0 {
		t.Error("expected at least one LLM call")
	}
	var llmMerge *DedupCluster
	for i, c := range report.Clusters {
		if c.Method == "llm_yes" {
			llmMerge = &report.Clusters[i]
			break
		}
	}
	if llmMerge == nil {
		t.Errorf("expected llm_yes cluster, got clusters: %+v", report.Clusters)
	}
}

// TestDeduplicateEntities_MaxLLMCalls caps LLM spend.
func TestDeduplicateEntities_MaxLLMCalls(t *testing.T) {
	dir := t.TempDir()
	s, _ := NewStore(dir, nil)
	ctx := context.Background()

	// 4 entity/concept pages all in LLM range with each other.
	titles := []string{"A Corp", "A Inc", "A Ltd", "A GmbH"}
	pages := make([]*Page, len(titles))
	for i, title := range titles {
		pages[i] = makeEntityPage(title, "entity")
		_ = s.WritePage(ctx, pages[i])
	}

	slugs := []string{"a-corp", "a-inc", "a-ltd", "a-gmbh"}
	byText := map[string][]DedupHit{}
	for i, pg := range pages {
		hits := []DedupHit{}
		for j, slug := range slugs {
			if i != j {
				hits = append(hits, DedupHit{Slug: slug, Category: "entity", Score: 0.82})
			}
		}
		byText[dedupEntityText(pg)] = hits
	}
	llm := &fakeDedupLLM{response: "YES"}
	s.SetDedupBackend(&fakeDedupSearcher{byText: byText})
	s.SetDedupLLM(llm)

	report, err := s.DeduplicateEntities(ctx, DedupOptions{DryRun: true, MaxLLMCalls: 1})
	if err != nil {
		t.Fatalf("DeduplicateEntities: %v", err)
	}
	if llm.calls > 1 {
		t.Errorf("LLM was called %d times, want ≤1", llm.calls)
	}
	// Remaining pairs should be AMBIGUOUS.
	for _, c := range report.Clusters {
		if c.Method == "llm_yes" || c.Method == "ambiguous" {
			continue // expected
		}
		if c.Method == "cosine_auto" {
			t.Errorf("unexpected cosine_auto cluster: %+v", c)
		}
	}
}

// TestDeduplicateEntities_SkipsNonEntityPages confirms only entity/concept
// categories participate in dedup.
func TestDeduplicateEntities_SkipsNonEntityPages(t *testing.T) {
	dir := t.TempDir()
	s, _ := NewStore(dir, nil)
	ctx := context.Background()

	alphaPage := makeEntityPage("Alpha", "entity")
	_ = s.WritePage(ctx, alphaPage)
	// Page with different category should be ignored.
	factPage := makeEntityPage("Some Fact", "fact")
	_ = s.WritePage(ctx, factPage)

	// searcher returns the fact page as a neighbor — should be filtered out.
	searcher := &fakeDedupSearcher{byText: map[string][]DedupHit{
		dedupEntityText(alphaPage): {{Slug: "some-fact", Category: "fact", Score: 0.98}},
	}}
	s.SetDedupBackend(searcher)

	report, err := s.DeduplicateEntities(ctx, DedupOptions{DryRun: true})
	if err != nil {
		t.Fatalf("DeduplicateEntities: %v", err)
	}
	for _, c := range report.Clusters {
		if c.Survivor == "some-fact" || slices.Contains(c.Losers, "some-fact") {
			t.Error("non-entity page 'some-fact' should not appear in any cluster")
		}
	}
}
