package summarizer_test

import (
	"context"
	"testing"

	"github.com/aura/aura/internal/conversation/summarizer"
	"github.com/aura/aura/internal/search"
)

// mockSearchEngine satisfies summarizer.WikiSearcher with fixed results.
type mockSearchEngine struct {
	results []search.Result
}

func (m *mockSearchEngine) Search(_ context.Context, _ string, _ int) ([]search.Result, error) {
	return m.results, nil
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
