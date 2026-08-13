package main

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/chetto1983/aura/internal/arcadedb"
)

// fixedNow is the instant every test asserts against, so a written timestamp is
// checkable rather than merely present.
var fixedNow = time.Date(2026, 7, 31, 12, 0, 0, 0, time.UTC)

func testClock() time.Time { return fixedNow }

// recordingDB captures every statement the client sends so the SQL itself can
// be asserted on -- the reserved-word and bitemporal contracts live in the SQL.
type recordingDB struct {
	statements []string
	params     []map[string]any
	responses  []string
	status     int
}

func newRecordingDB(t *testing.T, responses ...string) (*arcadedb.Client, *recordingDB) {
	t.Helper()
	rec := &recordingDB{responses: responses, status: http.StatusOK}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		var payload struct {
			Command string         `json:"command"`
			Params  map[string]any `json:"params"`
		}
		_ = json.Unmarshal(raw, &payload)
		rec.statements = append(rec.statements, payload.Command)
		rec.params = append(rec.params, payload.Params)
		body := `{"result":[]}`
		if idx := len(rec.statements) - 1; idx < len(rec.responses) {
			body = rec.responses[idx]
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(rec.status)
		_, _ = io.WriteString(w, body)
	}))
	t.Cleanup(srv.Close)
	client, err := arcadedb.New(arcadedb.Config{
		BaseURL: srv.URL, Database: "aura", User: "root", Password: "pw",
	})
	if err != nil {
		t.Fatalf("arcadedb.New: %v", err)
	}
	return client, rec
}

func (r *recordingDB) find(fragment string) (string, map[string]any, bool) {
	for i, statement := range r.statements {
		if strings.Contains(statement, fragment) {
			return statement, r.params[i], true
		}
	}
	return "", nil, false
}

func validFactInput() MemoryUpsertFactInput {
	return MemoryUpsertFactInput{
		UserIdentifier: testIdentity,
		Subject:        "Davide",
		SubjectKind:    "Person",
		Predicate:      "lives_in",
		Object:         "Caraglio",
		ObjectKind:     "Location",
		Statement:      "Davide lives in Caraglio.",
		Source:         MemoryFactSource{RunID: "run-1", MemoryIDs: []string{"mem-1"}},
	}
}

func upsert(
	t *testing.T,
	client *arcadedb.Client,
	in MemoryUpsertFactInput,
) (MemoryUpsertFactOutput, error) {
	t.Helper()
	_, out, err := memoryUpsertFactHandler(singleTenant(t, client), testClock)(
		context.Background(), nil, in)
	return out, err
}

func TestUpsertFactWritesTheEdgeWithBothTimeAxes(t *testing.T) {
	client, rec := newRecordingDB(t)
	if _, err := upsert(t, client, validFactInput()); err != nil {
		t.Fatalf("upsert: %v", err)
	}
	statement, params, ok := rec.find("CREATE EDGE")
	if !ok {
		t.Fatalf("no CREATE EDGE issued; statements = %v", rec.statements)
	}
	for _, field := range []string{"valid_from", "valid_to", "created_at", "sources", "fact_key"} {
		if !strings.Contains(statement, field) {
			t.Fatalf("CREATE EDGE missing %s: %s", field, statement)
		}
	}
	if params["created_at"] != fixedNow.Format(time.RFC3339) {
		t.Fatalf("created_at = %v, want the injected clock", params["created_at"])
	}
	if params["valid_from"] != fixedNow.Format(time.RFC3339) {
		t.Fatalf("valid_from = %v, want a default of now", params["valid_from"])
	}
	if params["valid_to"] != nil {
		t.Fatalf("valid_to = %v, want nil while the fact still holds", params["valid_to"])
	}
}

// The whole design rests on the statement being searchable, so the embedding
// must be the statement's -- not the subject's or the predicate's.
func TestUpsertFactStoresProvenance(t *testing.T) {
	client, rec := newRecordingDB(t)
	if _, err := upsert(t, client, validFactInput()); err != nil {
		t.Fatalf("upsert: %v", err)
	}
	_, params, _ := rec.find("CREATE EDGE")
	sources, ok := params["sources"].([]any)
	if !ok || len(sources) != 1 {
		t.Fatalf("sources = %v", params["sources"])
	}
	source, ok := sources[0].(map[string]any)
	if !ok || source["run_id"] != "run-1" {
		t.Fatalf("source = %v", sources[0])
	}
	ids, ok := source["memory_ids"].([]any)
	if !ok || len(ids) != 1 || ids[0] != "mem-1" {
		t.Fatalf("source memory ids = %v", source["memory_ids"])
	}
}

