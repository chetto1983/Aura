// Unit tests for the read-only graph normalizer (Phase 27, GRAPH-01/03/04).
// These inject a FAKE GraphReader returning canned []map[string]any rows — no
// subprocess, no goroutines — so they add no goleak of their own and do NOT need
// to: the package's sole untagged goleak.VerifyTestMain lives in
// client_unit_test.go and runs for every `go test ./internal/knowledge/...`
// invocation, catching any accidental goroutine package-wide. Do NOT add a second
// TestMain here — it would be a duplicate-symbol link error.
package knowledge

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

// fakeReader is the mockable GraphReader seam: it returns canned rows (or an
// error) and records the last query+params it was handed, so tests can assert the
// compiled Cypher + the param map without a live Neo4j.
type fakeReader struct {
	rows      []map[string]any
	err       error
	lastQuery string
	lastParam map[string]any
	calls     int
}

func (f *fakeReader) Read(_ context.Context, query string, params map[string]any) ([]map[string]any, error) {
	f.calls++
	f.lastQuery = query
	f.lastParam = params
	if f.err != nil {
		return nil, f.err
	}
	return f.rows, nil
}

// TestClamp pins the cap clamp: below-default floors to default, above-max caps to
// max, in-range passes through.
func TestClamp(t *testing.T) {
	cases := []struct {
		name        string
		v, def, max int
		want        int
	}{
		{"zero floors to default", 0, 75, 300, 75},
		{"negative floors to default", -5, 75, 300, 75},
		{"above max caps to max", 500, 75, 300, 300},
		{"in range passes through", 50, 75, 300, 50},
		{"exactly default", 75, 75, 300, 75},
		{"exactly max", 300, 75, 300, 300},
		{"edge cap zero floors to default", 0, defaultEdgeCap, maxEdgeCap, defaultEdgeCap},
		{"edge cap over max caps", 9999, defaultEdgeCap, maxEdgeCap, maxEdgeCap},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := clamp(tc.v, tc.def, tc.max); got != tc.want {
				t.Fatalf("clamp(%d,%d,%d) = %d, want %d", tc.v, tc.def, tc.max, got, tc.want)
			}
		})
	}
}

// noLiteral asserts the compiled Cypher body contains NONE of the bound scalar
// values as literals — the no-interpolation invariant (T-27-01).
func noLiteral(t *testing.T, cypher string, literals ...string) {
	t.Helper()
	for _, lit := range literals {
		if lit == "" {
			continue
		}
		if strings.Contains(cypher, lit) {
			t.Fatalf("compiled Cypher leaks bound value %q as a literal:\n%s", lit, cypher)
		}
	}
}

// TestCompileExpand: values ride the param map (never interpolated), edge cap is
// clamped to the hard max, label/rel-type filters bind as data.
func TestCompileExpand(t *testing.T) {
	in := GraphIntent{Op: "expand", SeedID: "x", EdgeCap: 9999, Labels: []string{"Entity"}, RelTypes: []string{"MENTIONS"}}
	cypher, params := compileExpand(in)

	if params["seed"] != "x" {
		t.Fatalf("params[seed] = %v, want x", params["seed"])
	}
	if params["edge_cap"] != maxEdgeCap {
		t.Fatalf("params[edge_cap] = %v, want %d (clamped)", params["edge_cap"], maxEdgeCap)
	}
	// the body must NOT contain the seed id "x" nor the raw cap "9999"
	noLiteral(t, cypher, "9999")
	if strings.Contains(cypher, "= 'x'") || strings.Contains(cypher, "\"x\"") {
		t.Fatalf("seed id leaked as a literal:\n%s", cypher)
	}
	if !strings.Contains(cypher, "elementId(s) = $seed") {
		t.Fatalf("expand must anchor on elementId(s) = $seed:\n%s", cypher)
	}
	// labels/rel-types bound as data, never as Cypher identifiers
	if !strings.Contains(cypher, "l IN $labels") {
		t.Fatalf("labels must bind as `l IN $labels`:\n%s", cypher)
	}
	if !strings.Contains(cypher, "type(r) IN $rel_types") {
		t.Fatalf("rel-types must bind as `type(r) IN $rel_types`:\n%s", cypher)
	}
	// labels filter must be a NON-nil slice so `$labels = []` works in Cypher
	if _, ok := params["labels"].([]string); !ok {
		t.Fatalf("params[labels] must be a []string, got %T", params["labels"])
	}
	// explicit-field projection — never RETURN n / bare labels()
	assertExplicitProjection(t, cypher)
}

