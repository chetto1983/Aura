package summarizer_test

import (
	"context"
	"errors"
	"testing"

	"github.com/aura/aura/internal/conversation/summarizer"
	"github.com/aura/aura/internal/search"
)

// mockSearchEngine satisfies summarizer.WikiSearcher with fixed results.
type mockSearchEngine struct {
	results []search.Result
	err     error
}

func (m *mockSearchEngine) Search(_ context.Context, _ string, _ int) ([]search.Result, error) {
	return m.results, m.err
}

func TestDeduper_HighSim_Skip(t *testing.T) {
	engine := &mockSearchEngine{results: []search.Result{
		{Slug: "marco-bologna", Score: 0.9},
	}}
	d := summarizer.NewDeduper(engine, 0.85, 0.5)
	c := summarizer.Candidate{Fact: "Marco lives in Bologna", Score: 0.9}

	dec, err := d.Deduplicate(context.Background(), c)
	if err != nil {
		t.Fatalf("Deduplicate: %v", err)
	}
	if dec.Action != summarizer.ActionSkip {
		t.Fatalf("want ActionSkip (sim>0.85), got %s", dec.Action)
	}
}

func TestDeduper_MidSim_Patch(t *testing.T) {
	engine := &mockSearchEngine{results: []search.Result{
		{Slug: "marco-info", Score: 0.7},
	}}
	d := summarizer.NewDeduper(engine, 0.85, 0.5)
	c := summarizer.Candidate{Fact: "Marco works in fintech", Score: 0.8}

	dec, err := d.Deduplicate(context.Background(), c)
	if err != nil {
		t.Fatalf("Deduplicate: %v", err)
	}
	if dec.Action != summarizer.ActionPatch {
		t.Fatalf("want ActionPatch (0.5<=sim<=0.85), got %s", dec.Action)
	}
	if dec.TargetSlug != "marco-info" {
		t.Fatalf("want TargetSlug=marco-info, got %q", dec.TargetSlug)
	}
}

func TestDeduper_LowSim_New(t *testing.T) {
	engine := &mockSearchEngine{results: []search.Result{
		{Slug: "unrelated", Score: 0.2},
	}}
	d := summarizer.NewDeduper(engine, 0.85, 0.5)
	c := summarizer.Candidate{Fact: "Completely new fact", Score: 0.8}

	dec, err := d.Deduplicate(context.Background(), c)
	if err != nil {
		t.Fatalf("Deduplicate: %v", err)
	}
	if dec.Action != summarizer.ActionNew {
		t.Fatalf("want ActionNew (sim<0.5), got %s", dec.Action)
	}
}

func TestDeduper_BoundaryAtPatchThreshold(t *testing.T) {
	// sim exactly at 0.85 → patch (boundary is patch-side per plan)
	engine := &mockSearchEngine{results: []search.Result{
		{Slug: "boundary-page", Score: 0.85},
	}}
	d := summarizer.NewDeduper(engine, 0.85, 0.5)
	c := summarizer.Candidate{Fact: "boundary fact", Score: 0.8}

	dec, err := d.Deduplicate(context.Background(), c)
	if err != nil {
		t.Fatalf("Deduplicate: %v", err)
	}
	if dec.Action != summarizer.ActionPatch {
		t.Fatalf("want ActionPatch at boundary 0.85, got %s", dec.Action)
	}
}

func TestDeduper_BoundaryAtNewThreshold(t *testing.T) {
	// sim exactly at 0.5 → patch (boundary is patch-side per plan)
	engine := &mockSearchEngine{results: []search.Result{
		{Slug: "lower-boundary", Score: 0.5},
	}}
	d := summarizer.NewDeduper(engine, 0.85, 0.5)
	c := summarizer.Candidate{Fact: "boundary fact low", Score: 0.8}

	dec, err := d.Deduplicate(context.Background(), c)
	if err != nil {
		t.Fatalf("Deduplicate: %v", err)
	}
	if dec.Action != summarizer.ActionPatch {
		t.Fatalf("want ActionPatch at boundary 0.5, got %s", dec.Action)
	}
}

func TestDeduper_EmptyWiki(t *testing.T) {
	engine := &mockSearchEngine{results: nil}
	d := summarizer.NewDeduper(engine, 0.85, 0.5)
	c := summarizer.Candidate{Fact: "brand new fact", Score: 0.9}

	dec, err := d.Deduplicate(context.Background(), c)
	if err != nil {
		t.Fatalf("Deduplicate: %v", err)
	}
	if dec.Action != summarizer.ActionNew {
		t.Fatalf("want ActionNew (empty wiki), got %s", dec.Action)
	}
}

// TestDeduper_SearchError covers the search engine error return path.
func TestDeduper_SearchError(t *testing.T) {
	engine := &mockSearchEngine{err: errors.New("search unavailable")}
	d := summarizer.NewDeduper(engine, 0.85, 0.5)
	c := summarizer.Candidate{Fact: "some fact", Score: 0.8}

	_, err := d.Deduplicate(context.Background(), c)
	if err == nil {
		t.Fatal("want error when search engine fails, got nil")
	}
}

// TestDeduper_MultipleResults_TopUsed verifies that only the top result's
// similarity drives the decision when multiple candidates are returned.
func TestDeduper_MultipleResults_TopUsed(t *testing.T) {
	engine := &mockSearchEngine{results: []search.Result{
		{Slug: "top-page", Score: 0.92},    // skip
		{Slug: "second-page", Score: 0.6},  // would be patch
		{Slug: "third-page", Score: 0.1},   // would be new
	}}
	d := summarizer.NewDeduper(engine, 0.85, 0.5)
	c := summarizer.Candidate{Fact: "well-covered fact", Score: 0.9}

	dec, err := d.Deduplicate(context.Background(), c)
	if err != nil {
		t.Fatalf("Deduplicate: %v", err)
	}
	if dec.Action != summarizer.ActionSkip {
		t.Fatalf("want ActionSkip (top result 0.92 > 0.85), got %s", dec.Action)
	}
	if dec.TargetSlug != "top-page" {
		t.Fatalf("want TargetSlug=top-page, got %q", dec.TargetSlug)
	}
}

// TestAction_String covers the Action.String() method on types.go.
func TestAction_String(t *testing.T) {
	cases := []struct {
		action summarizer.Action
		want   string
	}{
		{summarizer.ActionSkip, "skip"},
		{summarizer.ActionPatch, "patch"},
		{summarizer.ActionNew, "new"},
	}
	for _, tc := range cases {
		if got := tc.action.String(); got != tc.want {
			t.Errorf("Action(%q).String() = %q, want %q", tc.action, got, tc.want)
		}
	}
}
