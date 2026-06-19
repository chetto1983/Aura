import {
  type ClientEdge,
  type ClientGraph,
  type ClientNode,
  type GraphEdge,
  type GraphIntent,
  type GraphNode,
  type GraphResult,
  type GraphSchema,
  OP_EXPAND,
  OP_SEED,
} from './types';

// graphIntent.ts is the PURE, jsdom-testable core of the graph workspace: the intent
// reducer (seed/expand/filter toggles), the active-filter dim/exclude logic, the
// schema-driven label-family color mapper (D-02), and the contract→graphology-ready
// projection. It imports NO sigma/@react-sigma/graphology (jsdom has no WebGL — the
// renderer is imported only by SigmaCanvas in plan 04, Pitfall 4). Keeping the logic
// off the .tsx is the sourceExplorerData.ts idiom: directly unit- + mutation-tested,
// so the ≥85% line / ≥70% mutation gates are reachable without real WebGL.
//
// COLOR is schema-DRIVEN from the brand accent ramp (D-02) — NOT a hard-coded per-label
// table and NOT a rainbow. Known Frame-06 families map to curated brand-token shades;
// an unmapped label gets a deterministic hash→ramp color so the legend is reproducible
// across sessions. Color is NEVER the only encoding (WCAG 1.4.1 / D-03): rowsToClientGraph
// also emits the caption text and encodes degree as node SIZE (a non-color channel).

/** Default subgraph caps (GRAPH-04 / D-08, mirrored from graphview.go's defaultNodeCap/
 * defaultEdgeCap). Dense graphs default to a legible filtered subgraph, not a hairball. */
export const DEFAULT_NODE_CAP = 75;
export const DEFAULT_EDGE_CAP = 200;

/** Curated, blue-anchored ramp DERIVED from the locked dark-operator brand tokens
 * (theme.css). The label-family palette maps onto THIS ramp; unmapped labels hash into
 * it. Each step meets ≥3:1 vs --color-bg (#131314) per the UI-SPEC contrast gate — the
 * ramp is the single source the contrast-check + the mapper share, so there is no
 * second hard-coded per-label table. */
export const BRAND_RAMP: readonly string[] = [
  '#AECBFA', // --color-info — accent blue (evidence/source anchor)
  '#81C995', // --color-success — the "self"/agent-memory signal
  '#FDD663', // --color-warning — warm-neutral (claims/facts)
  '#F28B82', // --color-danger — reserved conflict marker
  '#A7C7E7', // desaturated blue step
  '#B4A7D6', // muted violet-grey (topic clustering)
  '#7FC9C3', // desaturated teal/cyan (entity family)
  '#D7AEFB', // soft violet step
] as const;

/** Frame-06 semantic families (D-02). Each known family pins a BRAND_RAMP index; the
 * 'entity' family additionally varies by Entity.type as a SECOND color dimension. */
export type LabelFamily = 'source' | 'entity' | 'claim' | 'agent' | 'topic' | 'conflict';

/** familyRampIndex maps a known family to its anchor color in BRAND_RAMP. */
const FAMILY_RAMP_INDEX: Readonly<Record<LabelFamily, number>> = {
  source: 0, // --color-info evidence blue
  agent: 1, // --color-success self signal
  claim: 2, // warm-neutral
  conflict: 3, // reserved warm-alert
  topic: 5, // muted violet-grey
  entity: 6, // desaturated teal/cyan base
};

/** LABEL_FAMILY classifies a live Neo4j label into a Frame-06 family. The mapping is
 * label→family (semantic), NOT label→color — the color comes from the shared ramp, so
 * the palette stays brand-derived and reproducible (D-02). */
const LABEL_FAMILY: Readonly<Record<string, LabelFamily>> = {
  Document: 'source',
  Source: 'source',
  Chunk: 'source',
  Entity: 'entity',
  Fact: 'claim',
  Preference: 'claim',
  ReasoningTrace: 'agent',
  AgentEpisode: 'agent',
  Insight: 'agent',
  Topic: 'topic',
  Conflict: 'conflict',
};

/** POLE+O Entity.type sub-types — the SECOND color dimension within the entity family
 * (D-02): each shifts the entity base color a deterministic step along the ramp. */
const ENTITY_TYPE_OFFSET: Readonly<Record<string, number>> = {
  PERSON: 0,
  ORGANIZATION: 1,
  LOCATION: 2,
  EVENT: 3,
  OBJECT: 4,
};

/** stableHash is a small deterministic FNV-1a-style string hash — same input always
 * yields the same index, so an unmapped label's color is reproducible across sessions. */
function stableHash(s: string): number {
  let h = 2166136261;
  for (let i = 0; i < s.length; i += 1) {
    h ^= s.charCodeAt(i);
    h = Math.imul(h, 16777619);
  }
  return h >>> 0;
}

