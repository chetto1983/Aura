package arcadedb

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

var now = time.Date(2026, 7, 31, 12, 0, 0, 0, time.UTC)

type recorder struct {
	statements []string
	params     []map[string]any
	body       string
}

func recordingClient(t *testing.T, body string) (*Client, *recorder) {
	t.Helper()
	rec := &recorder{body: body}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		var payload struct {
			Command string         `json:"command"`
			Params  map[string]any `json:"params"`
		}
		_ = json.Unmarshal(raw, &payload)
		rec.statements = append(rec.statements, payload.Command)
		rec.params = append(rec.params, payload.Params)
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, rec.body)
	}))
	t.Cleanup(srv.Close)
	client, err := New(Config{BaseURL: srv.URL, Database: "aura", User: "root"})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return client, rec
}

func (r *recorder) joined() string { return strings.Join(r.statements, "\n") }

func validFact() Fact {
	return Fact{
		Subject:     "Davide",
		Predicate:   "lives_in",
		Object:      "Caraglio",
		Statement:   "Davide lives in Caraglio.",
		SourceRunID: "run-1",
	}
}

const oneFactRow = `{"result":[{"statement":"Davide lives in Caraglio.","predicate":"lives_in",
"subject":"Davide","object":"Caraglio","valid_from":"2026-01-01T00:00:00Z",
"source_run_id":"run-1","source_memory_ids":["m1"]}]}`

func TestSchemaIsIdempotentAndCarriesNoVectorIndex(t *testing.T) {
	client, rec := recordingClient(t, `{"result":[]}`)
	if err := client.EnsureMemorySchema(context.Background()); err != nil {
		t.Fatalf("EnsureMemorySchema: %v", err)
	}
	for _, statement := range rec.statements {
		if !strings.Contains(statement, "IF NOT EXISTS") {
			t.Fatalf("statement is not replayable on a warm database: %s", statement)
		}
	}
	all := rec.joined()
	// Retrieval is the graph plus Lucene; a vector index would reintroduce an
	// embedding call on every read and every write.
	if strings.Contains(all, "LSM_VECTOR") || strings.Contains(all, "embedding") {
		t.Fatalf("schema still carries a vector index:\n%s", all)
	}
	if !strings.Contains(all, "ON FACT (statement) FULL_TEXT") {
		t.Fatalf("full-text index missing:\n%s", all)
	}
	// StandardAnalyzer does not stem, so "works for" would be invisible to a
	// question asking "who does X work for".
	if !strings.Contains(all, "EnglishAnalyzer") {
		t.Fatalf("full-text index is not stemmed:\n%s", all)
	}
}

// `end` is reserved by ArcadeDB both as a bare identifier and as a bind
// parameter name; the schema must never use it.
func TestSchemaAvoidsReservedWords(t *testing.T) {
	for _, statement := range memorySchemaStatements() {
		lowered := strings.ToLower(statement)
		for _, reserved := range []string{" end ", ".end ", "(end)", ":end"} {
			if strings.Contains(lowered, reserved) {
				t.Fatalf("reserved word in %q", statement)
			}
		}
	}
}

func TestValidateRejectsIncompleteFacts(t *testing.T) {
	cases := map[string]func(*Fact){
		"subject":    func(f *Fact) { f.Subject = " " },
		"predicate":  func(f *Fact) { f.Predicate = "" },
		"object":     func(f *Fact) { f.Object = "" },
		"statement":  func(f *Fact) { f.Statement = "" },
		"provenance": func(f *Fact) { f.SourceRunID = "" },
		"window backwards": func(f *Fact) {
			f.ValidFrom = now
			f.ValidTo = now.Add(-time.Hour)
		},
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			fact := validFact()
			mutate(&fact)
			if err := fact.Validate(); err == nil {
				t.Fatal("expected an error")
			}
		})
	}
}