// TestCompileSeed: session binds, node cap defaults, the footprint path matches.
func TestCompileSeed(t *testing.T) {
	cypher, params := compileSeed(GraphIntent{Op: "seed", Session: "thr-1"})

	if params["session"] != "thr-1" {
		t.Fatalf("params[session] = %v, want thr-1", params["session"])
	}
	if params["node_cap"] != defaultNodeCap {
		t.Fatalf("params[node_cap] = %v, want %d (default)", params["node_cap"], defaultNodeCap)
	}
	noLiteral(t, cypher, "thr-1")
	// the conversation footprint path (D-07). The Entity is captured as a bound
	// variable `(e:Entity)` because it is projected downstream, so assert on the
	// label fragment `:Entity)` which matches the bound form.
	for _, frag := range []string{
		"(:Conversation {session_id:$session})",
		"[:HAS_MESSAGE]",
		"(:Message)",
		"[:MENTIONS]",
		":Entity)",
	} {
		if !strings.Contains(cypher, frag) {
			t.Fatalf("seed footprint missing %q:\n%s", frag, cypher)
		}
	}
	assertExplicitProjection(t, cypher)
}

// TestCompileSchema: a labels/rel-types/property-keys query whose lists survive the
// read tool (apoc.convert.toJson or apoc.meta).
func TestCompileSchema(t *testing.T) {
	cypher, params := compileSchema()
	if cypher == "" {
		t.Fatal("compileSchema returned an empty query")
	}
	low := strings.ToLower(cypher)
	if !strings.Contains(low, "label") {
		t.Fatalf("schema query must introspect labels:\n%s", cypher)
	}
	// lists must be wrapped to survive the read tool (toJson) OR use apoc.meta
	if !strings.Contains(low, "apoc.convert.tojson") && !strings.Contains(low, "apoc.meta") {
		t.Fatalf("schema lists must be wrapped (apoc.convert.toJson / apoc.meta):\n%s", cypher)
	}
	// no write verbs in the schema query
	if err := assertReadOnly(cypher); err != nil {
		t.Fatalf("compileSchema emitted a non-read query: %v", err)
	}
	_ = params
}

// assertExplicitProjection enforces the Pattern-1 anti-`RETURN n` rule: every
// compiled read projects explicit scalar fields + toJson for label lists, never a
// bare node or a bare labels() list (which lose data through mcp serialization).
func assertExplicitProjection(t *testing.T, cypher string) {
	t.Helper()
	if !strings.Contains(cypher, "apoc.convert.toJson(labels(") {
		t.Fatalf("labels must be projected via apoc.convert.toJson(labels(...)):\n%s", cypher)
	}
	if !strings.Contains(cypher, "elementId(") {
		t.Fatalf("nodes/edges must be projected via elementId(...):\n%s", cypher)
	}
	// crude RETURN-n guard: a RETURN line that returns a bare single identifier
	for _, line := range strings.Split(cypher, "\n") {
		trimmed := strings.TrimSpace(line)
		up := strings.ToUpper(trimmed)
		if strings.HasPrefix(up, "RETURN N") && !strings.Contains(trimmed, "(") && !strings.Contains(trimmed, ".") {
			t.Fatalf("forbidden bare `RETURN n` shape:\n%s", cypher)
		}
	}
}

