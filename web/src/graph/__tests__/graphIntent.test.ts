import { afterEach, describe, expect, it, vi } from 'vitest';
import {
  applyFilters,
  BRAND_RAMP,
  DEFAULT_EDGE_CAP,
  DEFAULT_NODE_CAP,
  degreeToSize,
  EMPTY_FILTERS,
  type GraphFilters,
  type IntentState,
  initialIntentState,
  intentReducer,
  labelFamilyColor,
  mergeGraphResults,
  nodeDisplayName,
  rowsToClientGraph,
  toClientIntent,
} from '../graphIntent';
import { fetchGraphSchema, GRAPH_QUERY_PATH, GRAPH_SCHEMA_PATH, postGraphQuery } from '../graphApi';
import type { GraphResult, GraphSchema } from '../types';

// Exhaustive unit coverage for the PURE graph core (graphIntent.ts) + the credentialed
// fetch wrappers (graphApi.ts) — the sourceExplorerData.ts idiom: logic off the .tsx,
// directly mutation-tested without importing the browser renderer. The 401-reject cases pin
// requirement B3 / threat T-27-03.

const SCHEMA: GraphSchema = {
  labels: ['Document', 'Entity', 'Fact', 'ReasoningTrace', 'Topic', 'Conflict'],
  rel_types: ['MENTIONS', 'CITES'],
  entity_types: ['PERSON', 'ORGANIZATION', 'LOCATION'],
};

function filters(labels: string[], relTypes: string[]): GraphFilters {
  return { labels: new Set(labels), relTypes: new Set(relTypes) };
}

function urlOf(input: RequestInfo | URL): string {
  if (typeof input === 'string') return input;
  if (input instanceof URL) return input.href;
  return input.url;
}

function result(over: Partial<GraphResult>): GraphResult {
  return { nodes: [], edges: [], schema: SCHEMA, query: 'MATCH …', ...over };
}

describe('labelFamilyColor (schema-driven, deterministic — D-02)', () => {
  it('maps :Document to the source/evidence brand token, stable across calls', () => {
    const a = labelFamilyColor('Document', undefined, SCHEMA);
    const b = labelFamilyColor('Document', undefined, SCHEMA);
    expect(a).toBe(b);
    expect(a).toBe(BRAND_RAMP[0]); // --color-info evidence anchor
  });

  it('shifts :Entity color by Entity.type (PERSON vs ORGANIZATION) — second dimension', () => {
    const person = labelFamilyColor('Entity', 'PERSON', SCHEMA);
    const org = labelFamilyColor('Entity', 'ORGANIZATION', SCHEMA);
    expect(person).not.toBe(org);
    // both stay in the entity family hue band (adjacent ramp steps off the entity base)
    const personIdx = BRAND_RAMP.indexOf(person);
    const orgIdx = BRAND_RAMP.indexOf(org);
    expect(orgIdx).toBe(personIdx + 1);
  });

  it('falls back to the entity base when Entity.type is unknown/undefined', () => {
    const base = labelFamilyColor('Entity', undefined, SCHEMA);
    const unknownType = labelFamilyColor('Entity', 'NOT_A_POLE_TYPE', SCHEMA);
    expect(unknownType).toBe(base);
  });

  it('gives an unmapped label a deterministic hash→ramp color', () => {
    const a = labelFamilyColor('SomeUnmappedLabel', undefined, SCHEMA);
    const b = labelFamilyColor('SomeUnmappedLabel', undefined, SCHEMA);
    expect(a).toBe(b);
    expect(BRAND_RAMP).toContain(a);
  });

  it('generally maps two different unmapped labels to different ramp indices', () => {
    const a = labelFamilyColor('WeirdLabelOne', undefined, SCHEMA);
    const b = labelFamilyColor('CompletelyOtherLabel', undefined, SCHEMA);
    expect(a).not.toBe(b);
  });

  it('anchors an unmapped label present in the live schema by its sorted position', () => {
    // "Widget" is unmapped but IN the schema label set → schema-anchored (legend stable),
    // and still deterministic across calls.
    const schema: GraphSchema = { ...SCHEMA, labels: [...SCHEMA.labels, 'Widget'] };
    const a = labelFamilyColor('Widget', undefined, schema);
    const b = labelFamilyColor('Widget', undefined, schema);
    expect(a).toBe(b);
    expect(BRAND_RAMP).toContain(a);
  });
});

