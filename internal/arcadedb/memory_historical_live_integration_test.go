//go:build arcadedb_integration

// The historical-write measurement, against a live ArcadeDB.
//
// A fact written already-closed carries no fact_key -- the unique index means the key can
// only belong to the currently-valid version -- so the provenance attach, which looked a
// fact up BY that key, never found one and always created another edge. Measured through
// the MCP surface on 2026-09-03: the same closed fact written twice produced two edges, and
// `memory_facts_about` with an `as_of` inside the window returned it twice.
//
// Run: go test -tags arcadedb_integration ./internal/arcadedb/ -run Historical
package arcadedb

import (
	"context"
	"testing"
	"time"
)

// closedFact is a fact whose window has already ended at the instant it is written.
func closedFact(memoryID string) Fact {
	return Fact{
		Subject:   "HistoricalOfficer",
		Predicate: "stationed_at",
		Object:    "HistoricalPrecinct",
		Statement: "HistoricalOfficer was stationed at HistoricalPrecinct through 2025.",
		ValidFrom: time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC),
		ValidTo:   time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
		Source: FactSource{
			RunID: "historical-run", MemoryIDs: []string{memoryID}, WriterRole: WriterParent,
		},
	}
}

func TestHistoricalFactWrittenTwiceEnrichesItInsteadOfDuplicatingIt(t *testing.T) {
	client := disposableMemoryClient(t)
	ctx := context.Background()
	now := time.Now().UTC()

	for _, memoryID := range []string{"historical-a", "historical-b"} {
		if _, err := client.UpsertFact(ctx, closedFact(memoryID), now); err != nil {
			t.Fatalf("write %s: %v", memoryID, err)
		}
	}

	inWindow := time.Date(2025, 6, 1, 0, 0, 0, 0, time.UTC)
	hits, err := client.FactsAbout(ctx, "HistoricalOfficer", "", 10, inWindow, FactsAboutDirect)
	if err != nil {
		t.Fatalf("FactsAbout: %v", err)
	}
	if len(hits) != 1 {
		t.Fatalf("the same closed fact was stored %d times: %+v", len(hits), hits)
	}
	// Both writes' provenance has to survive on the one fact. Duplicating split it across
	// the copies, which is the fragmentation the source merge exists to prevent.
	var ids []string
	for _, source := range hits[0].Sources {
		ids = append(ids, source.MemoryIDs...)
	}
	if len(ids) != 2 {
		t.Fatalf("provenance = %v, want both writes' memory ids merged onto the one fact", ids)
	}
}

// A different window is a different fact. Merging on content alone would erase the
// distinction the bitemporal model exists to keep.
func TestHistoricalFactsWithDifferentWindowsStaySeparate(t *testing.T) {
	client := disposableMemoryClient(t)
	ctx := context.Background()
	now := time.Now().UTC()

	first := closedFact("window-a")
	second := closedFact("window-b")
	second.ValidFrom = time.Date(2023, 1, 1, 0, 0, 0, 0, time.UTC)
	second.ValidTo = time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)

	for _, fact := range []Fact{first, second} {
		if _, err := client.UpsertFact(ctx, fact, now); err != nil {
			t.Fatalf("write: %v", err)
		}
	}

	for instant, want := range map[time.Time]string{
		time.Date(2025, 6, 1, 0, 0, 0, 0, time.UTC): "window-a",
		time.Date(2023, 6, 1, 0, 0, 0, 0, time.UTC): "window-b",
	} {
		hits, err := client.FactsAbout(ctx, "HistoricalOfficer", "", 10, instant, FactsAboutDirect)
		if err != nil {
			t.Fatalf("FactsAbout at %s: %v", instant, err)
		}
		if len(hits) != 1 {
			t.Fatalf("at %s: %d facts, want exactly the one valid then: %+v", instant, len(hits), hits)
		}
		if got := hits[0].Sources[0].MemoryIDs[0]; got != want {
			t.Fatalf("at %s: memory id %q, want %q -- the two windows were merged", instant, got, want)
		}
	}
}
