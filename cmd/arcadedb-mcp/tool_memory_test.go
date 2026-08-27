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

// handleTransactionEndpoints answers ArcadeDB's explicit-transaction
// lifecycle (internal/arcadedb's transaction.go: begin/commit/rollback)
// with a synthetic session before this mock's own statement recording ever
// sees the request -- recordingDB exists to assert on the SQL statements
// memoryUpsertFactHandler's writes send, not to model ArcadeDB's
// transaction protocol (exercised for real only against the live server).
func handleTransactionEndpoints(w http.ResponseWriter, r *http.Request) bool {
	switch {
	case strings.Contains(r.URL.Path, "/api/v1/begin/"):
		w.Header().Set("arcadedb-session-id", "AS-mock-session")
		w.WriteHeader(http.StatusNoContent)
		return true
	case strings.Contains(r.URL.Path, "/api/v1/commit/"), strings.Contains(r.URL.Path, "/api/v1/rollback/"):
		w.WriteHeader(http.StatusNoContent)
		return true
	}
	return false
}

func newRecordingDB(t *testing.T, responses ...string) (*arcadedb.Client, *recordingDB) {
	t.Helper()
	rec := &recordingDB{responses: responses, status: http.StatusOK}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if handleTransactionEndpoints(w, r) {
			return
		}
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
		Subject:     "Davide",
		SubjectKind: "Person",
		Predicate:   "lives_in",
		Object:      "Caraglio",
		ObjectKind:  "Location",
		Statement:   "Davide lives in Caraglio.",
		Source:      MemoryUpsertFactWriteSource{MemoryIDs: []string{"mem-1"}},
	}
}