describe('applyFilters (dim/exclude — empty set = show all)', () => {
  const res = result({
    nodes: [
      { id: 'n1', labels: ['Document'] },
      { id: 'n2', labels: ['Entity'] },
      { id: 'n3', labels: ['Fact'] },
    ],
    edges: [
      { id: 'e1', source: 'n1', target: 'n2', rel_type: 'MENTIONS' },
      { id: 'e2', source: 'n2', target: 'n3', rel_type: 'CITES' },
    ],
  });

  it('treats an empty filter set as "show all" (nothing dimmed)', () => {
    const out = applyFilters(res, EMPTY_FILTERS);
    expect(out.dimmedNodeIds.size).toBe(0);
    expect(out.dimmedEdgeIds.size).toBe(0);
  });

  it('dims nodes whose label is not in the active label filter', () => {
    const out = applyFilters(res, filters(['Document'], []));
    expect(out.dimmedNodeIds.has('n1')).toBe(false);
    expect(out.dimmedNodeIds.has('n2')).toBe(true);
    expect(out.dimmedNodeIds.has('n3')).toBe(true);
  });

  it('dims edges whose rel_type is not in the active relType filter', () => {
    const out = applyFilters(res, filters([], ['MENTIONS']));
    expect(out.dimmedEdgeIds.has('e1')).toBe(false);
    expect(out.dimmedEdgeIds.has('e2')).toBe(true);
  });

  it('dims an edge when one of its endpoints is dimmed by the label filter', () => {
    const out = applyFilters(res, filters(['Document'], []));
    // e1 touches n2 (dimmed) and e2 touches n2+n3 (dimmed) → both dim
    expect(out.dimmedEdgeIds.has('e1')).toBe(true);
    expect(out.dimmedEdgeIds.has('e2')).toBe(true);
  });

  it('treats a label-less node and a rel_type-less edge under active filters', () => {
    const sparse = result({
      nodes: [
        { id: 'a' }, // no labels → dimmed under a non-empty label filter
        { id: 'b', labels: ['Document'] },
      ],
      edges: [{ id: 'e', source: 'b', target: 'b' }], // no rel_type
    });
    const out = applyFilters(sparse, filters(['Document'], ['MENTIONS']));
    expect(out.dimmedNodeIds.has('a')).toBe(true); // label-less → not in the filter
    expect(out.dimmedNodeIds.has('b')).toBe(false);
    expect(out.dimmedEdgeIds.has('e')).toBe(true); // missing rel_type ∉ {MENTIONS}
  });
});

describe('intentReducer (pure transitions)', () => {
  it('initialIntentState defaults to the per-identity overview with the 75/200 caps', () => {
    const s = initialIntentState();
    expect(s.op).toBe('overview');
    expect(s.nodeCap).toBe(DEFAULT_NODE_CAP);
    expect(s.edgeCap).toBe(DEFAULT_EDGE_CAP);
    expect(DEFAULT_NODE_CAP).toBe(75);
    expect(DEFAULT_EDGE_CAP).toBe(200);
  });

  it('refresh returns to the identity overview and clears expansion state', () => {
    const start: IntentState = { ...initialIntentState(), op: 'expand', nodeID: '#1:0' };
    const next = intentReducer(start, { kind: 'refresh' });
    expect(next.op).toBe('overview');
    expect(next.nodeID).toBe('');
  });

  it('expand(nodeId) sets op expand with node_id', () => {
    const next = intentReducer(initialIntentState(), { kind: 'expand', nodeId: '#1:7' });
    expect(next.op).toBe('expand');
    expect(next.nodeID).toBe('#1:7');
  });

  it('toggleLabel adds then removes a label (idempotent toggle)', () => {
    const on = intentReducer(initialIntentState(), { kind: 'toggleLabel', label: 'Entity' });
    expect(on.labels.has('Entity')).toBe(true);
    const off = intentReducer(on, { kind: 'toggleLabel', label: 'Entity' });
    expect(off.labels.has('Entity')).toBe(false);
  });

  it('toggleRelType updates the active rel-type set without mutating the input', () => {
    const start: IntentState = { ...initialIntentState(), op: 'expand', nodeID: '#1:0' };
    const next = intentReducer(start, { kind: 'toggleRelType', relType: 'CITES' });
    expect(next.relTypes.has('CITES')).toBe(true);
    expect(start.relTypes.has('CITES')).toBe(false); // immutability
    expect(next.op).toBe('overview');
    expect(next.nodeID).toBe('');
  });
});