func TestUpsertFactCreatesEntitiesThenTheEdge(t *testing.T) {
	client, rec := recordingClient(t, `{"result":[]}`)
	if _, err := client.UpsertFact(context.Background(), validFact(), now); err != nil {
		t.Fatalf("UpsertFact: %v", err)
	}
	if len(rec.statements) != 3 {
		t.Fatalf("statements = %d, want two entity upserts then the edge:\n%s",
			len(rec.statements), rec.joined())
	}
	if !strings.Contains(rec.statements[0], "UPSERT") || !strings.Contains(rec.statements[1], "UPSERT") {
		t.Fatalf("entities not upserted first:\n%s", rec.joined())
	}
	if !strings.HasPrefix(rec.statements[2], "CREATE EDGE FACT") {
		t.Fatalf("edge not created last: %s", rec.statements[2])
	}
	params := rec.params[2]
	if params["valid_from"] != now.Format(time.RFC3339) {
		t.Fatalf("valid_from = %v, want a default of now", params["valid_from"])
	}
	if params["valid_to"] != nil {
		t.Fatalf("valid_to = %v, want nil while the fact holds", params["valid_to"])
	}
	if params["created_at"] != now.Format(time.RFC3339) {
		t.Fatalf("created_at = %v", params["created_at"])
	}
	if params["source_run_id"] != "run-1" {
		t.Fatalf("provenance lost: %v", params["source_run_id"])
	}
}

func TestUpsertFactSupersedesByClosingTheWindow(t *testing.T) {
	client, rec := recordingClient(t, `{"result":[{"count":2}]}`)
	fact := validFact()
	fact.Supersedes = true
	written, err := client.UpsertFact(context.Background(), fact, now)
	if err != nil {
		t.Fatalf("UpsertFact: %v", err)
	}
	if written.Superseded != 2 {
		t.Fatalf("superseded = %d, want 2", written.Superseded)
	}
	all := rec.joined()
	if strings.Contains(strings.ToUpper(all), "DELETE") {
		t.Fatalf("supersession must never delete:\n%s", all)
	}
	if !strings.Contains(all, "valid_to IS NULL") {
		t.Fatalf("only still-valid facts may be closed:\n%s", all)
	}
	// outV(), not the dotted form: on an edge `out.name` yields NULL rather
	// than erroring, so the statement would match nothing, silently.
	if !strings.Contains(all, "outV().name = :subject_name") {
		t.Fatalf("supersession must match the subject via outV():\n%s", all)
	}
	// The object is the thing that changed; requiring it means this never fires.
	if strings.Contains(all, "inV().name = :object_name") {
		t.Fatalf("supersession must not filter on the object:\n%s", all)
	}
}

func TestUpsertFactWithoutSupersedesLeavesPriorFactsAlone(t *testing.T) {
	client, rec := recordingClient(t, `{"result":[]}`)
	if _, err := client.UpsertFact(context.Background(), validFact(), now); err != nil {
		t.Fatalf("UpsertFact: %v", err)
	}
	// The assertion is on the UPDATE, not on the string "expired_at". It used to
	// look for `expired_at = :expired_at`, which was a fair proxy while only the
	// supersede statement set that column -- until createFactStatement started
	// setting it too, so a merge could carry a closed fact's expiry across when it
	// re-points the edge. The proxy became ambiguous; the behaviour did not.
	for _, statement := range rec.statements {
		if strings.HasPrefix(strings.TrimSpace(statement), "UPDATE "+factEdgeType) {
			t.Fatalf("no supersede UPDATE should be issued by default:\n%s", statement)
		}
	}
}

func TestSearchFactsUsesTheFullTextIndexAndReadsEndpoints(t *testing.T) {
	client, rec := recordingClient(t, oneFactRow)
	hits, err := client.SearchFacts(context.Background(), "where does Davide live", 3, time.Time{})
	if err != nil {
		t.Fatalf("SearchFacts: %v", err)
	}
	statement := rec.statements[0]
	if !strings.Contains(statement, "SEARCH_INDEX('FACT[statement]', :query)") {
		t.Fatalf("search does not use the full-text index: %s", statement)
	}
	if !strings.Contains(statement, "outV().name AS subject") ||
		!strings.Contains(statement, "inV().name AS object") {
		t.Fatalf("endpoints not read with outV()/inV(): %s", statement)
	}
	if len(hits) != 1 || hits[0].Subject != "Davide" || hits[0].SourceRunID != "run-1" {
		t.Fatalf("hits = %+v", hits)
	}
}

