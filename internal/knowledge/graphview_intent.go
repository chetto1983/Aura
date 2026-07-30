// Structured graph-query intents → parameterized read-Cypher (Phase 27, GRAPH-01).
// Every intent value rides the returned param map ($seed/$session/$labels/
// $rel_types/$node_cap/$edge_cap) — there is NO fmt.Sprintf of any value into the
// query body (D-05, T-27-01). Label/rel-type filters are bound as DATA in
// `WHERE x IN $list`, never interpolated as Cypher label syntax. Every projection
// uses explicit scalar fields + apoc.convert.toJson(labels(...)) so node labels +
// element-ids survive the mcp-neo4j-cypher serialization boundary (Pattern 1; a
// bare `RETURN n` or a bare labels() list would lose data).
package knowledge

import (
	"strconv"
	"strings"
)

// captionMaxChars bounds a projected caption. A :Message carries its whole body in
// `content`; unbounded it would ship a paragraph per node to the canvas.
const captionMaxChars = 140

// captionProperties is the ordered search for a node's human-readable name, most
// specific first. Every label the graph actually holds is represented: an Entity names
// itself, but a Fact is identified by its predicate, a Preference by its text, a Message
// by its content and a Conversation by its session. Only `name`/`canonical_name` used to
// be consulted, so every node that was not an Entity projected an EMPTY caption and the
// cockpit fell back to printing the raw Neo4j elementId — a screen of
// "4:4260efd2-fa44-…:21" where names belong.
//
// `identifier` is deliberately absent: it is the :User node's UUID, so consulting it would
// reintroduce exactly the unreadable id this list exists to remove. A User with no caption
// renders as its label instead.
//
// `file_name` precedes `title` for a reason the canvas made visible: the extractor derives a
// Document's `title` from the basename of the UPLOAD TEMP FILE for every format that has no
// embedded document title (xlsx, docx, pptx, csv), so the operator saw a node captioned
// `tmpfl5h6p91.xlsx` next to an inspector panel reading `file_name: Clienti.xlsx`. `file_name`
// carries the name the operator actually chose and is correct on every Document in the live
// graph; `title` is better ONLY when it is a real embedded title (a PDF's), and that case
// still wins for the nodes that have no file_name at all. Ordering the reliable property
// first makes the caption robust to a bad title instead of merely hoping the title is good.
var captionProperties = []string{
	"name", "canonical_name", // Entity
	"preference", // Preference
	"file_name",  // Document — the operator's own name for it, always correct
	"title",      // Document fallback — a real embedded title, or a temp basename
	"predicate",  // Fact
	"content",    // Message
	"query",      // ReasoningStep
	"tool",       // ToolCall
	"session_id", // Conversation
}

// captionExpr renders the caption projection for a node variable. toString() guards a
// non-string property (an epoch counter, a number) that would otherwise make left() fail at
// runtime for the whole query. A :Message caption is a markdown body, so line breaks and
// tabs collapse to spaces before truncation — a caption is a one-line label on the canvas
// and in the evidence rows, and raw newlines break both.
func captionExpr(v string) string {
	refs := make([]string, 0, len(captionProperties)+1)
	for _, prop := range captionProperties {
		refs = append(refs, v+"."+prop)
	}
	refs = append(refs, "''")
	raw := "toString(coalesce(" + strings.Join(refs, ", ") + "))"
	flat := `replace(replace(replace(` + raw + `, '\n', ' '), '\r', ' '), '\t', ' ')`
	return "left(trim(" + flat + "), " + strconv.Itoa(captionMaxChars) + ")"
}

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
  ` + captionExpr("e") + ` AS s_caption,
  apoc.map.removeKey(properties(e), 'embedding') AS s_props,
  elementId(n)                       AS n_id,
  apoc.convert.toJson(labels(n))     AS n_labels,
  n.type                             AS n_entity_type,
  ` + captionExpr("n") + ` AS n_caption,
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

