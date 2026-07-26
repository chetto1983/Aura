//go:build neo4j_integration && db_integration

// Live-stack Wave-0 gate for the graph-explorer normalizer (Phase 27, GRAPH-01).
// It pins the EXACT mcp-neo4j-cypher serialization shape the normalizer depends on
// (resolves Assumption A1) and probes the active-conversation footprint vs. the
// schema-overview fallback (resolves A3). Run via:
//
//	make neo4j-migrate && go test -race -tags 'db_integration neo4j_integration' ./internal/knowledge/ -run TestGraphViewLive
//
// No-skip-as-green: testConfig/envOrSkipCI t.Fatal under $CI when their env is
// unset; a skipped probe fails the gate, never passes it.
//
// Goroutine-leak detection is INHERITED: the package's sole TestMain
// (goleak.VerifyTestMain) lives in client_unit_test.go (untagged, so compiled into
// this build too). Opening a real knowledge.Client here spawns the
// mcp-neo4j-cypher subprocess + reader goroutines; the inherited goleak already
// fails the build on a goroutine leaked past Close. Do NOT add a second TestMain
// here — it would be a duplicate-symbol link error.
package knowledge

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"
)

func seedGraphViewLiveFixture(ctx context.Context, t *testing.T, mcp *Client) (session string, cleanup func()) {
	t.Helper()

	runID := fmt.Sprintf("graphview-live-%d", time.Now().UnixNano())
	session = runID + "-session"
	params := map[string]any{
		"run":     runID,
		"session": session,
		"message": runID + "-message",
		"entity":  runID + "-entity",
		"doc":     runID + "-document",
	}
	const seed = `CREATE (c:Conversation {session_id:$session, test_run_id:$run})
CREATE (m:Message {id:$message, test_run_id:$run})
CREATE (e:Entity {id:$entity, name:'Graph Fixture Entity', canonical_name:'Graph Fixture Entity', type:'PERSON', test_run_id:$run})
CREATE (d:Document {id:$doc, title:'Graph Fixture Source', url:'https://example.test/graph-fixture', test_run_id:$run})
CREATE (c)-[:HAS_MESSAGE {test_run_id:$run}]->(m)
CREATE (m)-[:MENTIONS {test_run_id:$run}]->(e)
CREATE (e)-[:CITES {test_run_id:$run}]->(d)
RETURN c.session_id AS session`
	if _, err := mcp.Write(ctx, seed, params); err != nil {
		t.Fatalf("seed graphview live fixture: %v", err)
	}

	return session, func() {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if _, err := mcp.Write(cleanupCtx, `MATCH (n {test_run_id:$run}) DETACH DELETE n`, map[string]any{"run": runID}); err != nil {
			t.Logf("cleanup graphview live fixture %q: %v", runID, err)
		}
	}
}

