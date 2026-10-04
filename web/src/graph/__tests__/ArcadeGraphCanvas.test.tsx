import { beforeEach, describe, expect, it, vi } from 'vitest';
import { act, render, screen } from '@testing-library/react';
import '../../i18n/i18n';
import type { ClientEdge, ClientNode } from '../types';

const mocks = vi.hoisted(() => {
  const edge = {
    source: vi.fn(() => ({ id: () => 'n1' })),
    target: vi.fn(() => ({ id: () => 'n2' })),
    removeClass: vi.fn(),
    addClass: vi.fn(),
  };
  edge.removeClass.mockReturnValue(edge);
  edge.addClass.mockReturnValue(edge);
  // What the canvas currently draws: node ids, and every element id.
  const shown = { nodes: [] as string[], elements: [] as string[] };
  const collection = {
    length: 2,
    map: vi.fn(<T,>(callback: (item: { id: () => string }) => T): T[] =>
      shown.elements.map((id) => callback({ id: () => id })),
    ),
    nonempty: vi.fn(() => true),
    position: vi.fn(() => ({ x: 40, y: 50 })),
    remove: vi.fn(),
    removeClass: vi.fn(),
    addClass: vi.fn(),
    forEach: vi.fn<(callback: (item: typeof edge) => void) => void>((callback) => {
      callback(edge);
    }),
  };
  collection.removeClass.mockReturnValue(collection);
  collection.addClass.mockReturnValue(collection);
  const nodeList = {
    length: 2,
    map: vi.fn(
      <T,>(
        callback: (item: { id: () => string; position: () => { x: number; y: number } }) => T,
      ): T[] =>
        shown.nodes.map((id) => callback({ id: () => id, position: () => ({ x: 1, y: 2 }) })),
    ),
    boundingBox: vi.fn(() => ({ x1: 0, x2: 100, y1: 0, y2: 60 })),
    removeClass: vi.fn(),
    addClass: vi.fn(),
  };
  nodeList.removeClass.mockReturnValue(nodeList);
  nodeList.addClass.mockReturnValue(nodeList);
  const layout = { run: vi.fn(), stop: vi.fn() };
  const core = {
    on: vi.fn(),
    resize: vi.fn(),
    fit: vi.fn(),
    destroy: vi.fn(),
    startBatch: vi.fn(),
    endBatch: vi.fn(),
    add: vi.fn<(elements: readonly unknown[]) => void>(),
    layout: vi.fn(() => layout),
    elements: vi.fn(() => collection),
    nodes: vi.fn(() => nodeList),
    animate: vi.fn(),
    collection: vi.fn(() => ({ ...collection, nonempty: () => false })),
    edges: vi.fn(() => collection),
    getElementById: vi.fn(() => collection),
  };
  const factory = vi.fn<(options: Record<string, unknown>) => typeof core>(() => core);
  const use = vi.fn();
  return { collection, core, edge, factory, layout, nodeList, shown, use };
});

vi.mock('cytoscape', () => ({
  default: Object.assign(mocks.factory, { use: mocks.use }),
}));
vi.mock('cytoscape-fcose', () => ({ default: vi.fn() }));

class ResizeObserverStub {
  static instances: ResizeObserverStub[] = [];

  constructor(readonly callback: () => void) {
    ResizeObserverStub.instances.push(this);
  }

  observe = vi.fn();
  disconnect = vi.fn();
}

const { ArcadeGraphCanvas } = await import('../ArcadeGraphCanvas');

const NODES: readonly ClientNode[] = [
  { id: 'n1', caption: 'ArcadeDB', color: '#7FC9C3', size: 8, labels: ['Entity'] },
  { id: 'n2', caption: '5889', color: '#AECBFA', size: 6, labels: ['Entity'] },
];
const EDGES: readonly ClientEdge[] = [
  { id: 'e1', source: 'n1', target: 'n2', label: 'FACT', color: '#FDD663' },
];

beforeEach(() => {
  vi.clearAllMocks();
  mocks.collection.length = 2;
  mocks.nodeList.length = 2;
  mocks.shown.nodes = [];
  mocks.shown.elements = [];
  ResizeObserverStub.instances = [];
  vi.unstubAllGlobals();
  vi.stubGlobal('ResizeObserver', ResizeObserverStub);
});

