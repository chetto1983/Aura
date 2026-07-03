import { beforeEach, describe, expect, it, vi } from 'vitest';
import { render, screen } from '@testing-library/react';
import '../../i18n/i18n'; // side-effect: initialise i18next so t() resolves keys
import type { ClientEdge, ClientNode } from '../types';

// jsdom has no WebGL (Pitfall 4), so @react-sigma/core + the renderer stack are MOCKED:
// the container renders its children (so GraphLoader/EventHandler/HighlightManager mount and
// their hooks are exercised against the fakes) and the hooks return no-op shims. The REAL
// WebGL render + interaction is asserted in the Task-4 Playwright e2e, never here.
const loadGraph = vi.fn();
const registerEvents = vi.fn();
const cameraSetState = vi.fn();
const setCustomBBox = vi.fn();
const refresh = vi.fn();
const graphNodes = [
  ['n1', { x: 10, y: 20 }],
  ['n2', { x: 110, y: 220 }],
] as const;
const sigmaShim = {
  getContainer: () => ({ style: {} }),
  setSetting: vi.fn(),
  refresh,
  getGraph: () => ({
    source: () => 's',
    target: () => 't',
    forEachNode: (cb: (id: string, attrs: { x: number; y: number }) => void) => {
      for (const [id, attrs] of graphNodes) cb(id, attrs);
    },
  }),
  getCamera: () => ({ setState: cameraSetState }),
  setCustomBBox,
};

vi.mock('@react-sigma/core', () => ({
  SigmaContainer: ({ children }: { children: React.ReactNode }) => (
    <div data-testid="sigma-container">{children}</div>
  ),
  useLoadGraph: () => loadGraph,
  useRegisterEvents: () => registerEvents,
  useSigma: () => sigmaShim,
}));
vi.mock('@react-sigma/core/lib/style.css', () => ({}));

// Import AFTER the mock is registered so SigmaCanvas binds to the fakes.
const { SigmaCanvas } = await import('../SigmaCanvas');

const NODES: readonly ClientNode[] = [
  { id: 'n1', caption: 'Alpha', color: '#AECBFA', size: 8, labels: ['Entity'] },
  { id: 'n2', caption: 'Bravo', color: '#81C995', size: 6, labels: ['Document'] },
];
const TECHNICAL_NODES: readonly ClientNode[] = [
  {
    id: 'n1',
    caption: '4:21de867b-135a-43b0-9598-b82a63a99e7d:d:63',
    color: '#AECBFA',
    size: 8,
    labels: ['Chunk'],
  },
  { id: 'n2', caption: 'web_search', color: '#81C995', size: 6, labels: ['ToolSelectionExample'] },
];
const DENSE_NODES: readonly ClientNode[] = Array.from({ length: 33 }, (_, index) => ({
  id: `n${String(index)}`,
  caption: `4:21de867b-135a-43b0-9598-b82a63a99e7d:d:${String(index)}`,
  color: '#AECBFA',
  size: 6,
  labels: ['Chunk'],
}));
const EDGES: readonly ClientEdge[] = [{ id: 'e1', source: 'n1', target: 'n2', label: 'MENTIONS' }];
const PARALLEL_EDGES: readonly ClientEdge[] = [
  { id: 'e1', source: 'n1', target: 'n2', label: 'MENTIONS' },
  { id: 'e2', source: 'n1', target: 'n2', label: 'CITES' },
];

beforeEach(() => {
  loadGraph.mockClear();
  registerEvents.mockClear();
  sigmaShim.setSetting.mockClear();
  setCustomBBox.mockClear();
  cameraSetState.mockClear();
  refresh.mockClear();
  vi.stubGlobal('requestAnimationFrame', (cb: FrameRequestCallback) => {
    cb(0);
    return 1;
  });
  vi.stubGlobal('cancelAnimationFrame', vi.fn());
});