describe('mergeGraphResults (ArcadeDB Studio-style cumulative expansion)', () => {
  it('appends unseen RIDs, preserves existing records, and de-duplicates edges', () => {
    const current = result({
      nodes: [{ id: '#1:0', caption: 'Aura', labels: ['Entity'] }],
      edges: [],
      query: 'overview',
    });
    const expansion = result({
      nodes: [
        { id: '#1:0', caption: 'duplicate', labels: ['Entity'] },
        { id: '#1:1', caption: 'ArcadeDB', labels: ['Entity'] },
      ],
      edges: [
        { id: '#5:0', source: '#1:0', target: '#1:1', rel_type: 'FACT', caption: 'stores_in' },
        { id: '#5:0', source: '#1:0', target: '#1:1', rel_type: 'FACT' },
      ],
      query: 'expand',
    });

    const merged = mergeGraphResults(current, expansion);
    expect(merged.nodes.map((node) => node.caption)).toEqual(['Aura', 'ArcadeDB']);
    expect(merged.edges).toHaveLength(1);
    expect(merged.query).toBe('expand');
  });
});

describe('toClientIntent (UI state → wire intent)', () => {
  it('projects sorted filter arrays + caps into the POST body', () => {
    const state: IntentState = {
      ...initialIntentState(),
      op: 'expand',
      nodeID: '#1:1',
      labels: new Set(['Entity', 'Document']),
      relTypes: new Set(['MENTIONS']),
    };
    const intent = toClientIntent(state);
    expect(intent.op).toBe('expand');
    expect(intent.node_id).toBe('#1:1');
    expect(intent.labels).toEqual(['Document', 'Entity']); // sorted
    expect(intent.rel_types).toEqual(['MENTIONS']);
    expect(intent.node_cap).toBe(75);
    expect(intent.edge_cap).toBe(200);
  });

  it('omits node_id from an overview request', () => {
    expect(toClientIntent(initialIntentState())).not.toHaveProperty('node_id');
  });
});

describe('degreeToSize (non-color encoding channel)', () => {
  it('floors isolated nodes and grows sub-linearly with degree', () => {
    expect(degreeToSize(0)).toBe(4);
    expect(degreeToSize(-5)).toBe(4); // negative guarded to the floor
    expect(degreeToSize(4)).toBeGreaterThan(degreeToSize(0));
    expect(degreeToSize(16)).toBeGreaterThan(degreeToSize(4));
  });
});

describe('nodeDisplayName (the one place a node becomes readable text)', () => {
  it('prefers the caption, then the label, and only then the raw id', () => {
    expect(nodeDisplayName({ id: 'x', caption: 'Davide', labels: ['Entity'] })).toBe('Davide');
    expect(nodeDisplayName({ id: 'x', labels: ['Fact'] })).toBe('Fact');
    // A whitespace-only caption is as unreadable as none at all.
    expect(nodeDisplayName({ id: 'x', caption: '   ', labels: ['Preference'] })).toBe('Preference');
    // Nothing left to show: the id is the last resort, not the default.
    expect(nodeDisplayName({ id: '4:abc:21' })).toBe('4:abc:21');
  });
});

