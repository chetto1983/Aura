//go:build arcadedb_integration

package arcadedb

import (
	"context"
	"testing"
	"time"
)

// The unit tests pin the statement; this proves ArcadeDB groups and orders it as
// read: count(*) in ORDER BY with GROUP BY, count(DISTINCT ...), and the validity
// window over DATETIME properties.
func TestListEntitiesLive_ListsOnlyThePredicatesOfCurrentFacts(t *testing.T) {
	client := disposableMemoryClient(t)
	ctx := context.Background()
	now := time.Now().UTC()
	source := FactSource{RunID: "browse-run", WriterRole: WriterParent}
	for _, fact := range []Fact{
		{Subject: "BrowseDavide", Predicate: "works_on", Object: "BrowseAura"},
		{Subject: "BrowseMarta", Predicate: "works_on", Object: "BrowseAura"},
		{Subject: "BrowseDavide", Predicate: "lives_in", Object: "BrowseCaraglio"},
		{
			Subject: "BrowseDavide", Predicate: "lived_in", Object: "BrowseCuneo",
			ValidFrom: now.AddDate(-2, 0, 0), ValidTo: now.AddDate(-1, 0, 0),
		},
	} {
		fact.Statement = fact.Subject + " " + fact.Predicate + " " + fact.Object + "."
		fact.Source = source
		if _, err := client.UpsertFact(ctx, fact, now); err != nil {
			t.Fatalf("seed %s: %v", fact.Predicate, err)
		}
	}

	got, err := client.ListEntities(ctx, 0)
	if err != nil {
		t.Fatalf("ListEntities: %v", err)
	}
	want := []PredicateSummary{{Name: "works_on", Facts: 2}, {Name: "lives_in", Facts: 1}}
	if len(got.Predicates) != len(want) || got.Predicates[0] != want[0] || got.Predicates[1] != want[1] {
		t.Fatalf("predicates = %+v, want %+v (a predicate whose only fact is closed is not in use)",
			got.Predicates, want)
	}
	if got.PredicatesTotal != 2 {
		t.Fatalf("predicates total = %d, want 2", got.PredicatesTotal)
	}
}