// Superseding closes the previous window instead of deleting the row: that is
// what keeps "where did he live in 2024" answerable.
//
// arcadedb's D-16 ambiguity contract now resolves the subject+predicate
// candidate set before closing anything (0 or >1 distinct matches refuses
// instead of blindly closing) -- so this mock must answer the resolution
// SELECT with exactly one candidate for the close to be reached at all.
// oneFactRow's subject ("Davide") matches validFactInput() exactly.
func TestSupersedingClosesTheWindowAndDoesNotDelete(t *testing.T) {
	client, rec := newRecordingDB(t,
		`{"result":[]}`,            // entity upsert: subject
		`{"result":[]}`,            // entity upsert: object
		`{"result":[]}`,            // attachFactSource: release expired identity
		`{"result":[]}`,            // attachFactSource: exact replay lookup
		oneFactRow,                 // candidate resolution (D-16): exactly one still-valid match
		`{"result":[{"count":1}]}`, // the exact-match close this test asserts on
	)
	in := validFactInput()
	in.Supersedes = true
	if _, err := upsert(t, client, in); err != nil {
		t.Fatalf("upsert: %v", err)
	}
	statement, params, ok := rec.find("expired_at = :expired_at")
	if !ok {
		t.Fatalf("no supersede statement; statements = %v", rec.statements)
	}
	if strings.Contains(strings.ToUpper(statement), "DELETE") {
		t.Fatalf("supersede must not delete: %s", statement)
	}
	if !strings.Contains(statement, "valid_to IS NULL") {
		t.Fatalf("supersede must only close still-valid facts: %s", statement)
	}
	if params["expired_at"] != fixedNow.Format(time.RFC3339) {
		t.Fatalf("expired_at = %v", params["expired_at"])
	}
}

func TestUpsertFactWithoutSupersedesLeavesPriorFactsAlone(t *testing.T) {
	client, rec := newRecordingDB(t)
	if _, err := upsert(t, client, validFactInput()); err != nil {
		t.Fatalf("upsert: %v", err)
	}
	// Matching on "UPDATE FACT", not on "expired_at = :expired_at". The latter was
	// a fair proxy while only the supersede statement set that column; the fact
	// CREATE now sets it too, so that a merge can carry a closed fact's expiry
	// across when it re-points the edge. The proxy became ambiguous, the behaviour
	// did not.
	if _, _, found := rec.find("UPDATE FACT SET valid_to"); found {
		t.Fatal("no supersede UPDATE should be issued by default")
	}
}

func TestUpsertFactHonoursExplicitValidityWindow(t *testing.T) {
	client, rec := newRecordingDB(t)
	in := validFactInput()
	in.ValidFrom = "2024-01-01T00:00:00Z"
	in.ValidTo = "2025-06-30T00:00:00Z"
	if _, err := upsert(t, client, in); err != nil {
		t.Fatalf("upsert: %v", err)
	}
	_, params, _ := rec.find("CREATE EDGE")
	if params["valid_from"] != "2024-01-01T00:00:00Z" {
		t.Fatalf("valid_from = %v", params["valid_from"])
	}
	if params["valid_to"] != "2025-06-30T00:00:00Z" {
		t.Fatalf("valid_to = %v", params["valid_to"])
	}
}

func TestUpsertFactRejectsBadInput(t *testing.T) {
	cases := map[string]func(*MemoryUpsertFactInput){
		"no subject":     func(in *MemoryUpsertFactInput) { in.Subject = " " },
		"no predicate":   func(in *MemoryUpsertFactInput) { in.Predicate = "" },
		"no object":      func(in *MemoryUpsertFactInput) { in.Object = "" },
		"no statement":   func(in *MemoryUpsertFactInput) { in.Statement = "  " },
		"no provenance":  func(in *MemoryUpsertFactInput) { in.Source.RunID = "" },
		"bad valid_from": func(in *MemoryUpsertFactInput) { in.ValidFrom = "yesterday" },
		"bad valid_to":   func(in *MemoryUpsertFactInput) { in.ValidTo = "soon" },
		"window backwards": func(in *MemoryUpsertFactInput) {
			in.ValidFrom, in.ValidTo = "2026-01-01T00:00:00Z", "2025-01-01T00:00:00Z"
		},
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			client, rec := newRecordingDB(t)
			in := validFactInput()
			mutate(&in)
			if _, err := upsert(t, client, in); err == nil {
				t.Fatal("expected an error")
			}
			if len(rec.statements) != 0 {
				t.Fatal("a malformed fact must be rejected before anything is written")
			}
		})
	}
}

