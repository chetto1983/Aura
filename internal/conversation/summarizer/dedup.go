package summarizer

import (
	"context"
	"fmt"

	"github.com/aura/aura/internal/search"
)

// WikiSearcher is the search surface the Deduper needs. Satisfied by *search.Engine.
type WikiSearcher interface {
	Search(ctx context.Context, query string, topK int) ([]search.Result, error)
}

// Deduper checks a Candidate against the wiki and returns a dedup Decision.
type Deduper struct {
	engine         WikiSearcher
	patchThreshold float32 // sim > this → skip (redundant); stored as float32 to match search.Result.Score
	newThreshold   float32 // sim < this → new; between the two → patch
}

// NewDeduper creates a Deduper.
// patchThreshold: similarity strictly above which we skip (default 0.85).
// newThreshold: similarity strictly below which we create a new page (default 0.5).
func NewDeduper(engine WikiSearcher, patchThreshold, newThreshold float64) *Deduper {
	return &Deduper{
		engine:         engine,
		patchThreshold: float32(patchThreshold),
		newThreshold:   float32(newThreshold),
	}
}

// Deduplicate searches the wiki for the candidate fact and returns a Decision.
func (d *Deduper) Deduplicate(ctx context.Context, c Candidate) (Decision, error) {
	results, err := d.engine.Search(ctx, c.Fact, 3)
	if err != nil {
		return Decision{}, fmt.Errorf("dedup search: %w", err)
	}

	if len(results) == 0 {
		return Decision{Candidate: c, Action: ActionNew, Similarity: 0}, nil
	}

	top := results[0]
	sim := top.Score // float32, matches search.Result

	switch {
	case sim > d.patchThreshold:
		return Decision{Candidate: c, Action: ActionSkip, TargetSlug: top.Slug, Similarity: float64(sim)}, nil
	case sim >= d.newThreshold:
		return Decision{Candidate: c, Action: ActionPatch, TargetSlug: top.Slug, Similarity: float64(sim)}, nil
	default:
		return Decision{Candidate: c, Action: ActionNew, Similarity: float64(sim)}, nil
	}
}