// A search with no instant means "what is true now", not "across all time":
// unfiltered, a superseded fact wins.
func TestSearchFactsAlwaysFiltersOnValidity(t *testing.T) {
	client, rec := recordingClient(t, oneFactRow)
	if _, err := client.SearchFacts(context.Background(), "q", 3, time.Time{}); err != nil {
		t.Fatalf("SearchFacts: %v", err)
	}
	statement := rec.statements[0]
	if !strings.Contains(statement, "valid_from <= :as_of") {
		t.Fatalf("missing lower bound: %s", statement)
	}
	if !strings.Contains(statement, "valid_to IS NULL OR valid_to > :as_of") {
		t.Fatalf("a still-open window must match: %s", statement)
	}
	if _, present := rec.params[0]["as_of"]; !present {
		t.Fatal("as_of must default to now")
	}
}

func TestSearchFactsHonoursAsOfAndRejectsAnEmptyQuery(t *testing.T) {
	client, rec := recordingClient(t, `{"result":[]}`)
	asOf := time.Date(2024, 6, 1, 0, 0, 0, 0, time.UTC)
	if _, err := client.SearchFacts(context.Background(), "q", 3, asOf); err != nil {
		t.Fatalf("SearchFacts: %v", err)
	}
	if rec.params[0]["as_of"] != asOf.Format(time.RFC3339) {
		t.Fatalf("as_of = %v", rec.params[0]["as_of"])
	}
	if _, err := client.SearchFacts(context.Background(), "  ", 3, time.Time{}); err == nil {
		t.Fatal("expected an error for a blank query")
	}
}

func TestFactsAboutWalksTheEntitysEdges(t *testing.T) {
	client, rec := recordingClient(t, oneFactRow)
	hits, err := client.FactsAbout(context.Background(), "Davide", "", 0, time.Time{})
	if err != nil {
		t.Fatalf("FactsAbout: %v", err)
	}
	statement := rec.statements[0]
	if !strings.Contains(statement, "outV().name = :entity") {
		t.Fatalf("traversal does not anchor on the subject: %s", statement)
	}
	// No ranking, no similarity: this path is exact.
	if strings.Contains(statement, "SEARCH_INDEX") || strings.Contains(statement, "vector.") {
		t.Fatalf("traversal must not rank: %s", statement)
	}
	if !strings.Contains(statement, "LIMIT 20") {
		t.Fatalf("default limit missing: %s", statement)
	}
	if len(hits) != 1 || hits[0].Object != "Caraglio" {
		t.Fatalf("hits = %+v", hits)
	}
}

func TestFactsAboutNarrowsByPredicateAndInstant(t *testing.T) {
	client, rec := recordingClient(t, oneFactRow)
	asOf := time.Date(2024, 6, 1, 0, 0, 0, 0, time.UTC)
	if _, err := client.FactsAbout(context.Background(), "Davide", "works_for", 5, asOf); err != nil {
		t.Fatalf("FactsAbout: %v", err)
	}
	statement := rec.statements[0]
	if !strings.Contains(statement, "predicate = :predicate") {
		t.Fatalf("predicate filter missing: %s", statement)
	}
	if !strings.Contains(statement, "valid_to IS NULL OR valid_to > :as_of") {
		t.Fatalf("temporal filter missing: %s", statement)
	}
	if rec.params[0]["predicate"] != "works_for" || rec.params[0]["as_of"] != asOf.Format(time.RFC3339) {
		t.Fatalf("params = %v", rec.params[0])
	}
	if !strings.Contains(statement, "LIMIT 5") {
		t.Fatalf("caller limit ignored: %s", statement)
	}
}

func TestFactsAboutRejectsAnEmptyEntity(t *testing.T) {
	client, rec := recordingClient(t, `{"result":[]}`)
	if _, err := client.FactsAbout(context.Background(), "  ", "", 5, time.Time{}); err == nil {
		t.Fatal("expected an error")
	}
	if len(rec.statements) != 0 {
		t.Fatal("nothing should be queried for an empty entity")
	}
}