// TestGraphViewLive_SerializationShape pins the Pattern-1 explicit-field projection
// against the live mcp boundary (A1): id is a non-empty string, the labels column
// (apoc.convert.toJson) unmarshals to a non-empty []string, and properties() is a
// map. This proves labels + element-ids survive `result.data()` + _value_sanitize.
func TestGraphViewLive_SerializationShape(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	mcp := openTestMCP(ctx, t)
	defer mcp.Close()
	_, cleanup := seedGraphViewLiveFixture(ctx, t, mcp)
	defer cleanup()

	// Any existing node. If the graph is empty this skips locally (and the schema
	// probe below still asserts the live label set) — but on a seeded stack it pins
	// the shape the normalizer decodes.
	const probe = `MATCH (n)
WITH n LIMIT 1
RETURN elementId(n) AS id, apoc.convert.toJson(labels(n)) AS labels_json, properties(n) AS props`
	rows, err := mcp.Read(ctx, probe, nil)
	if err != nil {
		t.Fatalf("serialization probe read: %v", err)
	}
	if len(rows) == 0 {
		t.Log("serialization probe: live graph has no nodes — shape assertion skipped (schema probe still runs)")
		return
	}
	row := rows[0]

	id, ok := row["id"].(string)
	if !ok || id == "" {
		t.Fatalf("id column: want non-empty string, got %#v", row["id"])
	}
	labelsJSON, ok := row["labels_json"].(string)
	if !ok || labelsJSON == "" {
		t.Fatalf("labels_json column: want a JSON string (apoc.convert.toJson), got %#v", row["labels_json"])
	}
	var labels []string
	if err := json.Unmarshal([]byte(labelsJSON), &labels); err != nil {
		t.Fatalf("labels_json does not unmarshal to []string: %v (raw=%q)", err, labelsJSON)
	}
	if len(labels) == 0 {
		t.Fatalf("decoded labels are empty for node %s (raw=%q)", id, labelsJSON)
	}
	if _, ok := row["props"].(map[string]any); !ok {
		t.Fatalf("props column: want map[string]any, got %#v", row["props"])
	}
	t.Logf("serialization shape pinned (A1): id=%q labels=%v props-is-map=true", id, labels)

	// Confirm the SAME shape flows through normalizeRows (the projected-field keys).
	res := normalizeRows(OpExpand, []map[string]any{{
		"s_id": id, "s_labels": labelsJSON, "s_props": row["props"],
	}})
	if len(res.Nodes) != 1 || res.Nodes[0].ID != id {
		t.Fatalf("normalizeRows did not reconstruct the live node: %+v", res.Nodes)
	}
	if len(res.Nodes[0].Labels) == 0 {
		t.Fatalf("normalizeRows lost the live labels: %+v", res.Nodes[0])
	}
}

// TestGraphViewLive_Footprint probes the conversation footprint (A3): it records
// the real :Conversation/:Message/:Entity footprint in a t.Log and asserts the
// seed path returns cleanly when a :Conversation exists, OR that GraphView.Query
// falls back to the schema overview when the thread has no graph footprint (so the
// default open is never blank).
func TestGraphViewLive_Footprint(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	mcp := openTestMCP(ctx, t)
	defer mcp.Close()
	session, cleanup := seedGraphViewLiveFixture(ctx, t, mcp)
	defer cleanup()
	gv := NewGraphView(mcp)

	// What does the live loop actually write? (A3 observation, recorded for the executor.)
	convRows, err := mcp.Read(ctx, `MATCH (c:Conversation) RETURN c.session_id AS session LIMIT 5`, nil)
	if err != nil {
		t.Fatalf("conversation footprint probe: %v", err)
	}
	t.Logf("A3 footprint: %d :Conversation node(s) with a session_id (sampled up to 5)", len(convRows))

	if len(convRows) > 0 {
		// Run the real compileSeed path for one observed session; it must return
		// cleanly (rows or a clean empty), proving the seed Cypher is valid live.
		res, err := gv.Query(ctx, GraphIntent{Op: OpSeed, Session: session})
		if err != nil {
			t.Fatalf("live seed Query(session=%q): %v", session, err)
		}
		t.Logf("live seed for session=%q → %d nodes, %d edges (Query=%d chars)",
			session, len(res.Nodes), len(res.Edges), len(res.Query))
	} else {
		t.Log("no :Conversation footprint live — the loop may write only :Entity nodes; asserting the schema-overview fallback")
	}

	// Regardless: a non-existent thread MUST fall back to the drawable relationship
	// overview + schema, never a blank result (D-08). On a graph with relationships this
	// may legitimately include sample nodes/edges; on an empty graph it may be schema-only.
	res, err := gv.Query(ctx, GraphIntent{Op: OpSeed, Session: "no-such-thread-xyz-27"})
	if err != nil {
		t.Fatalf("empty-seed fallback Query: %v", err)
	}
	if len(res.Schema.Labels) == 0 {
		t.Fatalf("empty-seed must fall back to a non-empty schema overview, got %+v", res.Schema)
	}
	if !strings.Contains(res.Query, "MATCH (s)-[r]->(n)") {
		t.Fatalf("empty-seed fallback should expose the overview Cypher, got %q", res.Query)
	}
	t.Logf("empty-seed fallback OK: overview carries %d nodes, %d edges, %d labels",
		len(res.Nodes), len(res.Edges), len(res.Schema.Labels))
}

