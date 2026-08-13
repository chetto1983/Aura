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
	// languages parallels statements. Only a caller that batches cares — the write
	// path sends "sqlscript" for a whole batch and "sql" for the per-fact fallback,
	// and that distinction is invisible in the statements themselves.
	languages []string
	// failLanguage makes the server refuse requests in that language, which is how
	// a test reaches the fallback branch without a real poisoned row.
	failLanguage string
	body         string
}

func recordingClient(t *testing.T, body string) (*Client, *recorder) {
	t.Helper()
	rec := &recorder{body: body}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		var payload struct {
			Command  string         `json:"command"`
			Params   map[string]any `json:"params"`
			Language string         `json:"language"`
		}
		_ = json.Unmarshal(raw, &payload)
		rec.statements = append(rec.statements, payload.Command)
		rec.params = append(rec.params, payload.Params)
		rec.languages = append(rec.languages, payload.Language)
		if rec.failLanguage != "" && payload.Language == rec.failLanguage {
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = io.WriteString(w, `{"error":"refused"}`)
			return
		}
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
		Subject:   "Davide",
		Predicate: "lives_in",
		Object:    "Caraglio",
		Statement: "Davide lives in Caraglio.",
		Source:    FactSource{RunID: "run-1"},
	}
}

const oneFactRow = `{"result":[{"statement":"Davide lives in Caraglio.","predicate":"lives_in",
"subject":"Davide","object":"Caraglio","valid_from":"2026-01-01T00:00:00Z",
"sources":[{"run_id":"run-1","memory_ids":["m1"]}]}]}`

const oneFactRowWithKey = `{"result":[{"statement":"Davide lives in Caraglio.","predicate":"lives_in",
"subject":"Davide","object":"Caraglio","valid_from":"2026-01-01T00:00:00Z","fact_key":"key-abc123",
"sources":[{"run_id":"run-1","memory_ids":["m1"]}]}]}`

// This test used to assert the OPPOSITE — that the schema carried no vector
// index at all — on the reasoning that retrieval was the graph plus Lucene and a
// vector index would reintroduce an embedding call on every read and write.
//
// Measurement overruled it. Lexical-only retrieval cannot cross a language
// boundary, and the facts are written in English while the operator asks in
// Italian: `analyzer recall Italian English` returned the right fact first,
// `perche la ricerca testuale rendeva la meta in italiano` returned ZERO. With
// the dense leg fused in, the same Italian question returns that same fact
// first, and lexical alone still returns zero on it.
//
// The original concern was right about the cost and wrong about the trade: the
// embedding call is now fail-soft on both paths (a fact writes without its
// vector, a search falls back to lexical), so the dependency it feared cannot
// take retrieval down.
func TestSchemaIsIdempotentAndCarriesTheVectorIndex(t *testing.T) {
	client, rec := recordingClient(t, `{"result":[]}`)
	if err := client.EnsureMemorySchema(context.Background()); err != nil {
		t.Fatalf("EnsureMemorySchema: %v", err)
	}
	for _, statement := range rec.statements {
		if strings.HasPrefix(statement, "CREATE") && !strings.Contains(statement, "IF NOT EXISTS") {
			t.Fatalf("statement is not replayable on a warm database: %s", statement)
		}
	}
	all := rec.joined()
	if !strings.Contains(all, "LSM_VECTOR") {
		t.Fatalf("the dense leg's index is missing:\n%s", all)
	}
	// The width is part of the index definition and the embedder's native size;
	// a mismatch is rejected at query time, so it belongs in the schema contract.
	if !strings.Contains(all, `"dimensions": 768`) {
		t.Errorf("vector index is not declared at the embedder's 768 dimensions:\n%s", all)
	}
	// ARRAY_OF_FLOATS, not LIST OF FLOAT: a JSON array bound through the SQL
	// endpoint arrives as the former, and the latter rejects it at write time.
	if !strings.Contains(all, "embedding IF NOT EXISTS ARRAY_OF_FLOATS") {
		t.Errorf("embedding property is not declared as ARRAY_OF_FLOATS:\n%s", all)
	}
	if !strings.Contains(all, "ON FACT (statement) FULL_TEXT") {
		t.Fatalf("full-text index missing:\n%s", all)
	}
	// StandardAnalyzer does not stem, so "works for" would be invisible to a
	// question asking "who does X work for".
	if !strings.Contains(all, "EnglishAnalyzer") {
		t.Fatalf("full-text index is not stemmed:\n%s", all)
	}
	if !strings.Contains(all, "FACT.sources IF NOT EXISTS LIST OF MAP") ||
		!strings.Contains(all, "ON FACT (fact_key) UNIQUE") {
		t.Fatalf("multi-source exact identity schema missing:\n%s", all)
	}
	if strings.Contains(all, "CREATE PROPERTY FACT.source_run_id") ||
		strings.Contains(all, "CREATE PROPERTY FACT.source_memory_ids") {
		t.Fatalf("retired provenance properties recreated:\n%s", all)
	}
}

