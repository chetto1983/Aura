// Write-verb guard + row→contract normalizer + GraphView.Query/Schema unit tests
// (Phase 27, GRAPH-01/03/04). Split out of graphview_test.go to keep both files
// under the 600-LOC cap (CLAUDE.md no-god-class). These reuse the FAKE GraphReader
// from graphview_test.go (same package) — no subprocess, no goroutines; the
// package's sole goleak.VerifyTestMain in client_unit_test.go covers them.
package knowledge

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

// jsonLabels is the apoc.convert.toJson(labels(n)) wire shape: a JSON-array STRING.
func jsonLabels(labels ...string) string {
	b, _ := json.Marshal(labels)
	return string(b)
}

// TestAssertReadOnly (GRAPH-04 / T-27-02): every write verb is rejected, verbs in
// string literals are ignored, writes inside CALL{} are caught, case-insensitive.
// Mutation-spot-check target: the write-verb guard.
func TestAssertReadOnly(t *testing.T) {
	cases := []struct {
		name    string
		cypher  string
		wantErr bool
	}{
		{"plain read", "MATCH (n) RETURN n", false},
		{"read with where", "MATCH (n) WHERE n.x = 1 RETURN elementId(n) AS id", false},
		{"create", "MATCH (n) CREATE (m)", true},
		{"merge", "MERGE (n:X {id:1})", true},
		{"set", "MATCH (n) SET n.x = 1", true},
		{"delete", "MATCH (n) DELETE n", true},
		{"detach delete", "MATCH (n) DETACH DELETE n", true},
		{"remove", "MATCH (n) REMOVE n.x", true},
		{"drop", "DROP INDEX foo", true},
		{"foreach", "FOREACH (x IN [1] | SET x.y = 1)", true},
		{"lowercase create", "match (n) create (m)", true},
		{"mixed case Delete", "MATCH (n) DeLeTe n", true},
		{"delete inside single-quote literal", "MATCH (n) WHERE n.note = 'please DELETE this' RETURN n", false},
		{"create inside double-quote literal", `MATCH (n) WHERE n.note = "CREATE a backup" RETURN n`, false},
		{"write inside CALL block", "CALL { CREATE (x) }", true},
		{"write merge inside CALL block", "MATCH (a) CALL { WITH a MERGE (a)-[:R]->(:B) }", true},
		{"escaped quote then verb in data", `MATCH (n) WHERE n.s = 'it\'s DELETE safe' RETURN n`, false},
		{"verb substring not flagged", "MATCH (n) WHERE n.createdAt > 0 RETURN n", false},
		{"setting substring not flagged", "MATCH (n) WHERE n.settings IS NOT NULL RETURN n", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := assertReadOnly(tc.cypher)
			if tc.wantErr && err == nil {
				t.Fatalf("assertReadOnly(%q) = nil, want error", tc.cypher)
			}
			if !tc.wantErr && err != nil {
				t.Fatalf("assertReadOnly(%q) = %v, want nil", tc.cypher, err)
			}
		})
	}
}

// TestAssertReadOnlySanitized: the rejection error never echoes the offending query
// (T-27-05 info-disclosure).
func TestAssertReadOnlySanitized(t *testing.T) {
	secret := "MATCH (n) CREATE (m {dsn:'bolt://user:pw@host'})"
	err := assertReadOnly(secret)
	if err == nil {
		t.Fatal("expected rejection")
	}
	if strings.Contains(err.Error(), "bolt://") || strings.Contains(err.Error(), "CREATE (m") {
		t.Fatalf("error leaked the offending query: %v", err)
	}
}