// upsert routes every non-actor-specific test through a fixed, valid PARENT
// actor (testParentRunID) — the actor itself is not what these tests are
// about. tool_memory_actor_test.go covers the actor derivation directly:
// a missing/unknown actor, and a worker's supersede refusal.
func upsert(
	t *testing.T,
	client *arcadedb.Client,
	in MemoryUpsertFactInput,
) (MemoryUpsertFactOutput, error) {
	t.Helper()
	_, out, err := memoryUpsertFactHandler(singleTenant(t, client), testClock, "")(
		context.Background(), reqWithParentActor(testIdentity, testParentRunID), in)
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
	// run_id is no longer a field the caller supplied (D-10): it is
	// host-derived, so what shows up here is upsert()'s fixed test actor,
	// never the "run-1" this test used to pass in.
	if !ok || source["run_id"] != testParentRunID {
		t.Fatalf("source = %v, want host-derived run_id %q", sources[0], testParentRunID)
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

// D-15: supersedes_fact_key targets exactly the one fact it names, skipping
// the legacy subject+predicate candidate resolution entirely.
func TestUpsertFactClosesByFactKey(t *testing.T) {
	client, rec := newRecordingDB(t,
		`{"result":[]}`,            // entity upsert: subject
		`{"result":[]}`,            // entity upsert: object
		`{"result":[]}`,            // attachFactSource: release expired identity
		`{"result":[]}`,            // attachFactSource: exact replay lookup
		`{"result":[{"count":1}]}`, // the exact-match close by fact_key
	)
	in := validFactInput()
	in.SupersedesFactKey = "target-key-abc"
	out, err := upsert(t, client, in)
	if err != nil {
		t.Fatalf("upsert: %v", err)
	}
	if out.Refused {
		t.Fatalf("output = %+v, want refused=false", out)
	}
	if out.Superseded != 1 {
		t.Fatalf("superseded = %d, want 1", out.Superseded)
	}
	statement, params, ok := rec.find("WHERE fact_key = :target_fact_key")
	if !ok {
		t.Fatalf("no exact-match close by fact_key issued; statements = %v", rec.statements)
	}
	if params["target_fact_key"] != "target-key-abc" {
		t.Fatalf("target_fact_key = %v, want the supplied key", params["target_fact_key"])
	}
	if strings.Contains(statement, "outV().name = :subject_name") {
		t.Fatalf("fact_key close must not use the legacy subject+predicate WHERE: %s", statement)
	}
	if _, _, found := rec.find("outV().name = :entity OR inV().name = :entity"); found {
		t.Fatalf("fact_key close must skip subject+predicate candidate resolution: %v", rec.statements)
	}
}

// D-15: a supersedes_fact_key naming no still-valid fact refuses -- as a
// successful call, never an mcp.ToolCallError (D-17) -- and writes nothing.
func TestUpsertFactRefusesOnFactKeyMiss(t *testing.T) {
	client, rec := newRecordingDB(t,
		`{"result":[]}`, // entity upsert: subject
		`{"result":[]}`, // entity upsert: object
		`{"result":[]}`, // attachFactSource: release expired identity
		`{"result":[]}`, // attachFactSource: exact replay lookup
		`{"result":[]}`, // the exact-match close: 0 rows -> refusal
	)
	in := validFactInput()
	in.SupersedesFactKey = "nonexistent-key"
	out, err := upsert(t, client, in)
	if err != nil {
		t.Fatalf("a refusal must return a nil error (D-17), got: %v", err)
	}
	if !out.Refused {
		t.Fatalf("output = %+v, want refused=true", out)
	}
	if out.Superseded != 0 {
		t.Fatalf("superseded = %d, want 0 on refusal", out.Superseded)
	}
	if strings.TrimSpace(out.Reason) == "" {
		t.Fatal("refusal has no reason")
	}
	if _, _, found := rec.find("CREATE EDGE"); found {
		t.Fatalf("a refused correction must create no fact; statements = %v", rec.statements)
	}
}

// D-16: the legacy supersedes:true path refuses on more than one distinct
// candidate, returns every preview (with its fact_key, so the model can
// disambiguate), and creates nothing.
func TestUpsertFactRefusesOnAmbiguousSupersede(t *testing.T) {
	const twoFactRows = `{"result":[
{"statement":"Davide lives in Caraglio.","predicate":"lives_in","subject":"Davide","subject_kind":"Person",
"object":"Caraglio","object_kind":"Location","valid_from":"2026-01-01T00:00:00Z",
"sources":[{"run_id":"run-1","memory_ids":["mem-1"]}],"fact_key":"key-a"},
{"statement":"Davide lives in Torino.","predicate":"lives_in","subject":"Davide","subject_kind":"Person",
"object":"Torino","object_kind":"Location","valid_from":"2026-02-01T00:00:00Z",
"sources":[{"run_id":"run-2","memory_ids":["mem-2"]}],"fact_key":"key-b"}]}`
	client, rec := newRecordingDB(t,
		`{"result":[]}`,
		`{"result":[]}`,
		`{"result":[]}`,
		`{"result":[]}`,
		twoFactRows,
	)
	in := validFactInput()
	in.Supersedes = true
	out, err := upsert(t, client, in)
	if err != nil {
		t.Fatalf("a refusal must return a nil error (D-17), got: %v", err)
	}
	if !out.Refused || out.Superseded != 0 {
		t.Fatalf("output = %+v, want refused=true, superseded=0", out)
	}
	if len(out.Candidates) != 2 {
		t.Fatalf("candidates = %+v, want both previews", out.Candidates)
	}
	for _, candidate := range out.Candidates {
		if candidate.FactKey == "" {
			t.Fatalf("candidate preview missing fact_key, the value needed to disambiguate: %+v", candidate)
		}
	}
	if !strings.Contains(out.Reason, "supersedes_fact_key") {
		t.Fatalf("reason = %q, want it to name supersedes_fact_key as the disambiguation path", out.Reason)
	}
	if _, _, found := rec.find("CREATE EDGE"); found {
		t.Fatal("an ambiguous refusal must create no fact")
	}
}
