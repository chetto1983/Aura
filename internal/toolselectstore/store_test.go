package toolselectstore

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/chetto1983/aura/internal/neostore"
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
	lastReadQuery   string
	lastReadParams  map[string]any
	writeRows       []map[string]any
}

func (f *fakeGraph) Read(_ context.Context, query string, params map[string]any) ([]map[string]any, error) {
	f.lastReadQuery, f.lastReadParams = query, params
	return f.readRows, f.readErr
}

func (f *fakeGraph) Write(_ context.Context, query string, params map[string]any) ([]map[string]any, error) {
	f.lastWriteQuery = query
	f.lastWriteParams = params
	if f.writeRows != nil {
		return f.writeRows, nil
	}
	return []map[string]any{{"status": "created"}}, nil
}

func TestStore_LoadExamples_UsesBoundedCypher(t *testing.T) {
	g := &fakeGraph{}
	s := &Store{Client: g, Now: func() time.Time { return time.Unix(1_800_000_000, 0) }}
	if _, err := s.LoadExamples(context.Background()); err != nil {
		t.Fatal(err)
	}
	for _, fragment := range []string{"source = 'learned'", "updated_at >= datetime($cutoff)", "ORDER BY e.updated_at DESC, e.hash ASC", "LIMIT $bucket_limit", "LIMIT $store_limit"} {
		if !strings.Contains(g.lastReadQuery, fragment) {
			t.Errorf("bounded load query missing %q", fragment)
		}
	}
}

func TestStore_SaveLearnedReportsCapacity(t *testing.T) {
	g := &fakeGraph{writeRows: []map[string]any{{"status": "at_capacity"}}}
	s := &Store{Client: g}
	result, err := s.SaveLearned(context.Background(), "query", "web_search", []float64{1}, 0.8, 0.6)
	if err != nil {
		t.Fatal(err)
	}
	if result != SaveAtCapacity {
		t.Fatalf("result = %q", result)
	}
	if !strings.Contains(g.lastWriteQuery, "bucket_count < row.bucket_cap") || !strings.Contains(g.lastWriteQuery, "store_count < row.store_cap") {
		t.Fatalf("write does not enforce both caps: %s", g.lastWriteQuery)
	}
}

func TestStore_SaveAtCapacityDoesNotRetry(t *testing.T) {
	g := &fakeGraph{writeRows: []map[string]any{{"status": "at_capacity"}}}
	s := &Store{Client: g}
	if err := s.Save(context.Background(), "query", "web_search", []float64{1}); err != nil {
		t.Fatalf("capacity should be a committed drop, got retry error: %v", err)
	}
}

func TestStore_PinnedSeedsAreSeparateAndBounded(t *testing.T) {
	g := &fakeGraph{}
	s := &Store{Client: g}
	if err := s.SavePinned(context.Background(), "seed", "web_search", []float64{1}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(g.lastWriteQuery, ":ToolSelectionSeed") || strings.Contains(g.lastWriteQuery, "bucket_count") {
		t.Fatalf("pinned save is not isolated: %s", g.lastWriteQuery)
	}
	if _, err := s.LoadPinnedExamples(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(g.lastReadQuery, ":ToolSelectionSeed") || !strings.Contains(g.lastReadQuery, "LIMIT $limit") {
		t.Fatalf("pinned load is not bounded: %s", g.lastReadQuery)
	}
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
	if row["source"] != "learned" {
		t.Errorf("row source = %v, want learned", row["source"])
	}
	// The MERGE key is sha256(query) — idempotent re-labeling (risk #10).
	if row["hash"] != neostore.HashText("find me a restaurant") {
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

// WR-04: Save must reject an empty tool/embedding at the store boundary rather than
// persist an un-loadable dead row (LoadExamples skips len(vec)==0) that still poisons
// the caller's seen-set. The error keeps the write retryable, and no Write is issued.
func TestStore_Save_RejectsEmptyToolOrEmbedding(t *testing.T) {
	cases := []struct {
		name string
		tool string
		vec  []float64
	}{
		{"empty embedding", "web_search", nil},
		{"zero-length embedding", "web_search", []float64{}},
		{"empty tool", "", []float64{0.1, 0.2}},
		{"both empty", "", nil},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			g := &fakeGraph{}
			s := &Store{Client: g}
			if err := s.Save(context.Background(), "some query", c.tool, c.vec); err == nil {
				t.Errorf("Save(%q, vec_len=%d) must error (refuse the empty write)", c.tool, len(c.vec))
			}
			if g.lastWriteParams != nil {
				t.Errorf("a refused Save must not issue a Write; params=%v", g.lastWriteParams)
			}
		})
	}
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

// AsString/AsFloats helper-level coverage moved with the code to its canonical home
// (internal/neostore/neostore_test.go) when QUAL-03 extracted the byte-identical copies;
// the store tests above exercise them through the real LoadExamples/Save paths.