/** FALLBACK_COLOR is the provably-string ramp default (the evidence-blue anchor) — used
 * only if an index ever resolves out of the (non-empty) ramp, which the modulo prevents. */
const FALLBACK_COLOR = '#AECBFA';

/** rampColor returns BRAND_RAMP[index] wrapping the index into range — the single place
 * a ramp index becomes a hex string. */
function rampColor(index: number): string {
  const n = BRAND_RAMP.length;
  return BRAND_RAMP[((index % n) + n) % n] ?? FALLBACK_COLOR;
}

/** primaryLabel picks the most specific label for coloring: the first label that maps
 * to a known family, else the first label, else ''. The schema is reserved for future
 * legend ordering; today it makes the signature schema-aware per the UI-SPEC contract. */
function primaryLabel(labels: readonly string[]): string {
  for (const l of labels) {
    if (LABEL_FAMILY[l] !== undefined) return l;
  }
  return labels[0] ?? '';
}

/**
 * labelFamilyColor maps a live label (+ optional Entity.type) to a stable hex fill from
 * the brand ramp (D-02). Known families resolve via FAMILY_RAMP_INDEX; the entity family
 * shifts by Entity.type (the second color dimension); an unmapped label resolves
 * deterministically against the SAME ramp. The live schema makes the legend reproducible:
 * an unmapped label present in the schema's label set is anchored by its position in the
 * sorted label set (so the legend is stable across sessions); a label outside the schema
 * falls back to a content hash. There is NO per-label color table — every color is a
 * brand-ramp index.
 */
export function labelFamilyColor(
  label: string,
  entityType: string | undefined,
  schema: GraphSchema,
): string {
  const family = LABEL_FAMILY[label];
  if (family === undefined) {
    return rampColor(unmappedRampIndex(label, schema));
  }
  if (family === 'entity') {
    const offset = entityType !== undefined ? (ENTITY_TYPE_OFFSET[entityType] ?? 0) : 0;
    return rampColor(FAMILY_RAMP_INDEX.entity + offset);
  }
  return rampColor(FAMILY_RAMP_INDEX[family]);
}

/** unmappedRampIndex anchors an unmapped label deterministically. When the label is in
 * the live schema, its sorted position drives the index (legend reproducible across
 * sessions for the same schema); otherwise a content hash keeps it stable per-label. */
function unmappedRampIndex(label: string, schema: GraphSchema): number {
  const known = [...schema.labels].filter((l) => LABEL_FAMILY[l] === undefined).sort();
  const pos = known.indexOf(label);
  return pos >= 0 ? stableHash(label) + pos : stableHash(label);
}

/** GraphFilters is the active include-filter set. An EMPTY set on either axis means
 * "show all" on that axis (mirrors the Go `$labels = [] OR …` empty-filter semantics). */
export interface GraphFilters {
  readonly labels: ReadonlySet<string>;
  readonly relTypes: ReadonlySet<string>;
}

export const EMPTY_FILTERS: GraphFilters = {
  labels: new Set<string>(),
  relTypes: new Set<string>(),
};

/** FilteredGraph splits a result into the kept set (matches the active filters) and the
 * dimmed set (excluded) so the renderer can DIM rather than hard-remove — context is
 * preserved while the active filter is emphasised. */
export interface FilteredGraph {
  readonly nodes: readonly GraphNode[];
  readonly edges: readonly GraphEdge[];
  readonly dimmedNodeIds: ReadonlySet<string>;
  readonly dimmedEdgeIds: ReadonlySet<string>;
}

/** nodePasses is true when the node carries at least one active-filter label (or the
 * label filter is empty = show all). */
function nodePasses(node: GraphNode, labels: ReadonlySet<string>): boolean {
  if (labels.size === 0) return true;
  return (node.labels ?? []).some((l) => labels.has(l));
}

/**
 * applyFilters dims nodes whose label is not in the active label filter and edges whose
 * rel_type is not in the active relType filter. An EMPTY filter set on an axis = "show
 * all" on that axis. Excluded items are returned as the dimmed id sets (not removed) so
 * the canvas dims-for-context rather than dropping nodes.
 */
export function applyFilters(result: GraphResult, filters: GraphFilters): FilteredGraph {
  const dimmedNodeIds = new Set<string>();
  for (const node of result.nodes) {
    if (!nodePasses(node, filters.labels)) dimmedNodeIds.add(node.id);
  }
  const dimmedEdgeIds = new Set<string>();
  for (const edge of result.edges) {
    const typeExcluded = filters.relTypes.size > 0 && !filters.relTypes.has(edge.rel_type ?? '');
    const endpointDimmed = dimmedNodeIds.has(edge.source) || dimmedNodeIds.has(edge.target);
    if (typeExcluded || endpointDimmed) dimmedEdgeIds.add(edge.id);
  }
  return { nodes: result.nodes, edges: result.edges, dimmedNodeIds, dimmedEdgeIds };
}