// seedOwnershipFixture writes the ownership shape the agent-memory sidecar ACTUALLY
// writes — (:User)-[:HAS_ENTITY]->(:Entity) and (:User)-[:HAS_CONVERSATION]->
// (:Conversation)-[:HAS_MESSAGE]->(:Message) — for TWO distinct users. The pre-existing
// seedGraphViewLiveFixture invents a Conversation->Message-[:MENTIONS]->Entity path that
// no writer produces, which is exactly why the scoped queries could be broken in
// production while the live tests stayed green: the fixture built the graph the query
// wanted instead of the graph that exists.
func seedOwnershipFixture(ctx context.Context, t *testing.T, mcp *Client) (userA, userB, entityAID string, cleanup func()) {
	t.Helper()

	runID := fmt.Sprintf("graphview-own-%d", time.Now().UnixNano())
	userA, userB = runID+"-user-a", runID+"-user-b"
	params := map[string]any{
		"run": runID, "user_a": userA, "user_b": userB,
		"session": runID + "-session",
	}
	const seed = `CREATE (ua:User {identifier:$user_a, test_run_id:$run})
CREATE (ub:User {identifier:$user_b, test_run_id:$run})
CREATE (ea:Entity {name:'Ownership Fixture A', type:'PERSON', test_run_id:$run})
CREATE (eb:Entity {name:'Ownership Fixture B', type:'PERSON', test_run_id:$run})
CREATE (c:Conversation {session_id:$session, test_run_id:$run})
CREATE (m:Message {test_run_id:$run})
CREATE (ua)-[:HAS_ENTITY {test_run_id:$run}]->(ea)
CREATE (ub)-[:HAS_ENTITY {test_run_id:$run}]->(eb)
CREATE (ua)-[:HAS_CONVERSATION {test_run_id:$run}]->(c)
CREATE (c)-[:HAS_MESSAGE {test_run_id:$run}]->(m)`
	if _, err := mcp.Write(ctx, seed, params); err != nil {
		t.Fatalf("seed ownership fixture: %v", err)
	}

	cleanup = func() {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if _, err := mcp.Write(cleanupCtx, `MATCH (n {test_run_id:$run}) DETACH DELETE n`, map[string]any{"run": runID}); err != nil {
			t.Logf("cleanup ownership fixture %q: %v", runID, err)
		}
	}

	// The mcp write tool answers with a mutation summary, not the RETURN rows, so the
	// anchor id comes from a follow-up read. Any failure past this point must still tear
	// the fixture down — a leaked :User is exactly the state that broke production.
	rows, err := mcp.Read(ctx,
		`MATCH (:User {identifier:$user_a})-[:HAS_ENTITY]->(ea:Entity) RETURN elementId(ea) AS entity_a`,
		map[string]any{"user_a": userA})
	if err != nil {
		cleanup()
		t.Fatalf("read ownership fixture anchor: %v", err)
	}
	if len(rows) > 0 {
		entityAID, _ = rows[0]["entity_a"].(string)
	}
	if entityAID == "" {
		cleanup()
		t.Fatalf("ownership fixture did not yield entity A's elementId: %+v", rows)
	}

	return userA, userB, entityAID, cleanup
}

