//go:build arcadedb_integration

// The merge measurement, against a live ArcadeDB.
//
// MergeEntities shipped with unit tests only, and this directory's own integration
// header already says why that is not evidence: those tests assert the statement the
// package emits, against a fake server that answers `{"moved":1}` to anything. A real
// server answered differently. `SET n = properties(f)` copies EVERY property of the
// FACT edge, and one of them -- `sources`, declared `LIST OF MAP` by EnsureMemorySchema
// -- is a list of embedded documents. Measured 2026-09-03 on 26.9.1:
//
//	http 400: TypeError: InvalidPropertyType - Property values can not contain map values
//
// Every fact written through the MCP surface carries provenance, and an entity with no
// facts has nothing to merge, so this failed for every merge that could matter.
//
// Run: go test -tags arcadedb_integration ./internal/arcadedb/ -run Merge
package arcadedb

import (
	"context"
	"testing"
	"time"
)

// mergeFact is the shape the failure needs: provenance present, because provenance is
// the property the Cypher copy could not carry. A fact without sources would merge
// cleanly and prove nothing.
func mergeFact(subject, predicate, object, statement string) Fact {
	return Fact{
		Subject:   subject,
		Predicate: predicate,
		Object:    object,
		Statement: statement,
		Source: FactSource{
			RunID:      "merge-live-run",
			MemoryIDs:  []string{"merge-live-" + predicate},
			WriterRole: WriterParent,
		},
	}
}

// TestMergeEntitiesCarriesProvenanceOntoTheSurvivor is the regression. It merges a
// duplicate that is both the subject of one fact and the object of another, so both
// the outgoing and the incoming statement run, and asserts the survivor ends up
// holding both facts WITH their provenance intact -- moving a fact and dropping who
// said it would be a merge that loses the thing merging exists to preserve.
func TestMergeEntitiesCarriesProvenanceOntoTheSurvivor(t *testing.T) {
	client := disposableMemoryClient(t)
	ctx := context.Background()
	now := time.Now().UTC()

	for _, fact := range []Fact{
		mergeFact("M. Bellini", "specialises_in", "Fraud", "M. Bellini specialises in fraud."),
		mergeFact("Questura", "employs", "M. Bellini", "Questura employs M. Bellini."),
	} {
		if _, err := client.UpsertFact(ctx, fact, now); err != nil {
			t.Fatalf("seed %s: %v", fact.Predicate, err)
		}
	}

	got, err := client.MergeEntities(ctx, "M. Bellini", "Marta Bellini")
	if err != nil {
		t.Fatalf("MergeEntities: %v", err)
	}
	if got.Moved != 2 {
		t.Fatalf("moved = %d, want both directions: %+v", got.Moved, got)
	}

	hits, err := client.FactsAbout(ctx, "Marta Bellini", "", 10, now, FactsAboutDirect)
	if err != nil {
		t.Fatalf("FactsAbout: %v", err)
	}
	if len(hits) != 2 {
		t.Fatalf("survivor holds %d facts, want 2: %+v", len(hits), hits)
	}
	for _, hit := range hits {
		if len(hit.Sources) == 0 || hit.Sources[0].RunID != "merge-live-run" {
			t.Fatalf("provenance lost on %q: %+v", hit.Predicate, hit.Sources)
		}
	}

	// The duplicate is gone, not merely emptied: a merge that leaves the old name
	// behind leaves the ambiguity it was called to resolve.
	stale, err := client.FactsAbout(ctx, "M. Bellini", "", 10, now, FactsAboutDirect)
	if err != nil {
		t.Fatalf("FactsAbout(source): %v", err)
	}
	if len(stale) != 0 {
		t.Fatalf("duplicate still holds %d facts: %+v", len(stale), stale)
	}
}

// TestMergeEntitiesRenamesWhenTargetIsUnused covers the documented second use: a target
// that does not exist yet makes the merge a rename. It runs live because the statement
// creates the target vertex itself, and whether that lands in the right vertex type is
// not something a fake server can answer.
func TestMergeEntitiesRenamesWhenTargetIsUnused(t *testing.T) {
	client := disposableMemoryClient(t)
	ctx := context.Background()
	now := time.Now().UTC()

	if _, err := client.UpsertFact(ctx,
		mergeFact("Typo Name", "reports_to", "Chief", "Typo Name reports to the Chief."),
		now); err != nil {
		t.Fatalf("seed: %v", err)
	}

	got, err := client.MergeEntities(ctx, "Typo Name", "Correct Name")
	if err != nil {
		t.Fatalf("MergeEntities: %v", err)
	}
	if got.Moved != 1 {
		t.Fatalf("moved = %d, want 1: %+v", got.Moved, got)
	}

	hits, err := client.FactsAbout(ctx, "Correct Name", "", 10, now, FactsAboutDirect)
	if err != nil {
		t.Fatalf("FactsAbout: %v", err)
	}
	if len(hits) != 1 {
		t.Fatalf("renamed entity holds %d facts, want 1: %+v", len(hits), hits)
	}
	// The statement is rewritten along with the endpoint, so the full-text index stops
	// answering under a name that no longer exists.
	if hits[0].Subject != "Correct Name" {
		t.Fatalf("subject = %q, want the surviving name", hits[0].Subject)
	}
}

// A refused correction must leave the memory exactly as it found it. It did not: the
// endpoints were minted before the supersede was resolved, so an ambiguous correction
// answered `refused: true` and still coined its object as an entity with no facts.
// Measured through the MCP surface on 2026-09-03. The orphan is not inert -- it lands in
// memory_entities, whose stated job is to be read BEFORE coining a name, so a refused
// write taught the vocabulary a name nothing supports.
func TestRefusedSupersedeCoinsNothing(t *testing.T) {
	client := disposableMemoryClient(t)
	ctx := context.Background()
	now := time.Now().UTC()

	for _, object := range []string{"RefusedOne", "RefusedTwo"} {
		fact := mergeFact("RefusedAgent", "carries", object, "RefusedAgent carries "+object+".")
		if _, err := client.UpsertFact(ctx, fact, now); err != nil {
			t.Fatalf("seed %s: %v", object, err)
		}
	}

	ambiguous := mergeFact("RefusedAgent", "carries", "RefusedGhost", "RefusedAgent carries RefusedGhost.")
	ambiguous.Supersedes = true
	written, err := client.UpsertFact(ctx, ambiguous, now)
	if err != nil {
		t.Fatalf("UpsertFact: %v", err)
	}
	if !written.Refused {
		t.Fatalf("the correction was not refused, so this proves nothing: %+v", written)
	}

	classes, err := client.entityClassScan(ctx, "RefusedGhost")
	if err != nil {
		t.Fatalf("entityClassScan: %v", err)
	}
	if held, minted := classes["RefusedGhost"]; minted {
		t.Errorf("a refused write coined %q as %s: the vocabulary now holds a name no fact supports",
			"RefusedGhost", held)
	}
}
