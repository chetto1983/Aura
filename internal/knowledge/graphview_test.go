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
	"reflect"
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

func withGraphIntentUserID(t *testing.T, in GraphIntent, userID string) GraphIntent {
	t.Helper()
	v := reflect.ValueOf(&in).Elem()
	f := v.FieldByName("UserID")
	if !f.IsValid() {
		t.Fatalf("GraphIntent is missing UserID for single-database user scoping")
	}
	if f.Kind() != reflect.String {
		t.Fatalf("GraphIntent.UserID must be a string, got %s", f.Kind())
	}
	f.SetString(userID)
	return in
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

func TestCompileSeedScopesByUserID(t *testing.T) {
	in := withGraphIntentUserID(t, GraphIntent{Op: OpSeed, Session: "thr-1"}, "identity-1")
	cypher, params := compileSeed(in)

	if params["user_id"] != "identity-1" {
		t.Fatalf("params[user_id] = %v, want identity-1", params["user_id"])
	}
	noLiteral(t, cypher, "identity-1")
	for _, frag := range []string{
		"$user_id = ''",
		"c.user_identifier = $user_id",
		":User {identifier:$user_id}",
		"[:HAS_CONVERSATION]->(c)",
	} {
		if !strings.Contains(cypher, frag) {
			t.Fatalf("seed query missing user-scope fragment %q:\n%s", frag, cypher)
		}
	}
}

// bannedScopeConstructs are the two constructs the ownership prelude replaced. Both
// rendered the cockpit graph empty in production: `single_tenant` gated every row on "no
// other :User exists" (false from the moment a second identity appears), and the
// `owned_entities` roll-up traversed a MENTIONS edge no writer produces, so the list was
// always empty. Neither may return to a scoped query.
var bannedScopeConstructs = []string{"single_tenant", "owned_entities", "MENTIONS"}

// assertScopeContract pins what EVERY scoped query must do: bind the identity as a param
// (never a literal), resolve ownership through the shared prelude, keep the banned
// constructs out, and BALANCE its delimiters. The balance check is the cheap unit-level
// stand-in for a parser: compileExpand shipped with an unbalanced `})` that left one paren
// open, and because every assertion here was a strings.Contains on a fragment, the suite
// stayed green while Neo4j rejected the statement outright on every click-to-expand.
func assertScopeContract(t *testing.T, cypher string, params map[string]any) {
	t.Helper()
	if params["user_id"] != "identity-1" {
		t.Fatalf("params[user_id] = %v, want identity-1", params["user_id"])
	}
	noLiteral(t, cypher, "identity-1")
	for _, frag := range []string{
		"$user_id = ''",
		":User {identifier:$user_id}",
		"AS owned_ids",
	} {
		if !strings.Contains(cypher, frag) {
			t.Fatalf("scoped query missing ownership fragment %q:\n%s", frag, cypher)
		}
	}
	for _, banned := range bannedScopeConstructs {
		if strings.Contains(cypher, banned) {
			t.Fatalf("scoped query resurrected the banned construct %q:\n%s", banned, cypher)
		}
	}
	assertBalancedDelimiters(t, cypher)
}

// assertBalancedDelimiters fails when (), [] or {} do not balance outside string literals.
// It reuses the guard's stripStringLiterals so a brace inside property data cannot skew
// the count.
func assertBalancedDelimiters(t *testing.T, cypher string) {
	t.Helper()
	pairs := map[byte]byte{')': '(', ']': '[', '}': '{'}
	var stack []byte
	for i, c := range []byte(stripStringLiterals(cypher)) {
		switch c {
		case '(', '[', '{':
			stack = append(stack, c)
		case ')', ']', '}':
			if len(stack) == 0 || stack[len(stack)-1] != pairs[c] {
				t.Fatalf("unbalanced %q at offset %d — the query would not parse:\n%s", c, i, cypher)
			}
			stack = stack[:len(stack)-1]
		}
	}
	if len(stack) != 0 {
		t.Fatalf("%d delimiter(s) left open — the query would not parse:\n%s", len(stack), cypher)
	}
}

func TestCompileOverviewScopesByUserID(t *testing.T) {
	in := withGraphIntentUserID(t, GraphIntent{Op: OpSchemaOverview}, "identity-1")
	cypher, params := compileOverview(in)

	assertScopeContract(t, cypher, params)
	// Ownership is membership in one id set and BOTH endpoints must belong to it, so a
	// node owned by another identity can never be drawn.
	if !strings.Contains(cypher, "elementId(s) IN owned_ids AND elementId(n) IN owned_ids") {
		t.Fatalf("overview must require BOTH endpoints to be owned:\n%s", cypher)
	}
}

func TestCompileExpandScopesByUserID(t *testing.T) {
	in := withGraphIntentUserID(t, GraphIntent{Op: OpExpand, SeedID: "node-1"}, "identity-1")
	cypher, params := compileExpand(in)

	assertScopeContract(t, cypher, params)
	// A node the caller can SEE is exactly a node the caller can EXPAND, and a neighbor
	// they do not own is never revealed — both ends gate on the same ownership set.
	for _, frag := range []string{
		"unscoped OR elementId(s) IN owned_ids",
		"unscoped OR elementId(n) IN owned_ids",
	} {
		if !strings.Contains(cypher, frag) {
			t.Fatalf("expand query missing ownership gate %q:\n%s", frag, cypher)
		}
	}
}

func TestGraphIntentQueriesHaveNoLegacyLearningLabelExceptions(t *testing.T) {
	overview, _ := compileOverview(GraphIntent{Op: OpSchemaOverview})
	expand, _ := compileExpand(GraphIntent{Op: OpExpand, SeedID: "node-1"})
	for _, query := range []string{overview, expand} {
		for _, label := range []string{"ReasoningExample", "ReasoningSeed", "ToolSelectionExample", "ToolSelectionSeed"} {
			if strings.Contains(query, label) {
				t.Errorf("query retains legacy label exception %s:\n%s", label, query)
			}
		}
	}
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
		"(c:Conversation {session_id:$session})",
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
	for line := range strings.SplitSeq(cypher, "\n") {
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