// `end` is reserved by ArcadeDB both as a bare identifier and as a bind
// parameter name; the schema must never use it.
func TestSchemaAvoidsReservedWords(t *testing.T) {
	for _, statement := range append(memorySchemaStatements(), vectorSchemaStatements()...) {
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
		"provenance": func(f *Fact) { f.Source.RunID = "" },
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

func TestValidateRejectsOversizedFactInputs(t *testing.T) {
	cases := map[string]func(*Fact){
		"subject":    func(f *Fact) { f.Subject = strings.Repeat("界", 513) },
		"object":     func(f *Fact) { f.Object = strings.Repeat("界", 513) },
		"predicate":  func(f *Fact) { f.Predicate = strings.Repeat("界", 101) },
		"statement":  func(f *Fact) { f.Statement = strings.Repeat("界", 4097) },
		"source run": func(f *Fact) { f.Source.RunID = strings.Repeat("界", 101) },
		"source id":  func(f *Fact) { f.Source.MemoryIDs = []string{strings.Repeat("界", 101)} },
		"source id count": func(f *Fact) {
			f.Source.MemoryIDs = make([]string, 65)
		},
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			fact := validFact()
			mutate(&fact)
			if err := fact.Validate(); err == nil {
				t.Fatal("oversized fact accepted")
			}
		})
	}
}

// MEM-05: an object must name an entity, not carry prose. The bound was
// measured 2026-08-13 against mem_b130c94d_a213_463a_a797_ec124104363a (the
// live operator identity graph): the longest legitimate Entity.name in use
// is the operator identity UUID below, at 36 runes; the shortest measured
// prose violation lacking terminal punctuation
// ("verificare sempre il nome esatto di source/target prima di
// memory_merge_entities o memory_forget") is 96 runes. proseObjectRuneBound
// sits strictly between them.
func TestValidateRejectsProseObject(t *testing.T) {
	if proseObjectRuneBound >= defaultMemoryLimits.EntityRunes {
		t.Fatalf("prose bound %d must be strictly tighter than EntityRunes %d",
			proseObjectRuneBound, defaultMemoryLimits.EntityRunes)
	}
	const longestLegitimateMeasuredObject = "b130c94d-a213-463a-a797-ec124104363a"                                                            // 36 runes, live-measured
	const shortestMeasuredProseViolation = "verificare sempre il nome esatto di source/target prima di memory_merge_entities o memory_forget" // 96 runes, live-measured
	cases := map[string]struct {
		object    string
		wantError bool
	}{
		"short entity name accepted":                     {object: "Caraglio"},
		"longest measured legitimate object accepted":    {object: longestLegitimateMeasuredObject},
		"at the bound accepted":                          {object: strings.Repeat("a", proseObjectRuneBound)},
		"one over the bound rejected":                    {object: strings.Repeat("a", proseObjectRuneBound+1), wantError: true},
		"newline rejected":                               {object: "Caraglio\nTorino", wantError: true},
		"sentence-terminal period rejected":              {object: "Davide lives in Caraglio.", wantError: true},
		"sentence-terminal question mark rejected":       {object: "Where does Davide live?", wantError: true},
		"sentence-terminal exclamation rejected":         {object: "Move to Caraglio now!", wantError: true},
		"trailing whitespace after punctuation rejected": {object: "Davide lives in Caraglio.   ", wantError: true},
		"measured prose without terminal punctuation rejected": {
			object: shortestMeasuredProseViolation, wantError: true,
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			fact := validFact()
			fact.Object = tc.object
			err := fact.Validate()
			if tc.wantError && err == nil {
				t.Fatalf("expected rejection for object %q", tc.object)
			}
			if !tc.wantError && err != nil {
				t.Fatalf("expected acceptance for object %q, got %v", tc.object, err)
			}
			if tc.wantError && err != nil && !strings.Contains(err.Error(), "object") {
				t.Fatalf("error must name the object field: %v", err)
			}
			if tc.wantError && err != nil && !strings.Contains(err.Error(), "statement") {
				t.Fatalf("error must say the detail belongs in statement: %v", err)
			}
		})
	}
}