describe('rowsToClientGraph (contract → renderer-ready)', () => {
  it('maps nodes with id/caption/color/size and de-dupes by id', () => {
    const res = result({
      nodes: [
        { id: 'n1', caption: 'Doc A', labels: ['Document'], degree: 2 },
        { id: 'n1', caption: 'dup', labels: ['Document'] }, // duplicate id
        { id: 'n2', labels: ['Entity'], entity_type: 'PERSON', degree: 0 },
      ],
      edges: [
        { id: 'e1', source: 'n1', target: 'n2', rel_type: 'MENTIONS', caption: 'mentions_in' },
      ],
    });
    const out = rowsToClientGraph(res);
    expect(out.nodes.map((n) => n.id)).toEqual(['n1', 'n2']); // no dup
    const n1 = out.nodes.find((n) => n.id === 'n1');
    expect(n1?.caption).toBe('Doc A');
    expect(n1?.color).toBe(BRAND_RAMP[0]);
    expect(n1?.size).toBeGreaterThan(4); // degree 2 → larger than floor
    const n2 = out.nodes.find((n) => n.id === 'n2');
    // A caption-less node shows its LABEL. This assertion previously demanded the node id,
    // which is a raw graph record id: the workspace printed screens of
    // "4:4260efd2-fa44-4d3b-b1a9-8ccf7886ae88:21" because every node that was not an Entity
    // arrived without a caption. The old expectation encoded that behaviour as correct.
    expect(n2?.caption).toBe('Entity');
    expect(n2?.entityType).toBe('PERSON');
    expect(out.edges).toEqual([
      {
        id: 'e1',
        source: 'n1',
        target: 'n2',
        label: 'mentions_in',
        color: labelFamilyColor('MENTIONS', undefined, res.schema),
      },
    ]);
  });

  it('drops an edge that references a missing node id', () => {
    const res = result({
      nodes: [{ id: 'n1', labels: ['Document'] }],
      edges: [{ id: 'e-bad', source: 'n1', target: 'ghost', rel_type: 'CITES' }],
    });
    const out = rowsToClientGraph(res);
    expect(out.edges).toHaveLength(0);
  });

  it('handles a node with no labels and an edge with no rel_type', () => {
    const res = result({
      nodes: [{ id: 'x' }, { id: 'y' }], // no labels on either
      edges: [{ id: 'e', source: 'x', target: 'y' }], // no rel_type
    });
    const out = rowsToClientGraph(res);
    const x = out.nodes.find((n) => n.id === 'x');
    expect(x?.labels).toEqual([]); // ?? [] fallback
    expect(BRAND_RAMP).toContain(x?.color); // empty primaryLabel still gets a ramp color
    expect(out.edges[0]?.label).toBe(''); // ?? '' fallback for a missing rel_type
  });
});

describe('graphApi credentialed wrappers (same-origin; non-200 throws)', () => {
  afterEach(() => {
    vi.unstubAllGlobals();
  });

  it('fetchGraphSchema GETs same-origin and parses the typed schema', async () => {
    const fetchMock = vi.fn(() =>
      Promise.resolve(new Response(JSON.stringify(SCHEMA), { status: 200 })),
    );
    vi.stubGlobal('fetch', fetchMock);
    const schema = await fetchGraphSchema();
    expect(schema.labels).toContain('Entity');
    expect(fetchMock).toHaveBeenCalledWith(GRAPH_SCHEMA_PATH, {
      headers: { Accept: 'application/json' },
      credentials: 'same-origin',
    });
  });

  it('postGraphQuery POSTs the structured intent JSON same-origin and parses the result', async () => {
    let captured: { url: string; init?: RequestInit } | undefined;
    const res = result({ nodes: [{ id: 'n1', labels: ['Document'] }] });
    const fetchMock = vi.fn((input: RequestInfo | URL, init?: RequestInit) => {
      captured = { url: urlOf(input), ...(init ? { init } : {}) };
      return Promise.resolve(new Response(JSON.stringify(res), { status: 200 }));
    });
    vi.stubGlobal('fetch', fetchMock);
    const out = await postGraphQuery({ op: 'overview' });
    expect(out.nodes).toHaveLength(1);
    expect(captured?.url).toBe(GRAPH_QUERY_PATH);
    expect(captured?.init?.method).toBe('POST');
    expect(captured?.init?.credentials).toBe('same-origin');
    expect(captured?.init?.body).toBe(JSON.stringify({ op: 'overview' }));
  });

  // B3 / T-27-03: a 401 must REJECT so the consumer surfaces a visible auth-error state,
  // never a silent blank canvas.
  it('postGraphQuery rejects on 401', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn(() => Promise.resolve(new Response('unauthorized', { status: 401 }))),
    );
    await expect(postGraphQuery({ op: 'overview' })).rejects.toThrow('HTTP 401');
  });

  it('fetchGraphSchema rejects on 401', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn(() => Promise.resolve(new Response('unauthorized', { status: 401 }))),
    );
    await expect(fetchGraphSchema()).rejects.toThrow('HTTP 401');
  });
});
