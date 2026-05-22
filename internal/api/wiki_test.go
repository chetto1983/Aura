package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"testing"
	"time"

	"github.com/aura/aura/internal/storage/search"
	"github.com/aura/aura/internal/wiki"
)

// newTestEnvWithSearcher creates a testEnv whose router has WikiSearcher wired.
func newTestEnvWithSearcher(t *testing.T, searcher search.Searcher) *testEnv {
	t.Helper()
	e := newTestEnv(t)
	e.router = NewRouter(Deps{
		Wiki:         e.wiki,
		Sources:      e.sources,
		Scheduler:    e.sched,
		Markitdown:   &fakeMarkitdown{},
		WikiSearcher: searcher,
	})
	return e
}

// newWikiPage returns a minimal valid wiki.Page with the given title.
func newWikiPage(title string) *wiki.Page {
	now := time.Now().UTC().Format(time.RFC3339)
	return &wiki.Page{
		Title:         title,
		Body:          "body text",
		SchemaVersion: wiki.CurrentSchemaVersion,
		PromptVersion: "v1",
		CreatedAt:     now,
		UpdatedAt:     now,
	}
}

// TestWikiPage_UnversionedJSON_False asserts that a page written with a
// successful git commit (Unversioned stays false) returns
// frontmatter["unversioned"] = false on GET /wiki/page?slug=…
//
// Route verified at internal/api/router.go:156 during 2026-05-10 plan
// revision: slug arrives as a QUERY PARAMETER, not a path segment.
func TestWikiPage_UnversionedJSON_False(t *testing.T) {
	e := newTestEnv(t)

	// Write a normal page — git commit succeeds, so Unversioned stays false.
	p := newWikiPage("Normal Page")
	if err := e.wiki.WritePage(context.Background(), p); err != nil {
		t.Fatalf("WritePage: %v", err)
	}

	slug := wiki.Slug("Normal Page")
	rr := e.do("GET", "/wiki/page?slug="+slug)
	if rr.Code != http.StatusOK {
		t.Fatalf("status %d, body %s", rr.Code, rr.Body)
	}

	var got WikiPage
	if err := json.Unmarshal(rr.Body.Bytes(), &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	unv, ok := got.Frontmatter["unversioned"]
	if !ok {
		t.Fatalf("frontmatter missing \"unversioned\" key: %v", got.Frontmatter)
	}
	// JSON numbers decode to float64; booleans decode to bool.
	if unv != false {
		t.Fatalf("frontmatter[\"unversioned\"] = %v (%T), want false", unv, unv)
	}
}

// TestWikiPage_UnversionedJSON_True asserts that a page written while git
// commit is failing (Unversioned=true) returns frontmatter["unversioned"] =
// true on GET /wiki/page?slug=…
//
// Uses the EXPORTED test seam SetGitCommitFuncForTest from Plan 03
// (BLOCKER 7 of 2026-05-10 plan revision: lowercase gitCommitFunc field
// would be unreachable from package api; SetGitCommitFuncForTest is exported).
func TestWikiPage_UnversionedJSON_True(t *testing.T) {
	e := newTestEnv(t)

	// Install a failing git commit via the exported test seam.
	e.wiki.SetGitCommitFuncForTest(func(_ context.Context, _, _ string) error {
		return errors.New("simulated git failure")
	})

	p := newWikiPage("Bad Commit Page")
	// WritePage sets Unversioned=true when gitCommit fails (Plan 03 D-17/D-18).
	if err := e.wiki.WritePage(context.Background(), p); err != nil {
		t.Fatalf("WritePage: %v", err)
	}

	slug := wiki.Slug("Bad Commit Page")
	rr := e.do("GET", "/wiki/page?slug="+slug)
	if rr.Code != http.StatusOK {
		t.Fatalf("status %d, body %s", rr.Code, rr.Body)
	}

	var got WikiPage
	if err := json.Unmarshal(rr.Body.Bytes(), &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	unv, ok := got.Frontmatter["unversioned"]
	if !ok {
		t.Fatalf("frontmatter missing \"unversioned\" key: %v", got.Frontmatter)
	}
	if unv != true {
		t.Fatalf("frontmatter[\"unversioned\"] = %v (%T), want true", unv, unv)
	}
}

// TestWikiSearch_SystemFilter_DefaultExcludes verifies that pages with
// category="system" are absent from default /wiki/search results.
func TestWikiSearch_SystemFilter_DefaultExcludes(t *testing.T) {
	searcher := &fakeAPISearcher{
		indexed: true,
		results: []search.Result{
			{Slug: "aura-operating-memory", Title: "Operating Memory", Category: "system", Score: 0.9},
			{Slug: "dante-alighieri", Title: "Dante Alighieri", Category: "author", Score: 0.8},
			{Slug: "aura-quality-bench", Title: "Quality Bench", Category: "system", Score: 0.7},
			{Slug: "divina-commedia", Title: "Divina Commedia", Category: "opera", Score: 0.6},
		},
	}
	e := newTestEnvWithSearcher(t, searcher)

	rr := e.do("GET", "/wiki/search?q=dante")
	if rr.Code != http.StatusOK {
		t.Fatalf("status %d, body %s", rr.Code, rr.Body)
	}
	var resp WikiSearchResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	for _, hit := range resp.Results {
		if hit.Category == "system" {
			t.Errorf("default search returned system page %q", hit.Slug)
		}
	}
	slugs := make([]string, 0, len(resp.Results))
	for _, h := range resp.Results {
		slugs = append(slugs, h.Slug)
	}
	// Non-system pages must still appear.
	found := false
	for _, s := range slugs {
		if s == "dante-alighieri" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected dante-alighieri in results, got %v", slugs)
	}
}

// TestWikiSearch_SystemFilter_IncludeSystemParam verifies that
// ?include_system=1 surfaces system-category pages.
func TestWikiSearch_SystemFilter_IncludeSystemParam(t *testing.T) {
	searcher := &fakeAPISearcher{
		indexed: true,
		results: []search.Result{
			{Slug: "aura-operating-memory", Title: "Operating Memory", Category: "system", Score: 0.9},
			{Slug: "dante-alighieri", Title: "Dante Alighieri", Category: "author", Score: 0.8},
		},
	}
	e := newTestEnvWithSearcher(t, searcher)

	rr := e.do("GET", "/wiki/search?q=dante&include_system=1")
	if rr.Code != http.StatusOK {
		t.Fatalf("status %d, body %s", rr.Code, rr.Body)
	}
	var resp WikiSearchResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	found := false
	for _, h := range resp.Results {
		if h.Slug == "aura-operating-memory" {
			found = true
		}
	}
	if !found {
		t.Errorf("include_system=1 should return system pages, but aura-operating-memory was absent")
	}
}

// TestWikiSearch_SystemFilter_TagBased verifies that pages tagged "system"
// (regardless of category) are also excluded from default results.
func TestWikiSearch_SystemFilter_TagBased(t *testing.T) {
	searcher := &fakeAPISearcher{
		indexed: true,
		results: []search.Result{
			{Slug: "internal-notes", Title: "Internal Notes", Category: "", Tags: []string{"system", "meta"}, Score: 0.9},
			{Slug: "galileo-galilei", Title: "Galileo Galilei", Category: "scientist", Score: 0.8},
		},
	}
	e := newTestEnvWithSearcher(t, searcher)

	rr := e.do("GET", "/wiki/search?q=galileo")
	if rr.Code != http.StatusOK {
		t.Fatalf("status %d, body %s", rr.Code, rr.Body)
	}
	var resp WikiSearchResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	for _, hit := range resp.Results {
		if hit.Slug == "internal-notes" {
			t.Errorf("default search returned tag=system page %q", hit.Slug)
		}
	}
}

// TestWikiSearch_DedupeBySlug verifies that when the search backend returns
// both a wiki_page and a graph_node for the same slug (distinct-by-design
// dual-emission from loadWikiDocuments), only the highest-scored entry survives
// in the /wiki/search response. Reproduces the autori-json q2 observed bug
// where slug "davide" appeared twice in top-5 results.
func TestWikiSearch_DedupeBySlug(t *testing.T) {
	searcher := &fakeAPISearcher{
		indexed: true,
		results: []search.Result{
			// wiki_page scores higher than graph_node for the same slug — wiki_page must win.
			{Kind: "wiki_page", Slug: "davide", Title: "Davide", Score: 0.90},
			{Kind: "graph_node", Slug: "davide", Title: "Davide Node", Score: 0.85},
			{Kind: "wiki_page", Slug: "aura", Title: "Aura", Score: 0.70},
		},
	}
	e := newTestEnvWithSearcher(t, searcher)

	rr := e.do("GET", "/wiki/search?q=davide")
	if rr.Code != http.StatusOK {
		t.Fatalf("status %d, body %s", rr.Code, rr.Body)
	}
	var resp WikiSearchResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	davideCount := 0
	for _, hit := range resp.Results {
		if hit.Slug == "davide" {
			davideCount++
		}
	}
	if davideCount != 1 {
		t.Errorf("slug 'davide' appears %d times, want exactly 1", davideCount)
	}
	for _, hit := range resp.Results {
		if hit.Slug == "davide" && hit.Kind != "wiki_page" {
			t.Errorf("kept kind=%q for slug 'davide', want 'wiki_page' (highest score)", hit.Kind)
		}
	}
	// The non-duplicate slug must still be present.
	found := false
	for _, hit := range resp.Results {
		if hit.Slug == "aura" {
			found = true
		}
	}
	if !found {
		t.Error("expected slug 'aura' in results after dedup, but it was absent")
	}
}

// TestWikiSearch_DedupeBySlug_RanksRemainSequential verifies that rank numbering
// stays gapless after dedup removes a duplicate entry.
func TestWikiSearch_DedupeBySlug_RanksRemainSequential(t *testing.T) {
	searcher := &fakeAPISearcher{
		indexed: true,
		results: []search.Result{
			{Kind: "wiki_page", Slug: "davide", Title: "Davide", Score: 0.90},
			{Kind: "graph_node", Slug: "davide", Title: "Davide Node", Score: 0.85},
			{Kind: "wiki_page", Slug: "aura", Title: "Aura", Score: 0.70},
			{Kind: "wiki_page", Slug: "progetto", Title: "Progetto", Score: 0.60},
		},
	}
	e := newTestEnvWithSearcher(t, searcher)

	rr := e.do("GET", "/wiki/search?q=davide")
	if rr.Code != http.StatusOK {
		t.Fatalf("status %d, body %s", rr.Code, rr.Body)
	}
	var resp WikiSearchResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	for i, hit := range resp.Results {
		if hit.Rank != i+1 {
			t.Errorf("result[%d].Rank=%d, want %d (sequential after dedup)", i, hit.Rank, i+1)
		}
	}
	if len(resp.Results) != 3 {
		t.Errorf("expected 3 results after dedup (4 raw - 1 dup), got %d", len(resp.Results))
	}
}

// TestWikiSearch_SystemFilter_RanksAreSequential verifies that after filtering,
// the returned Rank values are sequential (1, 2, 3...) not sparse.
func TestWikiSearch_SystemFilter_RanksAreSequential(t *testing.T) {
	searcher := &fakeAPISearcher{
		indexed: true,
		results: []search.Result{
			{Slug: "aura-operating-memory", Title: "System Page", Category: "system", Score: 0.95},
			{Slug: "dante-alighieri", Title: "Dante Alighieri", Category: "author", Score: 0.8},
			{Slug: "divina-commedia", Title: "Divina Commedia", Category: "opera", Score: 0.7},
		},
	}
	e := newTestEnvWithSearcher(t, searcher)

	rr := e.do("GET", "/wiki/search?q=dante")
	if rr.Code != http.StatusOK {
		t.Fatalf("status %d, body %s", rr.Code, rr.Body)
	}
	var resp WikiSearchResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	for i, hit := range resp.Results {
		if hit.Rank != i+1 {
			t.Errorf("result[%d].Rank=%d, want %d (sequential after filter)", i, hit.Rank, i+1)
		}
	}
}
