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
  const collection = {
    length: 2,
    remove: vi.fn(),
    removeClass: vi.fn(),
    addClass: vi.fn(),
    forEach: vi.fn<(callback: (item: typeof edge) => void) => void>((callback) => {
      callback(edge);
    }),
  };
  collection.removeClass.mockReturnValue(collection);
  collection.addClass.mockReturnValue(collection);
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
    nodes: vi.fn(() => collection),
    edges: vi.fn(() => collection),
    getElementById: vi.fn(() => collection),
  };
  const factory = vi.fn<(options: Record<string, unknown>) => typeof core>(() => core);
  const use = vi.fn();
  return { collection, core, edge, factory, layout, use };
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
      />,
    );
    expect(mocks.core.layout).toHaveBeenCalledWith(
      expect.objectContaining({ animate: false, animationDuration: 0 }),
    );
    expect(mocks.edge.addClass).not.toHaveBeenCalledWith('path-edge');

    mocks.core.layout.mockClear();
    rerender(
      <ArcadeGraphCanvas nodes={[]} edges={[]} pinnedPath={new Set()} onNodeClick={vi.fn()} />,
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
      />,
    );
    const observer = ResizeObserverStub.instances[0];
    act(() => observer?.callback());
    expect(mocks.core.resize).toHaveBeenCalled();
    expect(mocks.core.fit).toHaveBeenCalled();

    mocks.collection.length = 0;
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
});