// ownedIDsPrelude computes the caller's ownership set ONCE, as the leading clause of
// every scoped read (overview + expand). It is the SINGLE definition of "what this
// identity may see": the caller's own :User node, everything that node points at
// directly (HAS_ENTITY / HAS_FACT / HAS_PREFERENCE / HAS_CONVERSATION — the shape the
// agent-memory sidecar actually writes), and the messages hanging off the caller's
// conversations. `unscoped` — an empty $user_id — is the CLI/unauthenticated path.
//
// It replaces two constructs that made the cockpit graph render EMPTY in production:
//
//   - a `single_tenant` heuristic — "I am the only :User in the database" — which is
//     false the moment a second :User node exists. One appeared on 2026-07-25 (the
//     memory-sidecar dogfooding session) and every scoped read silently returned zero
//     rows from that moment on, with no error and no log.
//   - an `owned_entities` roll-up traversing
//     User->Conversation->Message-[:MENTIONS]->Entity. `MENTIONS` is not a relationship
//     type the sidecar writes, so that list was ALWAYS empty; the query was written
//     against a graph shape that does not exist.
//
// Ownership is now membership in one id set and BOTH endpoints must be members, so a
// node owned by another identity can never be drawn — strictly tighter than the
// single-tenant escape hatch it replaces, and correct with any number of users.
// collect() over the OPTIONAL MATCH folds to exactly one row and drops the null when
// the caller has no :User node yet (a fresh identity sees an empty graph, not a leak).
const ownedIDsPrelude = `OPTIONAL MATCH (me:User {identifier:$user_id})
WITH ($user_id = '') AS unscoped, collect(elementId(me)) AS me_ids
WITH unscoped, me_ids
   + [ (:User {identifier:$user_id})-->(x) | elementId(x) ]
   + [ (:User {identifier:$user_id})-->(:Conversation)-->(msg) | elementId(msg) ] AS owned_ids
`

// compileOverview emits a bounded relationship sample across the caller's graph. It is
// the drawable default: every relationship whose two endpoints are both in the caller's
// ownership set (see ownedIDsPrelude), capped and filtered. Filters are still bound as
// data; the label filter keeps an edge when either endpoint carries one of the selected
// labels.
//
// The ownership set is ROW-INVARIANT, so it is computed once in the prelude and imported
// into the CALL, never re-derived per candidate edge — the per-row form re-ran a :User
// label scan plus a multi-hop expand for EVERY relationship (216.9k db-hits / ~760ms on a
// 6.5k-node graph; the hoisted form is ~12k).
func compileOverview(in GraphIntent) (cypher string, params map[string]any) {
	cypher = ownedIDsPrelude + `CALL {
  WITH unscoped, owned_ids
  MATCH (s)-[r]->(n)
  WHERE (unscoped OR (elementId(s) IN owned_ids AND elementId(n) IN owned_ids))
    AND ($rel_types = [] OR type(r) IN $rel_types)
    AND ($labels = [] OR any(l IN labels(s) WHERE l IN $labels) OR any(l IN labels(n) WHERE l IN $labels))
  WITH s, r, n LIMIT $edge_cap
  RETURN s, r, n
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
  ` + captionExpr("s") + ` AS s_caption,
  apoc.map.removeKey(properties(s), 'embedding') AS s_props,
  CASE WHEN n IS NULL THEN '' ELSE elementId(n) END AS n_id,
  CASE WHEN n IS NULL THEN '' ELSE apoc.convert.toJson(labels(n)) END AS n_labels,
  CASE WHEN n IS NULL THEN '' ELSE n.type END AS n_entity_type,
  CASE WHEN n IS NULL THEN '' ELSE ` + captionExpr("n") + ` END AS n_caption,
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
// immediate neighbors, filtered by the bound label/rel-type sets, capped. It shares
// ownedIDsPrelude with the overview, so a node the caller can SEE is exactly a node the
// caller can EXPAND.
//
// The scope clause it replaces was not merely wrong, it was UNPARSEABLE: an unbalanced
// `})` left one paren open, so Neo4j rejected the whole statement at parse time and every
// expand answered 502. It survived review because the unit tests assert on the query with
// strings.Contains and never parse it — the same blind spot that let the sidecar's
// `*_SCOPED` family ship green (see docs/audit/consolidated-fix-plan-2026-07-20.md, M-4).
func compileExpand(in GraphIntent) (cypher string, params map[string]any) {
	cypher = ownedIDsPrelude + `MATCH (s)
WHERE elementId(s) = $seed
  AND (unscoped OR elementId(s) IN owned_ids)
OPTIONAL MATCH (s)-[r]-(n)
WHERE (unscoped OR elementId(n) IN owned_ids)
  AND ($rel_types = [] OR type(r) IN $rel_types)
  AND ($labels = [] OR any(l IN labels(n) WHERE l IN $labels))
WITH s, r, n LIMIT $edge_cap
RETURN
  elementId(s)                       AS s_id,
  apoc.convert.toJson(labels(s))     AS s_labels,
  s.type                             AS s_entity_type,
  ` + captionExpr("s") + ` AS s_caption,
  apoc.map.removeKey(properties(s), 'embedding') AS s_props,
  elementId(n)                       AS n_id,
  apoc.convert.toJson(labels(n))     AS n_labels,
  n.type                             AS n_entity_type,
  ` + captionExpr("n") + ` AS n_caption,
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