// TestContractRoundTrip: the flat contract structs decode(encode) as identity
// (omitempty discipline, mirror display.Payload).
func TestContractRoundTrip(t *testing.T) {
	want := GraphResult{
		Nodes: []GraphNode{{
			ID: "n1", Caption: "Alice", Labels: []string{"Entity"}, EntityType: "PERSON",
			Degree: 3, Props: map[string]any{"name": "Alice"}, RefID: "ref-1",
			Citations: []string{"doc-1"},
		}},
		Edges:  []GraphEdge{{ID: "e1", Source: "n1", Target: "doc-1", RelType: "CITES"}},
		Paths:  []GraphPath{{Steps: []GraphEdge{{ID: "e1", Source: "n1", Target: "doc-1", RelType: "CITES"}}}},
		Schema: GraphSchema{Labels: []string{"Entity", "Document"}, RelTypes: []string{"CITES"}},
		Query:  "MATCH (n) RETURN n",
	}
	b, err := json.Marshal(want)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var got GraphResult
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	b2, err := json.Marshal(got)
	if err != nil {
		t.Fatalf("re-marshal: %v", err)
	}
	if string(b) != string(b2) {
		t.Fatalf("decode(encode) not identity:\n a=%s\n b=%s", b, b2)
	}
}

// TestContractOmitEmpty: an empty GraphNode omits its optional fields on the wire,
// and an empty Citations list does not serialize (omitempty).
func TestContractOmitEmpty(t *testing.T) {
	n := GraphNode{ID: "n1"}
	b, err := json.Marshal(n)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	s := string(b)
	for _, frag := range []string{"caption", "labels", "entity_type", "degree", "props", "ref_id", "citations"} {
		if strings.Contains(s, frag) {
			t.Fatalf("empty GraphNode leaked %q on the wire: %s", frag, s)
		}
	}
	if !strings.Contains(s, `"id":"n1"`) {
		t.Fatalf("id must always serialize: %s", s)
	}
}

// TestReaderErrorPropagates: a GraphReader error surfaces through a Read call (sanity
// for the seam wiring used by later Query tests).
func TestReaderErrorPropagates(t *testing.T) {
	sentinel := errors.New("boom")
	fr := &fakeReader{err: sentinel}
	_, err := fr.Read(context.Background(), "MATCH (n) RETURN elementId(n) AS id", nil)
	if !errors.Is(err, sentinel) {
		t.Fatalf("err = %v, want %v", err, sentinel)
	}
}

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
	res := normalizeRows(opSeed, rows)

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
	res := normalizeRows(opSeed, rows)
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
		res, err := gv.Query(context.Background(), GraphIntent{Op: opSeed, Session: "thr-1"})
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
		res, err := gv.Query(context.Background(), GraphIntent{Op: opExpand, SeedID: "e1"})
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
	// schema row. The fakeReader returns the same canned rows for every call, so we
	// drive the two phases with a scripted reader.
	fr := &scriptedReader{responses: [][]map[string]any{
		{}, // seed: empty footprint
		{{ // schema overview
			"labels_json":    jsonLabels("Entity", "Document"),
			"rel_types_json": jsonLabels("CITES"),
		}},
	}}
	gv := NewGraphView(fr)
	res, err := gv.Query(context.Background(), GraphIntent{Op: opSeed, Session: "empty-thread"})
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
	if _, err := gv.Query(context.Background(), GraphIntent{Op: opExpand, SeedID: "e1"}); err == nil {
		t.Fatal("expected read error to surface")
	}
}

// TestQueryCapClamp: a crafted over-cap intent is clamped server-side before
// binding (GRAPH-04). The reader records the bound param map.
func TestQueryCapClamp(t *testing.T) {
	fr := &fakeReader{rows: []map[string]any{{"s_id": "e1", "s_labels": jsonLabels("Entity")}}}
	gv := NewGraphView(fr)
	_, err := gv.Query(context.Background(), GraphIntent{Op: opExpand, SeedID: "e1", EdgeCap: 100000})
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if fr.lastParam["edge_cap"] != maxEdgeCap {
		t.Fatalf("edge_cap bound = %v, want clamped %d", fr.lastParam["edge_cap"], maxEdgeCap)
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