// TestNormalizeRows (GRAPH-01): labels decode from the toJson string, the neighbor
// node is added, the edge attaches with matching endpoints, dupes are de-duped, and
// missing/NULL columns are tolerated.
func TestNormalizeRows(t *testing.T) {
	rows := []map[string]any{
		{
			"s_id": "e1", "s_labels": jsonLabels("Entity"), "s_entity_type": "PERSON",
			"s_caption": "Alice", "s_props": map[string]any{"name": "Alice"},
			"n_id": "m1", "n_labels": jsonLabels("Message"), "n_caption": "msg",
			"n_props": map[string]any{},
			"r_id":    "r1", "r_type": "MENTIONS", "r_src": "m1", "r_dst": "e1",
		},
		{ // duplicate source node e1 (must de-dupe), new neighbor d1
			"s_id": "e1", "s_labels": jsonLabels("Entity"),
			"n_id": "d1", "n_labels": jsonLabels("Document"), "n_caption": "Doc A",
			"r_id": "r2", "r_type": "CITES", "r_src": "e1", "r_dst": "d1",
		},
		{ // a row with a NULL/missing relationship (no r_*): node only
			"s_id": "e2", "s_labels": jsonLabels("Entity"), "s_caption": "Bob",
		},
	}
	res := normalizeRows(OpSeed, rows)

	// nodes: e1, m1, d1, e2 — de-duped
	if len(res.Nodes) != 4 {
		t.Fatalf("nodes = %d, want 4 (de-duped): %+v", len(res.Nodes), res.Nodes)
	}
	byID := map[string]GraphNode{}
	for _, n := range res.Nodes {
		byID[n.ID] = n
	}
	if got := byID["e1"].Labels; len(got) != 1 || got[0] != "Entity" {
		t.Fatalf("e1 labels = %v, want [Entity]", got)
	}
	if byID["e1"].EntityType != "PERSON" {
		t.Fatalf("e1 entity_type = %q, want PERSON", byID["e1"].EntityType)
	}
	// edges attach with endpoints matching node IDs
	if len(res.Edges) != 2 {
		t.Fatalf("edges = %d, want 2", len(res.Edges))
	}
	for _, e := range res.Edges {
		if e.Source == "" || e.Target == "" {
			t.Fatalf("edge has empty endpoint: %+v", e)
		}
		if _, ok := byID[e.Source]; !ok {
			t.Fatalf("edge source %q is not a node", e.Source)
		}
		if _, ok := byID[e.Target]; !ok {
			t.Fatalf("edge target %q is not a node", e.Target)
		}
	}
}

// TestNormalizeCitations (GRAPH-03 / SC3 / D-09): a node adjacent to a
// :Document/:Source neighbor gets a NON-EMPTY Citations list naming that neighbor;
// a node with no such neighbor gets an empty list — derived from the fetched edges,
// no extra Cypher. Mutation-spot-check target.
func TestNormalizeCitations(t *testing.T) {
	rows := []map[string]any{
		{ // Alice cites Doc A
			"s_id": "e1", "s_labels": jsonLabels("Entity"), "s_caption": "Alice",
			"n_id": "d1", "n_labels": jsonLabels("Document"), "n_caption": "Doc A",
			"r_id": "r1", "r_type": "CITES", "r_src": "e1", "r_dst": "d1",
		},
		{ // Bob connects only to another Entity (no evidence neighbor)
			"s_id": "e2", "s_labels": jsonLabels("Entity"), "s_caption": "Bob",
			"n_id": "e3", "n_labels": jsonLabels("Entity"), "n_caption": "Carol",
			"r_id": "r2", "r_type": "KNOWS", "r_src": "e2", "r_dst": "e3",
		},
		{ // Carol connects to a :Source node
			"s_id": "e3", "s_labels": jsonLabels("Entity"), "s_caption": "Carol",
			"n_id": "s1", "n_labels": jsonLabels("Source"), "n_caption": "Src 1",
			"r_id": "r3", "r_type": "FROM", "r_src": "e3", "r_dst": "s1",
		},
	}
	res := normalizeRows(OpSeed, rows)
	byID := map[string]GraphNode{}
	for _, n := range res.Nodes {
		byID[n.ID] = n
	}
	// Alice → cites Doc A
	if cs := byID["e1"].Citations; len(cs) != 1 || cs[0] != "Doc A" {
		t.Fatalf("Alice citations = %v, want [Doc A]", cs)
	}
	// Carol → cites Src 1
	if cs := byID["e3"].Citations; len(cs) != 1 || cs[0] != "Src 1" {
		t.Fatalf("Carol citations = %v, want [Src 1]", cs)
	}
	// Bob → no evidence neighbor → empty
	if cs := byID["e2"].Citations; len(cs) != 0 {
		t.Fatalf("Bob citations = %v, want empty", cs)
	}
	// the :Document node itself has no Document/Source neighbor → empty
	if cs := byID["d1"].Citations; len(cs) != 0 {
		t.Fatalf("Doc A citations = %v, want empty", cs)
	}
}

