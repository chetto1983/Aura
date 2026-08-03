package documents

import (
	"context"
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

func TestSearchDigestsBindsOnlyBoundedInt32Limits(t *testing.T) {
	const identityID = "00000000-0000-0000-0000-000000000001"
	for _, tc := range []struct {
		name  string
		limit int
		want  int32
	}{
		{name: "negative", limit: -1, want: defaultDigestLimit},
		{name: "zero", limit: 0, want: defaultDigestLimit},
		{name: "minimum", limit: 1, want: 1},
		{name: "maximum", limit: maxDigestLimit, want: maxDigestLimit},
		{name: "over maximum", limit: maxDigestLimit + 1, want: defaultDigestLimit},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fake := &fakeDocDBTX{rows: &fakeDocRows{}}
			store := NewPostgresCatalogStore(fake)
			if _, err := store.SearchDigests(context.Background(), identityID, "query", tc.limit); err != nil {
				t.Fatalf("SearchDigests: %v", err)
			}
			if len(fake.queryArgs) != 3 || fake.queryArgs[2] != tc.want {
				t.Fatalf("query args = %#v, want int32 limit %d", fake.queryArgs, tc.want)
			}
		})
	}
}