func search(
	t *testing.T,
	client *arcadedb.Client,
	in MemorySearchInput,
) (MemorySearchOutput, error) {
	t.Helper()
	_, out, err := memorySearchHandler(singleTenant(t, client))(context.Background(), nil, in)
	return out, err
}

const oneFactRow = `{"result":[{"statement":"Davide lives in Caraglio.","predicate":"lives_in",
"subject":"Davide","subject_kind":"Person","object":"Caraglio","object_kind":"Location",
"valid_from":"2026-01-01T00:00:00Z",
"sources":[{"run_id":"run-1","memory_ids":["mem-1"]}]}]}`

func TestSearchUsesTheFullTextIndexAndKeepsProvenance(t *testing.T) {
	client, rec := newRecordingDB(t, oneFactRow)
	out, err := search(t, client, MemorySearchInput{UserIdentifier: testIdentity, Query: "where does Davide live"})
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
	if _, err := search(t, client, MemorySearchInput{UserIdentifier: testIdentity, Query: "q"}); err != nil {
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
	_, err := search(t, client, MemorySearchInput{UserIdentifier: testIdentity, Query: "q", AsOf: "last year"})
	if err == nil || !strings.Contains(err.Error(), "as_of") {
		t.Fatalf("err = %v", err)
	}
}

func TestSearchOnEmptyGraphReturnsNoFactsNotAnError(t *testing.T) {
	client, _ := newRecordingDB(t, `{"result":[]}`)
	out, err := search(t, client, MemorySearchInput{UserIdentifier: testIdentity, Query: "q"})
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
	_, out, err := memoryFactsAboutHandler(singleTenant(t, client))(context.Background(), nil, in)
	return out, err
}

// The traversal path is the exact one: it must not rank, and must not need a
// query string at all.
func TestFactsAboutWalksTheGraphWithoutRanking(t *testing.T) {
	client, rec := newRecordingDB(t, oneFactRow)
	out, err := factsAbout(t, client, MemoryFactsAboutInput{UserIdentifier: testIdentity, Entity: "Davide"})
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
	out, err := factsAbout(t, client, MemoryFactsAboutInput{UserIdentifier: testIdentity, Entity: "Nobody"})
	if err != nil {
		t.Fatalf("facts_about: %v", err)
	}
	if !out.Retrieval.Abstained || out.Retrieval.Path != "graph" || out.Retrieval.Reason != "no_facts" {
		t.Fatalf("retrieval metadata = %+v", out.Retrieval)
	}
}

func TestFactsAboutNarrowsByPredicate(t *testing.T) {
	client, rec := newRecordingDB(t, oneFactRow)
	in := MemoryFactsAboutInput{UserIdentifier: testIdentity, Entity: "Davide", Predicate: "works_for", Limit: 3}
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
	if _, err := factsAbout(t, client, MemoryFactsAboutInput{UserIdentifier: testIdentity, Entity: " "}); err == nil {
		t.Fatal("expected an error for an empty entity")
	}
	if _, err := factsAbout(t, client, MemoryFactsAboutInput{UserIdentifier: testIdentity, Entity: "Davide", AsOf: "soon"}); err == nil {
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
			context.Background(), nil, MemoryRecallInput{
				UserIdentifier: testIdentity,
				Entity:         "Davide",
				Predicate:      "lives_in",
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
			context.Background(), nil, MemoryRecallInput{
				UserIdentifier: testIdentity,
				Query:          "where does Davide live",
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
		context.Background(), nil, MemoryRecallInput{UserIdentifier: testIdentity},
	)
	if err == nil || !strings.Contains(err.Error(), "query or entity") {
		t.Fatalf("err = %v", err)
	}
	if len(rec.statements) != 0 {
		t.Fatalf("statements = %v, want no database access", rec.statements)
	}
}
