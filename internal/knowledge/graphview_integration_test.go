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

	// Regardless: a non-existent thread MUST fall back to the schema overview, never
	// a blank result (D-08). This is the load-bearing default-open guarantee.
	res, err := gv.Query(ctx, GraphIntent{Op: OpSeed, Session: "no-such-thread-xyz-27"})
	if err != nil {
		t.Fatalf("empty-seed fallback Query: %v", err)
	}
	if len(res.Nodes) != 0 {
		t.Fatalf("empty-seed must produce no nodes, got %d", len(res.Nodes))
	}
	if len(res.Schema.Labels) == 0 {
		t.Fatalf("empty-seed must fall back to a non-empty schema overview, got %+v", res.Schema)
	}
	t.Logf("empty-seed fallback OK: schema overview carries %d labels", len(res.Schema.Labels))
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
