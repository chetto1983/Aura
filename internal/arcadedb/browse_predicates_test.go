package arcadedb

import (
	"context"
	"strings"
	"testing"
)

// Measured on the lab VM on 2026-11-28: with no tool listing the predicates in use, a
// writer read memory_entities, saw Davide and Telegram, and coined
// prefers_work_reminders_via beside the existing prefers_notification_channel.
func TestListEntitiesListsThePredicatesInUse(t *testing.T) {
	client, rec := recordingClient(t,
		`{"result":[{"name":"Davide","kind":"Person","facts":3}]}`,
		`{"result":[{"total":1}]}`,
		`{"result":[{"name":"works_on","facts":2},{"name":"lives_in","facts":1}]}`,
		`{"result":[{"total":7}]}`)
	got, err := client.ListEntities(context.Background(), 0)
	if err != nil {
		t.Fatalf("ListEntities: %v", err)
	}
	want := []PredicateSummary{{Name: "works_on", Facts: 2}, {Name: "lives_in", Facts: 1}}
	if len(got.Predicates) != len(want) || got.Predicates[0] != want[0] || got.Predicates[1] != want[1] {
		t.Fatalf("predicates = %+v, want %+v", got.Predicates, want)
	}
	if got.PredicatesTotal != 7 {
		t.Fatalf("predicates total = %d, want 7", got.PredicatesTotal)
	}
	listing := rec.statements[2]
	for _, fragment := range []string{
		"FROM " + factEdgeType, asOfCondition, "GROUP BY predicate", "ORDER BY count(*) DESC", "LIMIT 50",
	} {
		if !strings.Contains(listing, fragment) {
			t.Fatalf("predicate listing %q lacks %q", listing, fragment)
		}
	}
	if !strings.Contains(rec.statements[3], "count(DISTINCT predicate)") ||
		!strings.Contains(rec.statements[3], asOfCondition) {
		t.Fatalf("predicate total does not count distinct current predicates: %s", rec.statements[3])
	}
	for _, i := range []int{2, 3} {
		if rec.params[i]["as_of"] == nil {
			t.Fatalf("statement %d has no as_of: %v", i, rec.params[i])
		}
	}
}

func TestListEntitiesGivesAnEmptyPredicateListForAMemoryWithNoFacts(t *testing.T) {
	client, _ := recordingClient(t,
		`{"result":[]}`, `{"result":[{"total":0}]}`, `{"result":[]}`, `{"result":[{"total":0}]}`)
	got, err := client.ListEntities(context.Background(), 0)
	if err != nil {
		t.Fatalf("ListEntities: %v", err)
	}
	if got.Predicates == nil || len(got.Predicates) != 0 || got.PredicatesTotal != 0 {
		t.Fatalf("predicates = %#v total = %d, want an empty non-nil list and 0",
			got.Predicates, got.PredicatesTotal)
	}
}

func TestListEntitiesFallsBackToThePredicatePageWhenTheCountIsAbsent(t *testing.T) {
	client, _ := recordingClient(t,
		`{"result":[]}`, `{"result":[{"total":0}]}`,
		`{"result":[{"name":"works_on","facts":2}]}`, `{"result":[{"unexpected":1}]}`)
	got, err := client.ListEntities(context.Background(), 0)
	if err != nil {
		t.Fatalf("ListEntities: %v", err)
	}
	if got.PredicatesTotal != 1 {
		t.Fatalf("predicates total = %d, want the returned page size", got.PredicatesTotal)
	}
}