describe('ArcadeGraphCanvas', () => {
  it('creates the Studio renderer, runs fCoSE and exposes an accessible graph name', () => {
    render(
      <ArcadeGraphCanvas
        nodes={NODES}
        edges={EDGES}
        pinnedPath={new Set()}
        onNodeClick={vi.fn()}
        onNodeDoubleClick={vi.fn()}
        focusId={undefined}
      />,
    );

    expect(screen.getByRole('img').getAttribute('aria-label')).toContain('2');
    expect(screen.getByTestId('arcade-graph-canvas')).toBeTruthy();
    expect(mocks.factory).toHaveBeenCalledTimes(1);
    expect(Object.hasOwn(mocks.factory.mock.calls[0]?.[0] ?? {}, 'wheelSensitivity')).toBe(false);
    expect(mocks.core.add.mock.calls[0]?.[0]).toHaveLength(3);
    expect(mocks.core.layout).toHaveBeenCalledWith(
      expect.objectContaining({ name: 'fcose', nodeSeparation: 50, idealEdgeLength: 75 }),
    );
    expect(mocks.layout.run).toHaveBeenCalledTimes(1);
  });

  it('routes node taps to the inspector callback and applies path classes', () => {
    const onNodeClick = vi.fn();
    const { rerender } = render(
      <ArcadeGraphCanvas
        nodes={NODES}
        edges={EDGES}
        pinnedPath={new Set()}
        onNodeClick={onNodeClick}
        onNodeDoubleClick={vi.fn()}
        focusId={undefined}
      />,
    );
    const tapHandler = mocks.core.on.mock.calls[0]?.[2] as
      ((event: { target: { id: () => string } }) => void) | undefined;
    act(() => tapHandler?.({ target: { id: () => 'n1' } }));
    expect(onNodeClick).toHaveBeenCalledWith('n1');

    rerender(
      <ArcadeGraphCanvas
        nodes={NODES}
        edges={EDGES}
        pinnedPath={new Set(['n1', 'n2'])}
        onNodeClick={onNodeClick}
        onNodeDoubleClick={vi.fn()}
        focusId={undefined}
      />,
    );
    expect(mocks.collection.addClass).toHaveBeenCalledWith('dimmed');
    expect(mocks.collection.addClass).toHaveBeenCalledWith('path-node');
    expect(mocks.edge.addClass).toHaveBeenCalledWith('path-edge');
  });

  it('uses reduced-motion layout settings and handles an empty graph without layout work', () => {
    vi.stubGlobal(
      'matchMedia',
      vi.fn(() => ({ matches: true })),
    );
    const { rerender } = render(
      <ArcadeGraphCanvas
        nodes={NODES}
        edges={EDGES}
        pinnedPath={new Set(['n1'])}
        onNodeClick={vi.fn()}
        onNodeDoubleClick={vi.fn()}
        focusId={undefined}
      />,
    );
    expect(mocks.core.layout).toHaveBeenCalledWith(
      expect.objectContaining({ animate: false, animationDuration: 0 }),
    );
    expect(mocks.edge.addClass).not.toHaveBeenCalledWith('path-edge');

    mocks.core.layout.mockClear();
    rerender(
      <ArcadeGraphCanvas
        nodes={[]}
        edges={[]}
        pinnedPath={new Set()}
        onNodeClick={vi.fn()}
        onNodeDoubleClick={vi.fn()}
        focusId={undefined}
      />,
    );
    expect(mocks.layout.stop).toHaveBeenCalled();
    expect(mocks.core.layout).not.toHaveBeenCalled();
  });

  it('resizes, refits and tears down the renderer', () => {
    const { unmount } = render(
      <ArcadeGraphCanvas
        nodes={NODES}
        edges={EDGES}
        pinnedPath={new Set()}
        onNodeClick={vi.fn()}
        onNodeDoubleClick={vi.fn()}
        focusId={undefined}
      />,
    );
    const observer = ResizeObserverStub.instances[0];
    act(() => observer?.callback());
    expect(mocks.core.resize).toHaveBeenCalled();
    expect(mocks.core.fit).toHaveBeenCalled();

    mocks.nodeList.length = 0;
    mocks.core.fit.mockClear();
    act(() => observer?.callback());
    expect(mocks.core.fit).not.toHaveBeenCalled();

    unmount();
    expect(observer?.disconnect).toHaveBeenCalled();
    expect(mocks.layout.stop).toHaveBeenCalled();
    expect(mocks.core.destroy).toHaveBeenCalled();
  });

  it('renders the accessible fallback when Cytoscape initialization fails', () => {
    vi.useFakeTimers();
    const warning = vi.spyOn(console, 'warn').mockImplementation(() => undefined);
    mocks.factory.mockImplementationOnce(() => {
      throw new Error('canvas unavailable');
    });

    render(
      <ArcadeGraphCanvas
        nodes={NODES}
        edges={EDGES}
        pinnedPath={new Set()}
        onNodeClick={vi.fn()}
        onNodeDoubleClick={vi.fn()}
        focusId={undefined}
      />,
    );
    act(() => {
      vi.runAllTimers();
    });
    expect(screen.getByRole('status')).toBeTruthy();
    expect(warning).toHaveBeenCalledWith('ArcadeGraphCanvas render error', 'canvas unavailable');

    warning.mockRestore();
    vi.useRealTimers();
  });
  it('routes a double click on a node to the expand callback', () => {
    const onNodeDoubleClick = vi.fn();
    render(
      <ArcadeGraphCanvas
        nodes={NODES}
        edges={EDGES}
        pinnedPath={new Set()}
        onNodeClick={vi.fn()}
        onNodeDoubleClick={onNodeDoubleClick}
        focusId={undefined}
      />,
    );
    const registration = mocks.core.on.mock.calls.find((call) => call[0] === 'dbltap');
    const handler = registration?.[2] as
      ((event: { target: { id: () => string } }) => void) | undefined;
    act(() => handler?.({ target: { id: () => 'n2' } }));
    expect(onNodeDoubleClick).toHaveBeenCalledWith('n2');
  });

  // An expansion keeps every node where the reader left it and adds only what is new, laid out
  // around the expanded node.
  it('grows the graph from the expanded node, keeping the drawn nodes fixed', () => {
    const { rerender } = render(
      <ArcadeGraphCanvas
        nodes={NODES}
        edges={EDGES}
        pinnedPath={new Set()}
        onNodeClick={vi.fn()}
        onNodeDoubleClick={vi.fn()}
        focusId={undefined}
      />,
    );
    mocks.shown.nodes = ['n1', 'n2'];
    mocks.shown.elements = ['n1', 'n2', 'e1'];
    mocks.core.add.mockClear();
    mocks.core.layout.mockClear();
    mocks.collection.remove.mockClear();
    const grown: readonly ClientNode[] = [
      ...NODES,
      { id: 'n3', caption: 'Giulia', color: '#7FC9C3', size: 6, labels: ['Person'] },
    ];
    rerender(
      <ArcadeGraphCanvas
        nodes={grown}
        edges={[
          ...EDGES,
          { id: 'e2', source: 'n1', target: 'n3', label: 'FACT', color: '#FDD663' },
        ]}
        pinnedPath={new Set()}
        onNodeClick={vi.fn()}
        onNodeDoubleClick={vi.fn()}
        focusId="n1"
      />,
    );
    const added = mocks.core.add.mock.calls[0]?.[0] as {
      data: { id: string };
      position?: unknown;
    }[];
    expect(added.map((element) => element.data.id)).toEqual(['n3', 'e2']);
    expect(added[0]?.position).toEqual({ x: 40, y: 50 });
    expect(mocks.collection.remove).not.toHaveBeenCalled();
    expect(mocks.core.layout).toHaveBeenCalledWith(
      expect.objectContaining({
        randomize: false,
        quality: 'proof',
        fit: false,
        fixedNodeConstraint: [
          { nodeId: 'n1', position: { x: 1, y: 2 } },
          { nodeId: 'n2', position: { x: 1, y: 2 } },
        ],
      }),
    );
    expect(mocks.core.animate).toHaveBeenCalledWith(
      expect.objectContaining({ center: expect.anything() as unknown }),
      expect.anything(),
    );
  });

  // Selecting a node re-renders the page with the same graph; that must not move anything.
  it('leaves the canvas alone when the graph has not changed', () => {
    const { rerender } = render(
      <ArcadeGraphCanvas
        nodes={NODES}
        edges={EDGES}
        pinnedPath={new Set()}
        onNodeClick={vi.fn()}
        onNodeDoubleClick={vi.fn()}
        focusId={undefined}
      />,
    );
    mocks.shown.nodes = ['n1', 'n2'];
    mocks.shown.elements = ['n1', 'n2', 'e1'];
    mocks.core.add.mockClear();
    mocks.core.layout.mockClear();
    rerender(
      <ArcadeGraphCanvas
        nodes={[...NODES]}
        edges={[...EDGES]}
        pinnedPath={new Set()}
        onNodeClick={vi.fn()}
        onNodeDoubleClick={vi.fn()}
        focusId={undefined}
      />,
    );
    expect(mocks.core.add).not.toHaveBeenCalled();
    expect(mocks.core.layout).not.toHaveBeenCalled();
  });
});