// TestNormalizeSchema: list fields decode from toJson strings into sorted []string.
func TestNormalizeSchema(t *testing.T) {
	rows := []map[string]any{{
		"labels_json":       jsonLabels("Entity", "Document", "Conversation"),
		"rel_types_json":    jsonLabels("MENTIONS", "CITES"),
		"prop_keys_json":    jsonLabels("name", "type"),
		"entity_types_json": jsonLabels("PERSON", "ORGANIZATION"),
	}}
	sch := normalizeSchema(rows)
	if len(sch.Labels) != 3 || sch.Labels[0] != "Conversation" {
		t.Fatalf("labels = %v, want sorted 3", sch.Labels)
	}
	if len(sch.RelTypes) != 2 || sch.RelTypes[0] != "CITES" {
		t.Fatalf("rel_types = %v, want sorted [CITES MENTIONS]", sch.RelTypes)
	}
	if len(sch.EntityTypes) != 2 {
		t.Fatalf("entity_types = %v, want 2", sch.EntityTypes)
	}
	// empty rows → non-nil empty slices (never null on the wire)
	empty := normalizeSchema(nil)
	if empty.Labels == nil || empty.RelTypes == nil {
		t.Fatalf("empty schema must have non-nil Labels/RelTypes: %+v", empty)
	}
}

// TestQueryDispatch: each op runs the compile→guard→read→normalize path and sets
// GraphResult.Query to the compiled Cypher.
func TestQueryDispatch(t *testing.T) {
	seedRows := []map[string]any{{
		"s_id": "e1", "s_labels": jsonLabels("Entity"), "s_caption": "Alice",
		"n_id": "d1", "n_labels": jsonLabels("Document"), "n_caption": "Doc A",
		"r_id": "r1", "r_type": "CITES", "r_src": "e1", "r_dst": "d1",
	}}
	t.Run("seed", func(t *testing.T) {
		fr := &fakeReader{rows: seedRows}
		gv := NewGraphView(fr)
		res, err := gv.Query(context.Background(), GraphIntent{Op: OpSeed, Session: "thr-1"})
		if err != nil {
			t.Fatalf("Query seed: %v", err)
		}
		if len(res.Nodes) != 2 {
			t.Fatalf("seed nodes = %d, want 2", len(res.Nodes))
		}
		if !strings.Contains(res.Query, "$session") {
			t.Fatalf("Query field must be the compiled Cypher, got %q", res.Query)
		}
	})
	t.Run("expand", func(t *testing.T) {
		fr := &fakeReader{rows: seedRows}
		gv := NewGraphView(fr)
		res, err := gv.Query(context.Background(), GraphIntent{Op: OpExpand, SeedID: "e1"})
		if err != nil {
			t.Fatalf("Query expand: %v", err)
		}
		if !strings.Contains(res.Query, "$seed") {
			t.Fatalf("expand Query must be the compiled Cypher: %q", res.Query)
		}
	})
	t.Run("unknown op", func(t *testing.T) {
		gv := NewGraphView(&fakeReader{})
		if _, err := gv.Query(context.Background(), GraphIntent{Op: "bogus"}); err == nil {
			t.Fatal("unknown op must error")
		}
	})
}

// TestQuerySeedFallback (D-07/D-08): an empty seed footprint falls back to the
// schema overview instead of a blank canvas.
func TestQuerySeedFallback(t *testing.T) {
	// First Read (seed) returns no rows; the fallback Read (schema) returns the
	// schema row. The scriptedReader drives the two phases deterministically.
	fr := &scriptedReader{responses: [][]map[string]any{
		{}, // seed: empty footprint
		{{ // schema overview
			"labels_json":    jsonLabels("Entity", "Document"),
			"rel_types_json": jsonLabels("CITES"),
		}},
	}}
	gv := NewGraphView(fr)
	res, err := gv.Query(context.Background(), GraphIntent{Op: OpSeed, Session: "empty-thread"})
	if err != nil {
		t.Fatalf("Query seed fallback: %v", err)
	}
	if len(res.Nodes) != 0 {
		t.Fatalf("fallback must have no nodes, got %d", len(res.Nodes))
	}
	if len(res.Schema.Labels) != 2 {
		t.Fatalf("fallback must carry the schema overview, got %+v", res.Schema)
	}
	if fr.calls != 2 {
		t.Fatalf("expected 2 reads (seed + schema fallback), got %d", fr.calls)
	}
}