// MEM-05 is about the object endpoint. A subject that reads as a sentence is
// unaffected -- MEM-04's subject work is plan 45-07's.
func TestValidateProseRuleAppliesOnlyToObjectNotSubject(t *testing.T) {
	fact := validFact()
	fact.Subject = "This looks like a full sentence, not an entity name."
	if err := fact.Validate(); err != nil {
		t.Fatalf("the prose rule must not apply to subject: %v", err)
	}
}

// Pure function: the same fact validated twice yields the same verdict, and
// a rejected write leaves no partial state because validation runs before
// any statement is issued.
func TestValidateProseRuleIsIdempotent(t *testing.T) {
	fact := validFact()
	fact.Object = "This is a full sentence shaped like the learned_lesson prose objects."
	err1 := fact.Validate()
	err2 := fact.Validate()
	if (err1 == nil) != (err2 == nil) {
		t.Fatalf("validate is not idempotent: first=%v second=%v", err1, err2)
	}
	if err1 == nil {
		t.Fatal("this fixture must be rejected for the idempotency check to be meaningful")
	}
}

func TestUpsertFactCreatesEntitiesThenTheEdge(t *testing.T) {
	client, rec := recordingClient(t, `{"result":[]}`)
	if _, err := client.UpsertFact(context.Background(), validFact(), now); err != nil {
		t.Fatalf("UpsertFact: %v", err)
	}
	if len(rec.statements) != 5 {
		t.Fatalf("statements = %d, want two entity upserts, stale-key release, exact lookup, then edge:\n%s",
			len(rec.statements), rec.joined())
	}
	if !strings.Contains(rec.statements[0], "UPSERT") || !strings.Contains(rec.statements[1], "UPSERT") {
		t.Fatalf("entities not upserted first:\n%s", rec.joined())
	}
	if !strings.Contains(rec.statements[2], "SET fact_key = NULL") {
		t.Fatalf("expired fact key release missing: %s", rec.statements[2])
	}
	if !strings.Contains(rec.statements[3], "WHERE fact_key = :fact_key") {
		t.Fatalf("exact replay lookup missing: %s", rec.statements[3])
	}
	if !strings.HasPrefix(rec.statements[4], "CREATE EDGE FACT") {
		t.Fatalf("edge not created last: %s", rec.statements[4])
	}
	params := rec.params[4]
	if params["valid_from"] != now.Format(time.RFC3339) {
		t.Fatalf("valid_from = %v, want a default of now", params["valid_from"])
	}
	if params["valid_to"] != nil {
		t.Fatalf("valid_to = %v, want nil while the fact holds", params["valid_to"])
	}
	if params["created_at"] != now.Format(time.RFC3339) {
		t.Fatalf("created_at = %v", params["created_at"])
	}
	sources, ok := params["sources"].([]any)
	if !ok || len(sources) != 1 || sources[0].(map[string]any)["run_id"] != "run-1" {
		t.Fatalf("provenance lost: %v", params["sources"])
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
	if !strings.Contains(statement, "$score >= :min_lexical_score") ||
		rec.params[0]["min_lexical_score"] != float64(2) {
		t.Fatalf("lexical relevance gate missing: %s params=%v", statement, rec.params[0])
	}
	if !strings.Contains(statement, "outV().name AS subject") ||
		!strings.Contains(statement, "inV().name AS object") {
		t.Fatalf("endpoints not read with outV()/inV(): %s", statement)
	}
	if len(hits) != 1 || hits[0].Subject != "Davide" || len(hits[0].Sources) != 1 || hits[0].Sources[0].RunID != "run-1" {
		t.Fatalf("hits = %+v", hits)
	}
}

// fact_key is what a correction names to close exactly one edge (D-15); a
// hit that omits it cannot be targeted, only guessed at.
func TestSearchFactsCarriesFactKey(t *testing.T) {
	client, rec := recordingClient(t, oneFactRowWithKey)
	hits, err := client.SearchFacts(context.Background(), "where does Davide live", 3, time.Time{})
	if err != nil {
		t.Fatalf("SearchFacts: %v", err)
	}
	if !strings.Contains(rec.statements[0], "fact_key") {
		t.Fatalf("search projection omits fact_key: %s", rec.statements[0])
	}
	if len(hits) != 1 || hits[0].FactKey != "key-abc123" {
		t.Fatalf("hits = %+v, want fact_key key-abc123", hits)
	}
}

func TestLexicalScoreFloorDistinguishesLookupFromUnsupportedClaim(t *testing.T) {
	for _, test := range []struct {
		name  string
		query string
		want  float64
	}{
		{name: "exact lookup", query: "Caraglio", want: 0},
		{name: "punctuated lookup", query: "Torino?", want: 0},
		{name: "natural language", query: "Dove vive Davide?", want: 2},
	} {
		t.Run(test.name, func(t *testing.T) {
			if got := lexicalScoreFloor(test.query, 2); got != test.want {
				t.Fatalf("lexicalScoreFloor(%q) = %v, want %v", test.query, got, test.want)
			}
		})
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

func TestSearchFactsEscapesLuceneSyntax(t *testing.T) {
	client, rec := recordingClient(t, `{"result":[]}`)
	if _, err := client.SearchFacts(context.Background(), `where? "now"`, 3, now); err != nil {
		t.Fatalf("SearchFacts: %v", err)
	}
	if got := rec.params[0]["query"]; got != `where\? \"now\"` {
		t.Fatalf("query = %q, want escaped Lucene text", got)
	}
}

func TestSearchFactsCapsQueryAndResults(t *testing.T) {
	client, rec := recordingClient(t, `{"result":[]}`)
	if _, err := client.SearchFacts(context.Background(), strings.Repeat("界", 2049), 1, now); err == nil {
		t.Fatal("oversized query accepted")
	}
	if len(rec.statements) != 0 {
		t.Fatal("oversized query reached ArcadeDB")
	}
	if _, err := client.SearchFacts(context.Background(), "bounded", 1000, now); err != nil {
		t.Fatalf("SearchFacts: %v", err)
	}
	if !strings.Contains(rec.statements[0], "LIMIT 100") {
		t.Fatalf("result cap missing: %s", rec.statements[0])
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

// factHitFromRow is the single mapper both readers share (REUSABLE CODE,
// 45-PATTERNS.md); this proves FactsAbout gets fact_key through it exactly
// like SearchFacts does above, not via a second mapping.
func TestFactsAboutCarriesFactKey(t *testing.T) {
	client, rec := recordingClient(t, oneFactRowWithKey)
	hits, err := client.FactsAbout(context.Background(), "Davide", "", 0, time.Time{})
	if err != nil {
		t.Fatalf("FactsAbout: %v", err)
	}
	if !strings.Contains(rec.statements[0], "fact_key") {
		t.Fatalf("facts-about projection omits fact_key: %s", rec.statements[0])
	}
	if len(hits) != 1 || hits[0].FactKey != "key-abc123" {
		t.Fatalf("hits = %+v, want fact_key key-abc123", hits)
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

func TestFactsAboutCapsEntityPredicateAndResults(t *testing.T) {
	client, rec := recordingClient(t, `{"result":[]}`)
	if _, err := client.FactsAbout(context.Background(), strings.Repeat("界", 513), "", 1, now); err == nil {
		t.Fatal("oversized entity accepted")
	}
	if _, err := client.FactsAbout(context.Background(), "Davide", strings.Repeat("界", 101), 1, now); err == nil {
		t.Fatal("oversized predicate accepted")
	}
	if len(rec.statements) != 0 {
		t.Fatal("oversized traversal input reached ArcadeDB")
	}
	if _, err := client.FactsAbout(context.Background(), "Davide", "", 1000, now); err != nil {
		t.Fatalf("FactsAbout: %v", err)
	}
	if !strings.Contains(rec.statements[0], "LIMIT 100") {
		t.Fatalf("result cap missing: %s", rec.statements[0])
	}
}
