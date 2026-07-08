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
	UserID   string   `json:"-"`                   // authenticated Aura identity; never client-supplied
}

// compileSeed emits the conversation-footprint read (D-07): the agent-memory
// entities tied to the active session, plus their immediate neighbors. On zero
// rows the caller falls back to the graph overview (D-08).
func compileSeed(in GraphIntent) (cypher string, params map[string]any) {
	cypher = `MATCH (c:Conversation {session_id:$session})
WHERE $user_id = '' OR c.user_identifier = $user_id OR EXISTS {
  MATCH (:User {identifier:$user_id})-[:HAS_CONVERSATION]->(c)
}
MATCH (c)-[:HAS_MESSAGE]->(:Message)-[:MENTIONS]->(e:Entity)
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
  apoc.map.removeKey(properties(e), 'embedding') AS s_props,
  elementId(n)                       AS n_id,
  apoc.convert.toJson(labels(n))     AS n_labels,
  n.type                             AS n_entity_type,
  coalesce(n.name, n.canonical_name) AS n_caption,
  apoc.map.removeKey(properties(n), 'embedding') AS n_props,
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
		"user_id":   in.UserID,
	}
}

// compileOverview emits a bounded relationship sample across the live graph. It is
// the drawable fallback for stores that do not currently write the older
// Conversation->Message->MENTIONS footprint but do contain evidence relationships
// such as Document->Chunk, Chunk->Chunk, and User->Preference. Filters are still
// bound as data; the label filter keeps an edge when either endpoint carries one
// of the selected labels. A label-filtered branch also returns isolated learning
// evidence nodes (ReasoningExample/ToolSelectionExample), because those nodes are
// intentionally stored without relationships and would otherwise look "empty".
//
// The three ownership facts are ROW-INVARIANT, so they are computed ONCE in the
// leading WITH and imported into the CALL, never re-derived per candidate edge:
// `single_tenant` (this is the only tenant) and `owned_entities` (the elementIds
// of the Entities the caller's conversations mention). Before this, the per-row
// WHERE re-ran a full :User label scan AND a 4-hop HAS_CONVERSATION expand for
// EVERY relationship, making the overview O(relationships x users) — measured at
// 216.9k db-hits / ~760ms on a 6.5k-node graph. Hoisting drops it to one relationship
// scan + one entity roll-up (~12k db-hits, -94%). The boolean factoring is exact:
// `unscoped OR elementId(s|n) IN owned_entities OR (single_tenant AND <label>)` has
// the same truth value as the original nested EXISTS, verified row-for-row live
// (unscoped, scoped-owning, and cross-tenant no-leak cases).
func compileOverview(in GraphIntent) (cypher string, params map[string]any) {
	cypher = `WITH
  ($user_id = '') AS unscoped,
  (EXISTS { MATCH (:User {identifier:$user_id}) }
   AND NOT EXISTS { MATCH (other:User) WHERE coalesce(other.identifier, other.id, '') <> $user_id }) AS single_tenant,
  [ (:User {identifier:$user_id})-[:HAS_CONVERSATION]->(:Conversation)-[:HAS_MESSAGE]->(:Message)-[:MENTIONS]->(e:Entity) | elementId(e) ] AS owned_entities
CALL {
  WITH unscoped, single_tenant, owned_entities
  MATCH (s)-[r]->(n)
  WHERE (unscoped
    OR elementId(s) IN owned_entities OR elementId(n) IN owned_entities
    OR (single_tenant AND (
      s:Document OR s:Chunk OR s:Preference OR s:Entity OR s:Source OR s:ReasoningExample OR s:ToolSelectionExample OR
      n:Document OR n:Chunk OR n:Preference OR n:Entity OR n:Source OR n:ReasoningExample OR n:ToolSelectionExample
    )))
    AND ($rel_types = [] OR type(r) IN $rel_types)
    AND ($labels = [] OR any(l IN labels(s) WHERE l IN $labels) OR any(l IN labels(n) WHERE l IN $labels))
  WITH s, r, n LIMIT $edge_cap
  RETURN s, r, n
  UNION
  WITH unscoped, single_tenant
  MATCH (s)
  WHERE $labels <> []
    AND any(l IN labels(s) WHERE l IN $labels)
    AND NOT (s)--()
    AND (unscoped OR (single_tenant AND (s:ReasoningExample OR s:ToolSelectionExample)))
  WITH s LIMIT $node_cap
  RETURN s, null AS r, null AS n
}
WITH collect({s:s, r:r, n:n}) AS rows
WITH [row IN rows | elementId(row.s)] + [row IN rows WHERE row.n IS NOT NULL | elementId(row.n)] AS candidate_ids, rows
UNWIND candidate_ids AS candidate_id
WITH DISTINCT candidate_id, rows
WHERE candidate_id IS NOT NULL
WITH candidate_id, rows LIMIT $node_cap
WITH collect(candidate_id) AS node_ids, rows
UNWIND rows AS row
WITH row.s AS s, row.r AS r, row.n AS n, node_ids
WHERE elementId(s) IN node_ids AND (n IS NULL OR elementId(n) IN node_ids)
RETURN
  elementId(s)                       AS s_id,
  apoc.convert.toJson(labels(s))     AS s_labels,
  s.type                             AS s_entity_type,
  coalesce(s.name, s.canonical_name, s.tool, s.query) AS s_caption,
  apoc.map.removeKey(properties(s), 'embedding') AS s_props,
  CASE WHEN n IS NULL THEN '' ELSE elementId(n) END AS n_id,
  CASE WHEN n IS NULL THEN '' ELSE apoc.convert.toJson(labels(n)) END AS n_labels,
  CASE WHEN n IS NULL THEN '' ELSE n.type END AS n_entity_type,
  CASE WHEN n IS NULL THEN '' ELSE coalesce(n.name, n.canonical_name, n.tool, n.query) END AS n_caption,
  CASE WHEN n IS NULL THEN {} ELSE apoc.map.removeKey(properties(n), 'embedding') END AS n_props,
  CASE WHEN r IS NULL THEN '' ELSE elementId(r) END AS r_id,
  CASE WHEN r IS NULL THEN '' ELSE type(r) END AS r_type,
  CASE WHEN r IS NULL THEN '' ELSE elementId(startNode(r)) END AS r_src,
  CASE WHEN r IS NULL THEN '' ELSE elementId(endNode(r)) END AS r_dst`
	return cypher, map[string]any{
		"labels":    nonNil(in.Labels),
		"rel_types": nonNil(in.RelTypes),
		"node_cap":  clamp(in.NodeCap, defaultNodeCap, maxNodeCap),
		"edge_cap":  clamp(in.EdgeCap, defaultEdgeCap, maxEdgeCap),
		"user_id":   in.UserID,
	}
}

// compileExpand emits the click-to-expand-neighbors read (D-04): the seed node's
// immediate neighbors, filtered by the bound label/rel-type sets, capped.
func compileExpand(in GraphIntent) (cypher string, params map[string]any) {
	cypher = `MATCH (s)
WHERE elementId(s) = $seed
  AND ($user_id = '' OR (EXISTS {
    MATCH (:User {identifier:$user_id})-[:HAS_CONVERSATION]->(:Conversation)-[:HAS_MESSAGE]->(:Message)-[:MENTIONS]->(root:Entity)
    WHERE root = s OR (root)--(s)
  }) OR (
    EXISTS { MATCH (:User {identifier:$user_id}) }
    AND NOT EXISTS {
      MATCH (other:User)
      WHERE coalesce(other.identifier, other.id, '') <> $user_id
    }
    AND (s:Document OR s:Chunk OR s:Preference OR s:Entity OR s:Source OR s:User OR s:ReasoningExample OR s:ToolSelectionExample)
  })
OPTIONAL MATCH (s)-[r]-(n)
WHERE ($rel_types = [] OR type(r) IN $rel_types)
  AND ($labels = [] OR any(l IN labels(n) WHERE l IN $labels))
WITH s, r, n LIMIT $edge_cap
RETURN
  elementId(s)                       AS s_id,
  apoc.convert.toJson(labels(s))     AS s_labels,
  s.type                             AS s_entity_type,
  coalesce(s.name, s.canonical_name, s.tool, s.query) AS s_caption,
  apoc.map.removeKey(properties(s), 'embedding') AS s_props,
  elementId(n)                       AS n_id,
  apoc.convert.toJson(labels(n))     AS n_labels,
  n.type                             AS n_entity_type,
  coalesce(n.name, n.canonical_name, n.tool, n.query) AS n_caption,
  apoc.map.removeKey(properties(n), 'embedding') AS n_props,
  elementId(r)                       AS r_id,
  type(r)                            AS r_type,
  elementId(startNode(r))            AS r_src,
  elementId(endNode(r))              AS r_dst`
	return cypher, map[string]any{
		"seed":      in.SeedID,
		"labels":    nonNil(in.Labels),
		"rel_types": nonNil(in.RelTypes),
		"edge_cap":  clamp(in.EdgeCap, defaultEdgeCap, maxEdgeCap),
		"user_id":   in.UserID,
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
