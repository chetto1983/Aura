package main

// Split out of tool_memory_test.go (620/600 LOC after adding
// handleTransactionEndpoints -- CLAUDE.md's NO GOD CLASS file cap): the
// read paths (memory_search, memory_facts_about, memory_recall) are a
// distinct concern from memory_upsert_fact's write-path tests that remain
// there. Both share newRecordingDB/recordingDB/oneFactRow, defined once in
// tool_memory_test.go and visible here because this file is the same
// package.

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/chetto1983/aura/internal/arcadedb"
)

// D-15: a recall's fact_key is the identifier a correction later names --
// without it surfacing here, an ambiguous refusal has nothing to retry with.
func TestSearchAndFactsAboutSurfaceFactKey(t *testing.T) {
	t.Run("memory_search", func(t *testing.T) {
		client, _ := newRecordingDB(t, oneFactRow)
		out, err := search(t, client, MemorySearchInput{Query: "where does Davide live"})
		if err != nil {
			t.Fatalf("search: %v", err)
		}
		if len(out.Facts) != 1 || out.Facts[0].FactKey != "solo-key" {
			t.Fatalf("facts = %+v, want fact_key threaded through", out.Facts)
		}
	})
	t.Run("memory_facts_about", func(t *testing.T) {
		client, _ := newRecordingDB(t, oneFactRow)
		out, err := factsAbout(t, client, MemoryFactsAboutInput{Entity: "Davide"})
		if err != nil {
			t.Fatalf("facts_about: %v", err)
		}
		if len(out.Facts) != 1 || out.Facts[0].FactKey != "solo-key" {
			t.Fatalf("facts = %+v, want fact_key threaded through", out.Facts)
		}
	})
}

func search(
	t *testing.T,
	client *arcadedb.Client,
	in MemorySearchInput,
) (MemorySearchOutput, error) {
	t.Helper()
	_, out, err := memorySearchHandler(singleTenant(t, client))(context.Background(), reqWithIdentity(testIdentity), in)
	return out, err
}

const oneFactRow = `{"result":[{"statement":"Davide lives in Caraglio.","predicate":"lives_in",
"subject":"Davide","subject_kind":"Person","object":"Caraglio","object_kind":"Location",
"valid_from":"2026-01-01T00:00:00Z","fact_key":"solo-key",
"sources":[{"run_id":"run-1","memory_ids":["mem-1"]}]}]}`

func TestSearchUsesTheFullTextIndexAndKeepsProvenance(t *testing.T) {
	client, rec := newRecordingDB(t, oneFactRow)
	out, err := search(t, client, MemorySearchInput{Query: "where does Davide live"})
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	statement, _, ok := rec.find("SEARCH_INDEX")
	if !ok {
		t.Fatalf("no full-text search issued; statements = %v", rec.statements)
	}
	if strings.Contains(statement, "vector.") {
		t.Fatalf("search must not use a vector index: %s", statement)
	}
	if len(out.Facts) != 1 || out.Facts[0].Subject != "Davide" {
		t.Fatalf("facts = %+v", out.Facts)
	}
	if len(out.Facts[0].Sources) != 1 || out.Facts[0].Sources[0].RunID != "run-1" {
		t.Fatalf("provenance lost: %+v", out.Facts[0])
	}
	if out.Retrieval.Path != "lexical" || out.Retrieval.Abstained || out.Retrieval.Reason != "embedder_not_configured" {
		t.Fatalf("retrieval metadata = %+v", out.Retrieval)
	}
}

// A search with no instant means "what is true now": unfiltered, a superseded
// fact wins at rank 1.
func TestSearchWithoutAsOfStillFiltersToNow(t *testing.T) {
	client, rec := newRecordingDB(t, oneFactRow)
	if _, err := search(t, client, MemorySearchInput{Query: "q"}); err != nil {
		t.Fatalf("search: %v", err)
	}
	statement, params, ok := rec.find("valid_to IS NULL OR valid_to > :as_of")
	if !ok {
		t.Fatalf("expired facts are not excluded: %v", rec.statements)
	}
	if !strings.Contains(statement, "valid_from <= :as_of") {
		t.Fatalf("missing lower bound: %s", statement)
	}
	if _, present := params["as_of"]; !present {
		t.Fatal("as_of must default to now")
	}
}

func TestSearchRejectsAMalformedAsOf(t *testing.T) {
	client, _ := newRecordingDB(t)
	_, err := search(t, client, MemorySearchInput{Query: "q", AsOf: "last year"})
	if err == nil || !strings.Contains(err.Error(), "as_of") {
		t.Fatalf("err = %v", err)
	}
}

func TestSearchOnEmptyGraphReturnsNoFactsNotAnError(t *testing.T) {
	client, _ := newRecordingDB(t, `{"result":[]}`)
	out, err := search(t, client, MemorySearchInput{Query: "q"})
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	encoded, _ := json.Marshal(out)
	if !strings.Contains(string(encoded), `"facts":[]`) {
		t.Fatalf("facts did not encode as an empty array: %s", encoded)
	}
	if !out.Retrieval.Abstained || out.Retrieval.Path != "lexical" || out.Retrieval.Reason == "" {
		t.Fatalf("empty search did not expose abstention: %+v", out.Retrieval)
	}
}

func factsAbout(
	t *testing.T,
	client *arcadedb.Client,
	in MemoryFactsAboutInput,
) (MemorySearchOutput, error) {
	t.Helper()
	_, out, err := memoryFactsAboutHandler(singleTenant(t, client))(context.Background(), reqWithIdentity(testIdentity), in)
	return out, err
}