// TestGraphViewLive_EveryCompiledQueryExecutes runs EVERY compiled query against the live
// database, scoped and unscoped, and fails on any error. The unit tests assert query shape
// with strings.Contains and never parse the Cypher, so compileExpand shipped with an
// unbalanced `})` — one paren left open — and answered 502 on every click-to-expand while
// the suite stayed green. Substring assertions cannot catch that; executing it can.
func TestGraphViewLive_EveryCompiledQueryExecutes(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	mcp := openTestMCP(ctx, t)
	defer mcp.Close()
	userA, _, entityAID, cleanup := seedOwnershipFixture(ctx, t, mcp)
	defer cleanup()

	for _, userID := range []string{"", userA} {
		scope := "unscoped"
		if userID != "" {
			scope = "scoped"
		}
		intents := map[string]GraphIntent{
			"seed":     {Op: OpSeed, Session: "any-session", UserID: userID},
			"overview": {Op: OpSchemaOverview, UserID: userID},
			"expand":   {Op: OpExpand, SeedID: entityAID, UserID: userID},
		}
		for name, in := range intents {
			t.Run(scope+"/"+name, func(t *testing.T) {
				var cypher string
				var params map[string]any
				switch in.Op {
				case OpSeed:
					cypher, params = compileSeed(in)
				case OpSchemaOverview:
					cypher, params = compileOverview(in)
				default:
					cypher, params = compileExpand(in)
				}
				if _, err := mcp.Read(ctx, cypher, params); err != nil {
					t.Fatalf("compiled %s query failed against the live database: %v\n%s", name, err, cypher)
				}
			})
		}
	}
}

// TestGraphViewLive_OwnershipIsolation is the direct regression for the empty cockpit
// graph. The replaced query gated every row on a `single_tenant` heuristic — "no other
// :User exists" — so a second identity turned the whole graph off for everyone, silently.
// This asserts BOTH halves at once: the caller's own subgraph is non-empty WHILE a second
// user exists, and the second user's node never appears in it.
func TestGraphViewLive_OwnershipIsolation(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	mcp := openTestMCP(ctx, t)
	defer mcp.Close()
	userA, userB, entityAID, cleanup := seedOwnershipFixture(ctx, t, mcp)
	defer cleanup()
	gv := NewGraphView(mcp)

	res, err := gv.Query(ctx, GraphIntent{Op: OpSchemaOverview, UserID: userA})
	if err != nil {
		t.Fatalf("scoped overview: %v", err)
	}
	captions := make([]string, 0, len(res.Nodes))
	for _, n := range res.Nodes {
		captions = append(captions, n.Caption)
	}
	joined := strings.Join(captions, "|")
	if !strings.Contains(joined, "Ownership Fixture A") {
		t.Fatalf("scoped overview lost the caller's OWN entity while a second :User exists "+
			"(the single_tenant regression); captions=%v", captions)
	}
	if strings.Contains(joined, "Ownership Fixture B") {
		t.Fatalf("scoped overview leaked another identity's entity; captions=%v", captions)
	}

	// Expand is scoped by the SAME ownership set: user B may not expand user A's node.
	foreign, err := gv.Query(ctx, GraphIntent{Op: OpExpand, SeedID: entityAID, UserID: userB})
	if err != nil {
		t.Fatalf("foreign expand: %v", err)
	}
	if len(foreign.Nodes) != 0 {
		t.Fatalf("expand leaked a node the caller does not own: %+v", foreign.Nodes)
	}
}

// TestGraphViewLive_Schema asserts GraphView.Schema returns a non-empty label set
// from the live graph (the legend + filter source). The schema is dynamic, so it
// asserts non-empty only — never a specific label set.
func TestGraphViewLive_Schema(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	mcp := openTestMCP(ctx, t)
	defer mcp.Close()
	_, cleanup := seedGraphViewLiveFixture(ctx, t, mcp)
	defer cleanup()
	gv := NewGraphView(mcp)

	sch, err := gv.Schema(ctx)
	if err != nil {
		t.Fatalf("live Schema: %v", err)
	}
	if len(sch.Labels) == 0 {
		t.Fatalf("live schema label set is empty — the legend/filters would be blank")
	}
	t.Logf("live schema: %d labels, %d rel-types, %d entity-types",
		len(sch.Labels), len(sch.RelTypes), len(sch.EntityTypes))
}
