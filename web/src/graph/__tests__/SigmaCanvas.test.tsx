import { describe, expect, it, vi } from 'vitest';
import { render, screen } from '@testing-library/react';
import '../../i18n/i18n'; // side-effect: initialise i18next so t() resolves keys
import type { ClientEdge, ClientNode } from '../types';

// jsdom has no WebGL (Pitfall 4), so @react-sigma/core + the renderer stack are MOCKED:
// the container renders its children (so GraphLoader/EventHandler/HighlightManager mount and
// their hooks are exercised against the fakes) and the hooks return no-op shims. The REAL
// WebGL render + interaction is asserted in the Task-4 Playwright e2e, never here.
const loadGraph = vi.fn();
const registerEvents = vi.fn();
const sigmaShim = {
  getContainer: () => ({ style: {} }),
  setSetting: vi.fn(),
  refresh: vi.fn(),
  getGraph: () => ({ source: () => 's', target: () => 't' }),
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
const EDGES: readonly ClientEdge[] = [{ id: 'e1', source: 'n1', target: 'n2', label: 'MENTIONS' }];

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

  it('registers node events so a canvas click can open the inspector (non-hover path wiring)', () => {
    registerEvents.mockClear();
    render(
      <SigmaCanvas
        nodes={NODES}
        edges={EDGES}
        pinnedPath={new Set(['n1', 'n2'])}
        sigmaKey={1}
        onNodeClick={vi.fn()}
      />,
    );
    expect(registerEvents).toHaveBeenCalled();
    const handlers = registerEvents.mock.calls[0]?.[0] as { clickNode?: unknown };
    expect(typeof handlers.clickNode).toBe('function');
  });
});