describe('SigmaCanvas (renderer mocked — no WebGL in jsdom)', () => {
  it('gives the canvas wrapper role="img" + the accessible name with node/edge counts', () => {
    render(
      <SigmaCanvas
        nodes={NODES}
        edges={EDGES}
        pinnedPath={new Set()}
        sigmaKey={0}
        onNodeClick={vi.fn()}
      />,
    );
    const canvas = screen.getByRole('img');
    expect(canvas).toBeTruthy();
    // graph.a11y.canvasName = "Evidence graph: {{nodeCount}} nodes, {{edgeCount}} connections"
    expect(canvas.getAttribute('aria-label')).toContain('2');
    expect(canvas.getAttribute('aria-label')).toContain('1');
  });

  it('mounts the SigmaContainer and flows node/edge props into the loader (loadGraph called)', () => {
    loadGraph.mockClear();
    render(
      <SigmaCanvas
        nodes={NODES}
        edges={EDGES}
        pinnedPath={new Set()}
        sigmaKey={0}
        onNodeClick={vi.fn()}
      />,
    );
    expect(screen.getByTestId('sigma-container')).toBeTruthy();
    // GraphLoader's useEffect built the graphology Graph from the props and loaded it.
    expect(loadGraph).toHaveBeenCalledTimes(1);
    const graph = loadGraph.mock.calls[0]?.[0] as { order: number; size: number };
    expect(graph.order).toBe(2); // both nodes flowed in
    expect(graph.size).toBe(1); // the one edge flowed in
  });

  it('loads parallel Neo4j relationships without crashing the graphology renderer', () => {
    render(
      <SigmaCanvas
        nodes={NODES}
        edges={PARALLEL_EDGES}
        pinnedPath={new Set()}
        sigmaKey={0}
        onNodeClick={vi.fn()}
      />,
    );

    const graph = loadGraph.mock.calls[0]?.[0] as { order: number; size: number };
    expect(graph.order).toBe(2);
    expect(graph.size).toBe(2);
  });

  it('uses compact canvas labels for technical captions while preserving human labels', () => {
    render(
      <SigmaCanvas
        nodes={TECHNICAL_NODES}
        edges={EDGES}
        pinnedPath={new Set()}
        sigmaKey={0}
        onNodeClick={vi.fn()}
      />,
    );

    const graph = loadGraph.mock.calls[0]?.[0] as {
      getNodeAttribute: (nodeId: string, key: string) => unknown;
    };
    expect(graph.getNodeAttribute('n1', 'label')).toBe('d:63');
    expect(graph.getNodeAttribute('n1', 'canvasLabel')).toBe('d:63');
    expect(graph.getNodeAttribute('n2', 'label')).toBe('web_search');
  });

  it('hides ambient labels for dense overviews but keeps compact labels for focus restore', () => {
    render(
      <SigmaCanvas
        nodes={DENSE_NODES}
        edges={[]}
        pinnedPath={new Set()}
        sigmaKey={0}
        onNodeClick={vi.fn()}
      />,
    );

    const graph = loadGraph.mock.calls[0]?.[0] as {
      getNodeAttribute: (nodeId: string, key: string) => unknown;
    };
    expect(graph.getNodeAttribute('n0', 'label')).toBeNull();
    expect(graph.getNodeAttribute('n0', 'canvasLabel')).toBe('d:0');
  });

  it('registers node events so a canvas click can open the inspector (non-hover path wiring)', () => {
    registerEvents.mockClear();
    const onNodeClick = vi.fn();
    render(
      <SigmaCanvas
        nodes={NODES}
        edges={EDGES}
        pinnedPath={new Set(['n1', 'n2'])}
        sigmaKey={1}
        onNodeClick={onNodeClick}
      />,
    );
    expect(registerEvents).toHaveBeenCalled();
    const handlers = registerEvents.mock.calls[0]?.[0] as {
      clickNode?: (p: { node: string }) => void;
      enterNode?: () => void;
      leaveNode?: () => void;
    };
    expect(typeof handlers.clickNode).toBe('function');
    // clickNode forwards the node id to the inspector opener; enter/leave toggle the cursor.
    handlers.clickNode?.({ node: 'n1' });
    expect(onNodeClick).toHaveBeenCalledWith('n1');
    handlers.enterNode?.();
    handlers.leaveNode?.();
  });

  it('installs the pinned-path reducers on the live sigma instance (HighlightManager)', () => {
    sigmaShim.setSetting.mockClear();
    render(
      <SigmaCanvas
        nodes={NODES}
        edges={EDGES}
        pinnedPath={new Set(['n1', 'n2'])}
        sigmaKey={2}
        onNodeClick={vi.fn()}
      />,
    );
    // HighlightManager set both the node + edge reducers and refreshed.
    expect(sigmaShim.setSetting).toHaveBeenCalledWith('nodeReducer', expect.any(Function));
    expect(sigmaShim.setSetting).toHaveBeenCalledWith('edgeReducer', expect.any(Function));
    // Exercise the installed edge reducer (covers the getGraph/source/target path).
    const edgeReducer = sigmaShim.setSetting.mock.calls.find(
      (c) => c[0] === 'edgeReducer',
    )?.[1] as (id: string, data: Record<string, unknown>) => Record<string, unknown>;
    expect(edgeReducer('e1', { color: '#000' })).toBeTruthy();
  });

  it('pads the graph bounds before fitting the camera so endpoint nodes are not clipped', () => {
    render(
      <SigmaCanvas
        nodes={NODES}
        edges={EDGES}
        pinnedPath={new Set()}
        sigmaKey={3}
        onNodeClick={vi.fn()}
      />,
    );

    expect(setCustomBBox).toHaveBeenCalledWith({
      x: [-30, 150],
      y: [-20, 260],
    });
    expect(cameraSetState).toHaveBeenCalledWith({ x: 0.5, y: 0.5, angle: 0, ratio: 1 });
  });
});
