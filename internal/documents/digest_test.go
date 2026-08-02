package documents

import (
	"testing"
	"time"
)

func TestSortDigestHitsIsStableOnTies(t *testing.T) {
	t.Parallel()
	hits := []DigestHit{
		{Title: "zeta", Rank: 0.5},
		{Title: "alpha", Rank: 0.5},
		{Title: "best", Rank: 0.9},
	}
	SortDigestHits(hits)
	if hits[0].Title != "best" {
		t.Fatalf("rank did not win: %#v", hits)
	}
	// A tie must not depend on whatever order the database returned.
	if hits[1].Title != "alpha" || hits[2].Title != "zeta" {
		t.Fatalf("tie is not broken by title: %#v", hits)
	}
}

// "The file I just uploaded" carries no search terms, so websearch_to_tsquery
// returns the empty tsquery and EVERY row ranks 0.0 — the whole page is a tie.
// Ordering that by title answered with the alphabetically first of the newest
// eight, which is the one question a library has to get right.
func TestSortDigestHitsPutsTheNewestFirstOnABlankQuery(t *testing.T) {
	t.Parallel()
	now := time.Now()
	hits := []DigestHit{
		{Title: "alpha.xlsx", Rank: 0, UpdatedAt: now.Add(-48 * time.Hour)},
		{Title: "zulu.xlsx", Rank: 0, UpdatedAt: now},
		{Title: "mike.xlsx", Rank: 0, UpdatedAt: now.Add(-24 * time.Hour)},
	}
	SortDigestHits(hits)
	if hits[0].Title != "zulu.xlsx" {
		t.Fatalf("first hit = %q, want the most recently updated document", hits[0].Title)
	}
	if hits[2].Title != "alpha.xlsx" {
		t.Fatalf("last hit = %q, want the oldest", hits[2].Title)
	}
}

// Rank still wins over recency: a real query must not be reordered by date.
func TestSortDigestHitsKeepsRankAboveRecency(t *testing.T) {
	t.Parallel()
	now := time.Now()
	hits := []DigestHit{
		{Title: "old-but-relevant", Rank: 0.8, UpdatedAt: now.Add(-72 * time.Hour)},
		{Title: "new-but-not", Rank: 0.1, UpdatedAt: now},
	}
	SortDigestHits(hits)
	if hits[0].Title != "old-but-relevant" {
		t.Fatalf("first hit = %q; recency overtook relevance", hits[0].Title)
	}
}
