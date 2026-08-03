// types.ts mirrors the Phase-27 read-only graph contract field-for-field with the
// Go structs in internal/agui/graph_contract.go. The TS keys
// are the Go json TAG names (not the Go field names) because the wire is JSON: a
// GraphResult round-trips graphview.go's `json:"..."` tags exactly. There is NO
// sigma/graphology/@react-sigma import anywhere in this module — it is a pure data
// contract consumed by both the jsdom-tested core (graphApi.ts/graphIntent.ts) and,
// only in plan 04, the WebGL SigmaCanvas (jsdom has no WebGL — Pitfall 4).

/** The only two graph operations accepted by the Go read contract. */
export type GraphOp = 'overview' | 'expand';

export const OP_OVERVIEW: GraphOp = 'overview';
export const OP_EXPAND: GraphOp = 'expand';

/** GraphIntent mirrors agui.GraphIntent: the structured
 * payload the cockpit POSTs to /api/graph/query. The client authors no query — it
 * sends this typed intent ONLY (D-05). node_id/labels/rel_types/node_cap/
 * edge_cap match the Go json tags; the omitempty fields are optional here. */
export interface GraphIntent {
  readonly op: GraphOp;
  readonly node_id?: string;
  readonly labels?: readonly string[];
  readonly rel_types?: readonly string[];
  readonly node_cap?: number;
  readonly edge_cap?: number;
}

/** GraphNode mirrors agui.GraphNode. id is the store's own record id (de-dupe + edge
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

/** GraphEdge mirrors agui.GraphEdge: source/target are ArcadeDB RIDs matching
 * node ids; rel_type is the relationship type. */
export interface GraphEdge {
  readonly id: string;
  readonly source: string;
  readonly target: string;
  readonly rel_type?: string;
  readonly caption?: string;
}

/** GraphPath mirrors agui.GraphPath — an ordered run of edges (the path strip). */
export interface GraphPath {
  readonly steps: readonly GraphEdge[];
}

/** GraphSchema mirrors agui.GraphSchema: the live introspection result driving
 * the left-panel filters + color legend + overview fallback. entity_types is
 * the POLE+O second color dimension. */
export interface GraphSchema {
  readonly labels: readonly string[];
  readonly rel_types: readonly string[];
  readonly property_keys?: readonly string[];
  readonly entity_types?: readonly string[];
  readonly counts?: Readonly<Record<string, number>>;
}

/** GraphResult mirrors agui.GraphResult. query is display-only ArcadeDB SQL and
 * truncated reports that the bounded server read hit a cap. */
export interface GraphResult {
  readonly nodes: readonly GraphNode[];
  readonly edges: readonly GraphEdge[];
  readonly paths?: readonly GraphPath[];
  readonly schema: GraphSchema;
  readonly query: string;
  readonly truncated?: boolean;
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
