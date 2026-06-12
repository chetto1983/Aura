package toolselectstore

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
)

// fakeGraph is a deterministic in-memory GraphClient. Save records the params the
// store sent (so the test can assert the UNWIND $rows shape + the content-hash key);
// Read replays a fixed row set (so the test can assert asFloats parses the APOC-JSON
// string form). No Neo4j needed — covers the store's param-shaping + parsing in CI.
type fakeGraph struct {
	lastWriteQuery  string
	lastWriteParams map[string]any
	readRows        []map[string]any
	readErr         error
}

func (f *fakeGraph) Read(_ context.Context, _ string, _ map[string]any) ([]map[string]any, error) {
	return f.readRows, f.readErr
}

func (f *fakeGraph) Write(_ context.Context, query string, params map[string]any) ([]map[string]any, error) {
	f.lastWriteQuery = query
	f.lastWriteParams = params
	return nil, nil
}

func TestStore_Save_NestsEmbeddingInUnwindRows(t *testing.T) {
	g := &fakeGraph{}
	s := &Store{Client: g}
	vec := []float64{0.1, -0.2, 0.3}
	if err := s.Save(context.Background(), "find me a restaurant", "web_search", vec); err != nil {
		t.Fatalf("Save: %v", err)
	}

	// The embedding MUST be nested under $rows (the write workaround) — a top-level
	// $embedding param would be dropped by mcp-neo4j-cypher.
	rowsAny, ok := g.lastWriteParams["rows"]
	if !ok {
		t.Fatalf("write params missing $rows key: %v", g.lastWriteParams)
	}
	rows, ok := rowsAny.([]map[string]any)
	if !ok || len(rows) != 1 {
		t.Fatalf("$rows = %#v, want one-row []map[string]any", rowsAny)
	}
	row := rows[0]
	if row["tool"] != "web_search" {
		t.Errorf("row tool = %v, want web_search", row["tool"])
	}
	if row["query"] != "find me a restaurant" {
		t.Errorf("row query = %v", row["query"])
	}
	if row["source"] != "oracle" {
		t.Errorf("row source = %v, want oracle", row["source"])
	}
	// The MERGE key is sha256(query) — idempotent re-labeling (risk #10).
	if row["hash"] != hashQuery("find me a restaurant") {
		t.Errorf("row hash = %v, want sha256(query)", row["hash"])
	}
	gotVec, ok := row["embedding"].([]float64)
	if !ok || len(gotVec) != len(vec) {
		t.Fatalf("row embedding = %#v, want the []float64 vector nested in the row", row["embedding"])
	}
}

func TestStore_Save_IsQueryKeyedIdempotent(t *testing.T) {
	// Re-labeling the same query (even to a different tool) keys the same node, so the
	// UPSERT is idempotent at the query level (no two contradictory examples accrue).
	g := &fakeGraph{}
	s := &Store{Client: g}
	_ = s.Save(context.Background(), "same query", "web_search", []float64{1})
	h1 := s.lastHash(g)
	_ = s.Save(context.Background(), "same query", "document_search", []float64{1})
	h2 := s.lastHash(g)
	if h1 != h2 {
		t.Errorf("hash differs for same query across tools: %q vs %q (must be query-keyed)", h1, h2)
	}
}

func (s *Store) lastHash(g *fakeGraph) string {
	rows := g.lastWriteParams["rows"].([]map[string]any)
	return rows[0]["hash"].(string)
}

func TestStore_LoadExamples_ParsesAPOCJSONString(t *testing.T) {
	// The read query returns the embedding as an APOC JSON string; asFloats must parse
	// it. A blank tool or an undecodable embedding is skipped.
	emb, _ := json.Marshal([]float64{0.5, 0.25, -0.125})
	g := &fakeGraph{readRows: []map[string]any{
		{"tool": "web_search", "embedding": string(emb), "query": "meteo domani"},
		{"tool": "", "embedding": string(emb), "query": "skip: blank tool"},
		{"tool": "send_file", "embedding": "not json", "query": "skip: bad embedding"},
		{"tool": "document_search", "embedding": []any{1.0, 2.0}, "query": "raw list form"},
	}}
	s := &Store{Client: g}
	got, err := s.LoadExamples(context.Background())
	if err != nil {
		t.Fatalf("LoadExamples: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("loaded %d examples, want 2 (blank-tool + bad-embedding rows skipped)", len(got))
	}
	if got[0].Tool != "web_search" || len(got[0].Vec) != 3 || got[0].Vec[0] != 0.5 {
		t.Errorf("APOC-JSON row mis-parsed: %+v", got[0])
	}
	if got[1].Tool != "document_search" || len(got[1].Vec) != 2 {
		t.Errorf("raw-list row mis-parsed: %+v", got[1])
	}
}

func TestStore_LoadExamples_PropagatesReadError(t *testing.T) {
	g := &fakeGraph{readErr: errors.New("neo4j down")}
	s := &Store{Client: g}
	if _, err := s.LoadExamples(context.Background()); err == nil {
		t.Error("LoadExamples must propagate the Read error")
	}
}

func TestAsString_NonString(t *testing.T) {
	if asString(42) != "" {
		t.Error("asString of a non-string must be empty")
	}
	if asString(nil) != "" {
		t.Error("asString of nil must be empty")
	}
}

func TestAsFloats_AllTransportForms(t *testing.T) {
	// int64 and int elements (some transports return integral embeddings as ints).
	if got := asFloats([]any{int64(1), int(2), 3.0}); len(got) != 3 || got[0] != 1 || got[1] != 2 || got[2] != 3 {
		t.Errorf("asFloats mixed-int list = %v", got)
	}
	// A non-numeric element aborts the parse (returns nil) — never a partial vector.
	if got := asFloats([]any{1.0, "nope"}); got != nil {
		t.Errorf("asFloats with a non-numeric element must return nil, got %v", got)
	}
	// An unsupported top-level type returns nil.
	if got := asFloats(42); got != nil {
		t.Errorf("asFloats of an unsupported type must return nil, got %v", got)
	}
	// An empty APOC-JSON array decodes to a zero-length slice (not an error).
	if got := asFloats("[]"); len(got) != 0 {
		t.Errorf("asFloats of an empty JSON array = %v", got)
	}
}