// TestSchema: GraphView.Schema runs the overview and returns the label set.
func TestSchema(t *testing.T) {
	fr := &fakeReader{rows: []map[string]any{{
		"labels_json":    jsonLabels("Entity", "Document", "Conversation"),
		"rel_types_json": jsonLabels("MENTIONS"),
	}}}
	gv := NewGraphView(fr)
	sch, err := gv.Schema(context.Background())
	if err != nil {
		t.Fatalf("Schema: %v", err)
	}
	if len(sch.Labels) != 3 {
		t.Fatalf("schema labels = %v, want 3", sch.Labels)
	}
}

// TestQueryReadError: a reader error surfaces from Query (not swallowed).
func TestQueryReadError(t *testing.T) {
	gv := NewGraphView(&fakeReader{err: errors.New("read failed")})
	if _, err := gv.Query(context.Background(), GraphIntent{Op: OpExpand, SeedID: "e1"}); err == nil {
		t.Fatal("expected read error to surface")
	}
}

// TestQueryCapClamp: a crafted over-cap intent is clamped server-side before
// binding (GRAPH-04). The reader records the bound param map.
func TestQueryCapClamp(t *testing.T) {
	fr := &fakeReader{rows: []map[string]any{{"s_id": "e1", "s_labels": jsonLabels("Entity")}}}
	gv := NewGraphView(fr)
	_, err := gv.Query(context.Background(), GraphIntent{Op: OpExpand, SeedID: "e1", EdgeCap: 100000})
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if fr.lastParam["edge_cap"] != maxEdgeCap {
		t.Fatalf("edge_cap bound = %v, want clamped %d", fr.lastParam["edge_cap"], maxEdgeCap)
	}
}

// TestDecodeStrings covers every branch of the list decoder: the toJson STRING
// shape, an empty/NULL column, a malformed JSON string (→ nil), a native []any
// (future-driver robustness), and an unexpected type.
func TestDecodeStrings(t *testing.T) {
	cases := []struct {
		name string
		in   any
		want []string
	}{
		{"toJson string", jsonLabels("A", "B"), []string{"A", "B"}},
		{"empty string", "", nil},
		{"literal null string", "null", nil},
		{"malformed json", "[not json", nil},
		{"native []any", []any{"X", "Y", 3}, []string{"X", "Y"}},
		{"unexpected type", 42, nil},
		{"nil", nil, nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := decodeStrings(tc.in)
			if len(got) != len(tc.want) {
				t.Fatalf("decodeStrings(%#v) = %v, want %v", tc.in, got, tc.want)
			}
			for i := range got {
				if got[i] != tc.want[i] {
					t.Fatalf("decodeStrings(%#v)[%d] = %q, want %q", tc.in, i, got[i], tc.want[i])
				}
			}
		})
	}
}

// TestSchemaReadError: a reader error surfaces from GraphView.Schema (the schema
// path's error branch).
func TestSchemaReadError(t *testing.T) {
	gv := NewGraphView(&fakeReader{err: errors.New("schema read failed")})
	if _, err := gv.Schema(context.Background()); err == nil {
		t.Fatal("expected schema read error to surface")
	}
}

// TestQuerySchemaOverviewOp: the explicit schema_overview op returns the schema as a
// GraphResult with the compiled query attached, and a reader error on that op
// surfaces.
func TestQuerySchemaOverviewOp(t *testing.T) {
	fr := &fakeReader{rows: []map[string]any{{
		"labels_json": jsonLabels("Entity", "Document"),
	}}}
	gv := NewGraphView(fr)
	res, err := gv.Query(context.Background(), GraphIntent{Op: OpSchemaOverview})
	if err != nil {
		t.Fatalf("schema_overview Query: %v", err)
	}
	if len(res.Schema.Labels) != 2 {
		t.Fatalf("schema_overview labels = %v, want 2", res.Schema.Labels)
	}
	if res.Query == "" {
		t.Fatal("schema_overview must attach the compiled query")
	}

	gvErr := NewGraphView(&fakeReader{err: errors.New("boom")})
	if _, err := gvErr.Query(context.Background(), GraphIntent{Op: OpSchemaOverview}); err == nil {
		t.Fatal("schema_overview read error must surface")
	}
}

// scriptedReader returns a different canned response per call, so a multi-phase
// path (seed → schema fallback) can be exercised deterministically.
type scriptedReader struct {
	responses [][]map[string]any
	calls     int
}

func (s *scriptedReader) Read(_ context.Context, _ string, _ map[string]any) ([]map[string]any, error) {
	i := s.calls
	s.calls++
	if i < len(s.responses) {
		return s.responses[i], nil
	}
	return nil, nil
}
