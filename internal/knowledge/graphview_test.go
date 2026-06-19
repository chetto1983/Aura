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
