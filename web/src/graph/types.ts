// types.ts mirrors the Phase-27 read-only graph contract field-for-field with the
// Go structs in internal/knowledge/graphview.go + graphview_intent.go. The TS keys
// are the Go json TAG names (not the Go field names) because the wire is JSON: a
// GraphResult round-trips graphview.go's `json:"..."` tags exactly. There is NO
// sigma/graphology/@react-sigma import anywhere in this module — it is a pure data
// contract consumed by both the jsdom-tested core (graphApi.ts/graphIntent.ts) and,
// only in plan 04, the WebGL SigmaCanvas (jsdom has no WebGL — Pitfall 4).

/** Op union — the underlying JSON STRING values of the Go OpSeed/OpExpand/OpSchemaOverview
 * constants (graphview_intent.go lines 15-17), NOT the unexported Go identifier names. */
export type GraphOp = 'seed' | 'expand' | 'schema_overview';

export const OP_SEED: GraphOp = 'seed';
export const OP_EXPAND: GraphOp = 'expand';

/** GraphIntent mirrors knowledge.GraphIntent (graphview_intent.go): the structured
 * payload the cockpit POSTs to /api/graph/query. The client authors NO Cypher — it
 * sends this typed intent ONLY (D-05). seed_id/session/labels/rel_types/node_cap/
 * edge_cap match the Go json tags; the omitempty fields are optional here. */
export interface GraphIntent {
  readonly op: GraphOp;
  readonly seed_id?: string;
  readonly session?: string;
  readonly labels?: readonly string[];
  readonly rel_types?: readonly string[];
  readonly node_cap?: number;
  readonly edge_cap?: number;
}

/** GraphNode mirrors knowledge.GraphNode. id is the Neo4j elementId (de-dupe + edge
 * anchor); entity_type is the POLE+O second color dimension; citations is the derived
 * Document/Source neighbor list (GRAPH-03). */
export interface GraphNode {
  readonly id: string;
  readonly caption?: string;
  readonly labels?: readonly string[];
  readonly entity_type?: string;
  readonly degree?: number;
  readonly props?: Readonly<Record<string, unknown>>;
  readonly ref_id?: string;
  readonly citations?: readonly string[];
}

/** GraphEdge mirrors knowledge.GraphEdge: source/target are elementId keys matching
 * node ids; rel_type is the relationship type. */
export interface GraphEdge {
  readonly id: string;
  readonly source: string;
  readonly target: string;
  readonly rel_type?: string;
}

/** GraphPath mirrors knowledge.GraphPath — an ordered run of edges (the path strip). */
export interface GraphPath {
  readonly steps: readonly GraphEdge[];
}

/** GraphSchema mirrors knowledge.GraphSchema: the live introspection result driving
 * the left-panel filters + color legend + schema-overview fallback. entity_types is
 * the POLE+O second color dimension. */
export interface GraphSchema {
  readonly labels: readonly string[];
  readonly rel_types: readonly string[];
  readonly property_keys?: readonly string[];
  readonly entity_types?: readonly string[];
  readonly counts?: Readonly<Record<string, number>>;
}

/** GraphResult mirrors knowledge.GraphResult — the flat contract a graph read emits.
 * query is the compiled display Cypher (read-only "Show Cypher" affordance, D-09). */
export interface GraphResult {
  readonly nodes: readonly GraphNode[];
  readonly edges: readonly GraphEdge[];
  readonly paths?: readonly GraphPath[];
  readonly schema: GraphSchema;
  readonly query: string;
}

/** ClientNode is a graphology-ready node projection (id/caption/color/size) the
 * SigmaCanvas (plan 04) loads. size encodes degree — a NON-color channel so color is
 * never the only encoding (WCAG 1.4.1 / D-03). It carries no sigma/graphology type. */
export interface ClientNode {
  readonly id: string;
  readonly caption: string;
  readonly color: string;
  readonly size: number;
  readonly labels: readonly string[];
  readonly entityType?: string;
}

/** ClientEdge is a graphology-ready edge projection (id/source/target/label). */
export interface ClientEdge {
  readonly id: string;
  readonly source: string;
  readonly target: string;
  readonly label: string;
}

/** ClientGraph is the {nodes,edges} pair rowsToClientGraph emits for the renderer. */
export interface ClientGraph {
  readonly nodes: readonly ClientNode[];
  readonly edges: readonly ClientEdge[];
}