/** toggleMember returns a new Set with member toggled — the immutable update the reducer
 * and the filter panel share. */
function toggleMember(set: ReadonlySet<string>, member: string): Set<string> {
  const next = new Set(set);
  if (next.has(member)) next.delete(member);
  else next.add(member);
  return next;
}

/** IntentState couples the wire GraphIntent with the active filter sets so the UI has
 * one source of truth; toClientIntent projects it back to the POST body. */
export interface IntentState {
  readonly op: GraphIntent['op'];
  readonly seedId: string;
  readonly session: string;
  readonly labels: ReadonlySet<string>;
  readonly relTypes: ReadonlySet<string>;
  readonly nodeCap: number;
  readonly edgeCap: number;
}

/** initialIntentState is the default open: a schema-overview-shaped empty seed with the
 * default caps (D-08 — the default open is never blank, the seed falls back server-side). */
export function initialIntentState(): IntentState {
  return {
    op: OP_SEED,
    seedId: '',
    session: '',
    labels: new Set<string>(),
    relTypes: new Set<string>(),
    nodeCap: DEFAULT_NODE_CAP,
    edgeCap: DEFAULT_EDGE_CAP,
  };
}

export type IntentAction =
  | { readonly kind: 'setSeed'; readonly session: string }
  | { readonly kind: 'expand'; readonly nodeId: string }
  | { readonly kind: 'toggleLabel'; readonly label: string }
  | { readonly kind: 'toggleRelType'; readonly relType: string };

/**
 * intentReducer is the pure state transition for the workspace: setSeed(session) →
 * op 'seed' for the active conversation footprint; expand(nodeId) → op 'expand' with
 * seed_id set; toggling a label/rel-type updates the active filter sets. Caps default to
 * 75/200 and are preserved across transitions.
 */
export function intentReducer(state: IntentState, action: IntentAction): IntentState {
  switch (action.kind) {
    case 'setSeed':
      return { ...state, op: OP_SEED, session: action.session, seedId: '' };
    case 'expand':
      return { ...state, op: OP_EXPAND, seedId: action.nodeId };
    case 'toggleLabel':
      return { ...state, labels: toggleMember(state.labels, action.label) };
    case 'toggleRelType':
      return { ...state, relTypes: toggleMember(state.relTypes, action.relType) };
  }
}

/** toClientIntent projects the UI state into the wire GraphIntent the POST body expects
 * (sorted filter arrays for a stable request signature / cache key). */
export function toClientIntent(state: IntentState): GraphIntent {
  return {
    op: state.op,
    seed_id: state.seedId,
    session: state.session,
    labels: [...state.labels].sort(),
    rel_types: [...state.relTypes].sort(),
    node_cap: state.nodeCap,
    edge_cap: state.edgeCap,
  };
}

/** degreeToSize maps a node's degree to a Sigma node radius — the NON-color channel that
 * keeps color from being the only encoding (WCAG 1.4.1 / D-03). A floor keeps isolated
 * nodes visible; growth is sub-linear so a hub never dwarfs the canvas. */
export function degreeToSize(degree: number): number {
  const MIN = 4;
  const SCALE = 2.2;
  return MIN + Math.sqrt(Math.max(0, degree)) * SCALE;
}

/**
 * rowsToClientGraph maps a contract GraphResult into a graphology-ready {nodes,edges}
 * (id/caption/color/size + source/target/label) with NO duplicate node ids. Color is the
 * schema-driven label-family fill; SIZE encodes degree (the non-color channel), so color
 * is never the only encoding. Edges referencing a missing node id are dropped (a dangling
 * edge would crash graphology.addEdge in plan 04).
 */
export function rowsToClientGraph(result: GraphResult): ClientGraph {
  const seen = new Set<string>();
  const nodes: ClientNode[] = [];
  for (const node of result.nodes) {
    if (seen.has(node.id)) continue; // de-dupe by id
    seen.add(node.id);
    const labels = node.labels ?? [];
    const color = labelFamilyColor(primaryLabel(labels), node.entity_type, result.schema);
    nodes.push({
      id: node.id,
      caption: node.caption ?? node.id,
      color,
      size: degreeToSize(node.degree ?? 0),
      labels,
      ...(node.entity_type !== undefined ? { entityType: node.entity_type } : {}),
    });
  }
  const edges: ClientEdge[] = [];
  for (const edge of result.edges) {
    if (!seen.has(edge.source) || !seen.has(edge.target)) continue; // drop dangling
    edges.push({
      id: edge.id,
      source: edge.source,
      target: edge.target,
      label: edge.rel_type ?? '',
    });
  }
  return { nodes, edges };
}