// The traversal path is the exact one: it must not rank, and must not need a
// query string at all.
func TestFactsAboutWalksTheGraphWithoutRanking(t *testing.T) {
	client, rec := newRecordingDB(t, oneFactRow)
	out, err := factsAbout(t, client, MemoryFactsAboutInput{Entity: "Davide"})
	if err != nil {
		t.Fatalf("facts_about: %v", err)
	}
	statement := rec.statements[0]
	if !strings.Contains(statement, "outV().name = :entity") {
		t.Fatalf("traversal does not anchor on the entity: %s", statement)
	}
	if strings.Contains(statement, "SEARCH_INDEX") || strings.Contains(statement, "vector.") {
		t.Fatalf("traversal must not rank: %s", statement)
	}
	if len(out.Facts) != 1 || out.Facts[0].Object != "Caraglio" {
		t.Fatalf("facts = %+v", out.Facts)
	}
	if out.Retrieval.Path != "graph" || out.Retrieval.Abstained {
		t.Fatalf("retrieval metadata = %+v", out.Retrieval)
	}
}

func TestFactsAboutEmptyGraphReportsNoFacts(t *testing.T) {
	client, _ := newRecordingDB(t, `{"result":[]}`)
	out, err := factsAbout(t, client, MemoryFactsAboutInput{Entity: "Nobody"})
	if err != nil {
		t.Fatalf("facts_about: %v", err)
	}
	if !out.Retrieval.Abstained || out.Retrieval.Path != "graph" || out.Retrieval.Reason != "no_facts" {
		t.Fatalf("retrieval metadata = %+v", out.Retrieval)
	}
}

func TestFactsAboutNarrowsByPredicate(t *testing.T) {
	client, rec := newRecordingDB(t, oneFactRow)
	in := MemoryFactsAboutInput{Entity: "Davide", Predicate: "works_for", Limit: 3}
	if _, err := factsAbout(t, client, in); err != nil {
		t.Fatalf("facts_about: %v", err)
	}
	statement, params, _ := rec.find("predicate = :predicate")
	if params["predicate"] != "works_for" {
		t.Fatalf("predicate = %v", params["predicate"])
	}
	if !strings.Contains(statement, "LIMIT 3") {
		t.Fatalf("caller limit ignored: %s", statement)
	}
}

func TestFactsAboutRejectsAnEmptyEntityAndABadAsOf(t *testing.T) {
	client, rec := newRecordingDB(t)
	if _, err := factsAbout(t, client, MemoryFactsAboutInput{Entity: " "}); err == nil {
		t.Fatal("expected an error for an empty entity")
	}
	if _, err := factsAbout(t, client, MemoryFactsAboutInput{Entity: "Davide", AsOf: "soon"}); err == nil {
		t.Fatal("expected an error for a malformed as_of")
	}
	if len(rec.statements) != 0 {
		t.Fatal("nothing should be queried for invalid input")
	}
}

func TestMemoryRecallSelectsOneDeterministicReadPath(t *testing.T) {
	t.Run("exact entity walks the graph", func(t *testing.T) {
		client, rec := newRecordingDB(t, oneFactRow)
		_, out, err := memoryRecallHandler(singleTenant(t, client))(
			context.Background(), reqWithIdentity(testIdentity), MemoryRecallInput{
				Entity:    "Davide",
				Predicate: "lives_in",
			},
		)
		if err != nil {
			t.Fatalf("memory_recall entity: %v", err)
		}
		if out.Retrieval.Path != "graph" || len(out.Facts) != 1 {
			t.Fatalf("output = %+v", out)
		}
		if len(rec.statements) != 1 || !strings.Contains(rec.statements[0], "outV().name = :entity") {
			t.Fatalf("statements = %v, want one graph traversal", rec.statements)
		}
	})

	t.Run("natural-language query uses ranked retrieval", func(t *testing.T) {
		client, rec := newRecordingDB(t, oneFactRow)
		_, out, err := memoryRecallHandler(singleTenant(t, client))(
			context.Background(), reqWithIdentity(testIdentity), MemoryRecallInput{
				Query: "where does Davide live",
			},
		)
		if err != nil {
			t.Fatalf("memory_recall query: %v", err)
		}
		if out.Retrieval.Path != "lexical" || len(out.Facts) != 1 {
			t.Fatalf("output = %+v", out)
		}
		if _, _, ok := rec.find("SEARCH_INDEX"); !ok {
			t.Fatalf("statements = %v, want ranked retrieval", rec.statements)
		}
	})
}

func TestMemoryRecallRejectsAnEmptySelectorBeforeAccessingATenant(t *testing.T) {
	client, rec := newRecordingDB(t)
	_, _, err := memoryRecallHandler(singleTenant(t, client))(
		context.Background(), reqWithIdentity(testIdentity), MemoryRecallInput{},
	)
	if err == nil || !strings.Contains(err.Error(), "query or entity") {
		t.Fatalf("err = %v", err)
	}
	if len(rec.statements) != 0 {
		t.Fatalf("statements = %v, want no database access", rec.statements)
	}
}

// TestMemoryRecallRejectsMissingIdentityBeforeSelector proves the ordering the
// new identity-first check establishes: a call with NO identity refuses on the
// identity, even before the empty-selector validation would have fired.
func TestMemoryRecallRejectsMissingIdentityBeforeSelector(t *testing.T) {
	client, rec := newRecordingDB(t)
	_, _, err := memoryRecallHandler(singleTenant(t, client))(
		context.Background(), nil, MemoryRecallInput{},
	)
	if err == nil || !errors.Is(err, errMissingOAuthSubject) {
		t.Fatalf("err = %v, want errMissingOAuthSubject", err)
	}
	if len(rec.statements) != 0 {
		t.Fatalf("statements = %v, want no database access", rec.statements)
	}
}
