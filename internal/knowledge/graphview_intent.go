// Structured graph-query intents → parameterized read-Cypher (Phase 27, GRAPH-01).
// Every intent value rides the returned param map ($seed/$session/$labels/
// $rel_types/$node_cap/$edge_cap) — there is NO fmt.Sprintf of any value into the
// query body (D-05, T-27-01). Label/rel-type filters are bound as DATA in
// `WHERE x IN $list`, never interpolated as Cypher label syntax. Every projection
// uses explicit scalar fields + apoc.convert.toJson(labels(...)) so node labels +
// element-ids survive the mcp-neo4j-cypher serialization boundary (Pattern 1; a
// bare `RETURN n` or a bare labels() list would lose data).
package knowledge

// Op enum (GraphIntent.Op). Exported so the REST layer (Phase 27 plan 02) can
// enum-validate an inbound intent against the SAME constants the dispatcher switches on —
// one source of truth for the wire op set, no drift between the validator and the compiler.
const (
	OpSeed           = "seed"
	OpExpand         = "expand"
	OpSchemaOverview = "schema_overview"
)

// GraphIntent is the structured payload the cockpit submits (D-05). The compilers
// turn it into parameterized read-Cypher; the cockpit never authors Cypher.
type GraphIntent struct {
	Op       string   `json:"op"`                  // "seed" | "expand" | "schema_overview"
	SeedID   string   `json:"seed_id,omitempty"`   // elementId anchor for "expand"
	Session  string   `json:"session,omitempty"`   // active conversation ThreadID for "seed"
	Labels   []string `json:"labels,omitempty"`    // include-label filter (bound as data)
	RelTypes []string `json:"rel_types,omitempty"` // include-rel filter (bound as data)
	NodeCap  int      `json:"node_cap,omitempty"`  // default 75, hard max 300
	EdgeCap  int      `json:"edge_cap,omitempty"`  // default 200, hard max 800
}

// compileSeed emits the conversation-footprint read (D-07): the agent-memory
// entities tied to the active session, plus their immediate neighbors. On zero
// rows the caller falls back to the schema overview (D-08).
func compileSeed(in GraphIntent) (cypher string, params map[string]any) {
	cypher = `MATCH (:Conversation {session_id:$session})-[:HAS_MESSAGE]->(:Message)-[:MENTIONS]->(e:Entity)
WITH DISTINCT e LIMIT $node_cap
OPTIONAL MATCH (e)-[r]-(n)
WHERE ($rel_types = [] OR type(r) IN $rel_types)
  AND ($labels = [] OR any(l IN labels(n) WHERE l IN $labels))
WITH e, r, n LIMIT $edge_cap
RETURN
  elementId(e)                       AS s_id,
  apoc.convert.toJson(labels(e))     AS s_labels,
  e.type                             AS s_entity_type,
  coalesce(e.name, e.canonical_name) AS s_caption,
  properties(e)                      AS s_props,
  elementId(n)                       AS n_id,
  apoc.convert.toJson(labels(n))     AS n_labels,
  n.type                             AS n_entity_type,
  coalesce(n.name, n.canonical_name) AS n_caption,
  properties(n)                      AS n_props,
  elementId(r)                       AS r_id,
  type(r)                            AS r_type,
  elementId(startNode(r))            AS r_src,
  elementId(endNode(r))              AS r_dst`
	return cypher, map[string]any{
		"session":   in.Session,
		"labels":    nonNil(in.Labels),
		"rel_types": nonNil(in.RelTypes),
		"node_cap":  clamp(in.NodeCap, defaultNodeCap, maxNodeCap),
		"edge_cap":  clamp(in.EdgeCap, defaultEdgeCap, maxEdgeCap),
	}
}

// compileExpand emits the click-to-expand-neighbors read (D-04): the seed node's
// immediate neighbors, filtered by the bound label/rel-type sets, capped.
func compileExpand(in GraphIntent) (cypher string, params map[string]any) {
	cypher = `MATCH (s) WHERE elementId(s) = $seed
OPTIONAL MATCH (s)-[r]-(n)
WHERE ($rel_types = [] OR type(r) IN $rel_types)
  AND ($labels = [] OR any(l IN labels(n) WHERE l IN $labels))
WITH s, r, n LIMIT $edge_cap
RETURN
  elementId(s)                       AS s_id,
  apoc.convert.toJson(labels(s))     AS s_labels,
  s.type                             AS s_entity_type,
  coalesce(s.name, s.canonical_name) AS s_caption,
  properties(s)                      AS s_props,
  elementId(n)                       AS n_id,
  apoc.convert.toJson(labels(n))     AS n_labels,
  n.type                             AS n_entity_type,
  coalesce(n.name, n.canonical_name) AS n_caption,
  properties(n)                      AS n_props,
  elementId(r)                       AS r_id,
  type(r)                            AS r_type,
  elementId(startNode(r))            AS r_src,
  elementId(endNode(r))              AS r_dst`
	return cypher, map[string]any{
		"seed":      in.SeedID,
		"labels":    nonNil(in.Labels),
		"rel_types": nonNil(in.RelTypes),
		"edge_cap":  clamp(in.EdgeCap, defaultEdgeCap, maxEdgeCap),
	}
}

// compileSchema emits the live label/rel-type/property-key overview (D-06). The
// list results are wrapped with apoc.convert.toJson so they survive the read tool
// (lists return NULL otherwise — Pitfall 3), and the POLE+O Entity.type values are
// collected as the legend's second color dimension (D-02).
func compileSchema() (cypher string, params map[string]any) {
	cypher = `CALL db.labels() YIELD label
WITH collect(label) AS labels
CALL db.relationshipTypes() YIELD relationshipType
WITH labels, collect(relationshipType) AS rel_types
CALL db.propertyKeys() YIELD propertyKey
WITH labels, rel_types, collect(propertyKey) AS prop_keys
OPTIONAL MATCH (e:Entity) WHERE e.type IS NOT NULL
WITH labels, rel_types, prop_keys, collect(DISTINCT e.type) AS entity_types
RETURN
  apoc.convert.toJson(labels)        AS labels_json,
  apoc.convert.toJson(rel_types)     AS rel_types_json,
  apoc.convert.toJson(prop_keys)     AS prop_keys_json,
  apoc.convert.toJson(entity_types)  AS entity_types_json`
	return cypher, map[string]any{}
}

// clamp returns v floored to def (for v<=0) and capped to max.
func clamp(v, def, max int) int {
	if v <= 0 {
		return def
	}
	if v > max {
		return max
	}
	return v
}

// nonNil returns a non-nil []string so it binds as a Cypher list `[]` (an empty
// filter), never a NULL — `$labels = []` must be a valid comparison.
func nonNil(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}
