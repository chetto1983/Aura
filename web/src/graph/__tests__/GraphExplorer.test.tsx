import { describe, expect, it, vi, beforeEach } from 'vitest';
import { fireEvent, render, screen, waitFor, within } from '@testing-library/react';
import '../../i18n/i18n'; // side-effect: initialise i18next so t() resolves keys
import type { GraphResult, GraphSchema } from '../types';

// GraphExplorer is the lazy three-pane shell. SigmaCanvas is mocked (no WebGL in jsdom,
// Pitfall 4) and graphApi is mocked so we drive the overview→schema fallback and the
// 401 → visible-auth-error path (the unit half of B3 / T-27-03; the full browser run is the
// Task-4 Playwright spec). These are the GraphExplorer behavior obligations from the plan.

const postGraphQuery = vi.fn();
const fetchGraphSchema = vi.fn();

vi.mock('../graphApi', () => ({
  postGraphQuery: (...args: unknown[]) => postGraphQuery(...args) as Promise<GraphResult>,
  fetchGraphSchema: (...args: unknown[]) => fetchGraphSchema(...args) as Promise<GraphSchema>,
}));

// Mock the WebGL renderer — assert ONLY that node/edge props arrive (the real render is e2e).
vi.mock('../SigmaCanvas', () => ({
  SigmaCanvas: ({ nodes, edges }: { nodes: readonly unknown[]; edges: readonly unknown[] }) => (
    <div data-testid="sigma-mock">
      canvas:{nodes.length}:{edges.length}
    </div>
  ),
}));

const { default: GraphExplorer } = await import('../GraphExplorer');

const EMPTY_RESULT: GraphResult = {
  nodes: [],
  edges: [],
  schema: { labels: [], rel_types: [] },
  query: 'MATCH ...',
};

const POPULATED_RESULT: GraphResult = {
  nodes: [
    { id: '#1:0', caption: 'Alpha', labels: ['Entity'], degree: 2 },
    { id: '#1:1', caption: 'Bravo', labels: ['Entity'], degree: 1 },
  ],
  edges: [{ id: '#5:0', source: '#1:0', target: '#1:1', rel_type: 'FACT' }],
  schema: { labels: ['Entity'], rel_types: ['FACT'] },
  query: 'SELECT FROM `FACT` LIMIT 200',
};

const SCHEMA: GraphSchema = { labels: ['Entity'], rel_types: ['FACT'] };

describe('GraphExplorer (renderer + graphApi mocked)', () => {
  beforeEach(() => {
    postGraphQuery.mockReset();
    fetchGraphSchema.mockReset();
  });

  it('loads the identity memory overview and falls back to schema on an empty result', async () => {
    postGraphQuery.mockResolvedValue(EMPTY_RESULT);
    fetchGraphSchema.mockResolvedValue(SCHEMA);

    render(<GraphExplorer />);

    // An empty overview triggers the schema-backed empty state, never a blank canvas.
    expect(await screen.findByRole('region', { name: 'Memory graph readiness' })).toBeTruthy();
    expect(postGraphQuery).toHaveBeenCalledWith(expect.objectContaining({ op: 'overview' }));
    expect(postGraphQuery.mock.calls[0]?.[0]).not.toHaveProperty('session');
    expect(postGraphQuery.mock.calls[0]?.[0]).not.toHaveProperty('seed_id');
    expect(fetchGraphSchema).toHaveBeenCalled();
  });

  it('renders an evidence readiness board instead of an empty canvas', async () => {
    postGraphQuery.mockResolvedValue(EMPTY_RESULT);
    fetchGraphSchema.mockResolvedValue(SCHEMA);

    render(<GraphExplorer />);

    const board = await screen.findByRole('region', { name: 'Memory graph readiness' });
    expect(board.textContent).toContain('Schema online');
    expect(board.textContent).toContain('1 node type');
    expect(board.textContent).toContain('1 connection type');
    expect(board.textContent).toContain('Entity');
    expect(board.textContent).toContain('FACT');
    expect(within(board).getByRole('button', { name: 'Load memory graph' })).toBeTruthy();
  });

  it('renders an auth-error state on a 401', async () => {
    // B3 / T-27-03: an expired authentication session surfaces a VISIBLE auth-error
    // state (graph.error.auth), NOT a blank canvas and NOT a swallowed error.
    postGraphQuery.mockRejectedValue(new Error('HTTP 401'));

    render(<GraphExplorer />);

    await waitFor(() => {
      expect(
        screen.getByText('Your session has expired. Sign in again to view the graph.'),
      ).toBeTruthy();
    });
    // The visible auth error is an alert, never a silent blank canvas.
    expect(screen.getByRole('alert')).toBeTruthy();
    expect(screen.queryByTestId('sigma-mock')).toBeNull();
  });

  it('renders the populated canvas with node/edge props on a non-empty overview', async () => {
    postGraphQuery.mockResolvedValue(POPULATED_RESULT);

    render(<GraphExplorer />);

    await waitFor(() => {
      expect(screen.getByTestId('sigma-mock').textContent).toBe('canvas:2:1');
    });
    const workspace = screen.getByTestId('graph-workspace');
    expect(workspace.className).toContain('graph-workspace');
    expect(workspace.className).toContain('overflow-hidden');
    expect(workspace.parentElement?.className).toContain('graph-workspace-container');
    expect(workspace.className).toContain('lg:grid-cols-[18rem_minmax(0,1fr)]');
    const canvas = workspace.querySelector('.graph-workspace__canvas');
    expect(canvas?.className).toContain('min-h-0');
    expect(canvas?.className).not.toContain('min-h-[46svh]');
    expect(screen.getByLabelText('Select a node').className).toContain('hidden');
    expect(screen.getByLabelText('Select a node').className).not.toContain('lg:block');
    // The schema fallback was NOT taken because the overview contained memory.
    expect(fetchGraphSchema).not.toHaveBeenCalled();
  });

  it('renders the query/service error state on a non-401 rejection, and retry re-fetches', async () => {
    postGraphQuery.mockRejectedValueOnce(new Error('HTTP 503'));

    render(<GraphExplorer />);

    await waitFor(() => {
      expect(screen.getByRole('alert')).toBeTruthy();
    });
    const retry = screen.getByText('Retry');
    expect(retry).toBeTruthy();

    // Retry re-dispatches the intent — now it succeeds and the canvas mounts.
    postGraphQuery.mockResolvedValueOnce(POPULATED_RESULT);
    fireEvent.click(retry);
    await waitFor(() => {
      expect(screen.getByTestId('sigma-mock')).toBeTruthy();
    });
  });

  it('shows the cap notice when the overview hits the node/edge caps', async () => {
    const big: GraphResult = {
      nodes: POPULATED_RESULT.nodes,
      edges: [],
      schema: SCHEMA,
      query: 'SELECT FROM `Entity` LIMIT 75',
      truncated: true,
    };
    postGraphQuery.mockResolvedValue(big);

    render(<GraphExplorer />);
    await waitFor(() => {
      expect(screen.getByText(/Showing the top/)).toBeTruthy();
    });
  });

  it('does not infer truncation when a complete result exactly fills a cap', async () => {
    postGraphQuery.mockResolvedValue({
      ...POPULATED_RESULT,
      nodes: Array.from({ length: 75 }, (_, index) => ({
        id: `#1:${String(index)}`,
        caption: `Entity ${String(index)}`,
        labels: ['Entity'],
      })),
      truncated: false,
    });

    render(<GraphExplorer />);
    await screen.findByTestId('sigma-mock');
    expect(screen.queryByText(/Showing the top/)).toBeNull();
  });

  it('the refresh action re-runs the identity overview', async () => {
    postGraphQuery.mockResolvedValue(POPULATED_RESULT);

    render(<GraphExplorer />);
    await waitFor(() => {
      expect(screen.getByTestId('sigma-mock')).toBeTruthy();
    });
    const calls = postGraphQuery.mock.calls.length;
    // Mobile and desktop controls share the same refresh path; click the first.
    const [refreshButton] = screen.getAllByRole('button', { name: 'Load memory graph' });
    if (refreshButton === undefined) throw new Error('refresh action not rendered');
    expect(refreshButton.className).toContain('min-h-[44px]');
    expect(screen.getByRole('button', { name: 'Node types' }).className).toContain('min-h-[44px]');
    fireEvent.click(refreshButton);
    await waitFor(() => {
      expect(postGraphQuery.mock.calls.length).toBeGreaterThan(calls);
    });
  });

  it('clicking a left-panel label filter immediately re-runs the graph query', async () => {
    postGraphQuery.mockResolvedValue(POPULATED_RESULT);

    render(<GraphExplorer />);
    await waitFor(() => {
      expect(screen.getByTestId('sigma-mock')).toBeTruthy();
    });

    const calls = postGraphQuery.mock.calls.length;
    fireEvent.click(screen.getByRole('button', { name: 'Entity' }));

    await waitFor(() => {
      expect(postGraphQuery.mock.calls.length).toBeGreaterThan(calls);
    });
    expect(postGraphQuery.mock.calls.at(-1)?.[0]).toMatchObject({
      op: 'overview',
      labels: ['Entity'],
    });

    const callsAfterLabel = postGraphQuery.mock.calls.length;
    await waitFor(() => {
      expect(screen.getByRole('button', { name: 'Entity' }).getAttribute('aria-pressed')).toBe(
        'true',
      );
    });
    fireEvent.click(screen.getByRole('button', { name: 'FACT' }));

    await waitFor(() => {
      expect(postGraphQuery.mock.calls.length).toBeGreaterThan(callsAfterLabel);
    });
    expect(postGraphQuery.mock.calls.at(-1)?.[0]).toMatchObject({
      op: 'overview',
      labels: ['Entity'],
      rel_types: ['FACT'],
    });
  });

  it('toggling a filter label + selecting a node from the path strip opens the inspector', async () => {
    postGraphQuery.mockResolvedValue(POPULATED_RESULT);

    render(<GraphExplorer />);
    await waitFor(() => {
      expect(screen.getByTestId('sigma-mock')).toBeTruthy();
    });
    // Filter toggle (the dispatch path).
    const filterCalls = postGraphQuery.mock.calls.length;
    fireEvent.click(screen.getByRole('button', { name: 'Entity' }));
    await waitFor(() => {
      expect(postGraphQuery.mock.calls.length).toBeGreaterThan(filterCalls);
    });
    expect(screen.getByLabelText('Node types').getAttribute('data-open')).toBe('false');
    // Select a node via the path-strip node list (the non-hover access path) → inspector opens.
    fireEvent.click(screen.getByRole('button', { name: /Alpha/ }));
    expect(screen.getByTestId('graph-workspace').className).toContain(
      'lg:grid-cols-[18rem_minmax(0,1fr)_20rem]',
    );
    expect(screen.getByLabelText('Select a node').getAttribute('data-open')).toBe('true');
    expect(screen.getByText('Connections')).toBeTruthy(); // inspector degree label
  });

  it('falls to the schema-error state when the empty-overview schema fetch also fails', async () => {
    postGraphQuery.mockResolvedValue(EMPTY_RESULT);
    fetchGraphSchema.mockRejectedValue(new Error('HTTP 500'));

    render(<GraphExplorer />);
    await waitFor(() => {
      expect(screen.getByText("Couldn't load the graph structure. Retry.")).toBeTruthy();
    });
  });

  it('surfaces an auth error when the empty-overview schema fetch returns 401', async () => {
    postGraphQuery.mockResolvedValue(EMPTY_RESULT);
    fetchGraphSchema.mockRejectedValue(new Error('HTTP 401'));

    render(<GraphExplorer />);
    await waitFor(() => {
      expect(
        screen.getByText('Your session has expired. Sign in again to view the graph.'),
      ).toBeTruthy();
    });
  });

  it('pin path from the inspector highlights the node + its neighbors in the path strip', async () => {
    postGraphQuery.mockResolvedValue(POPULATED_RESULT);

    render(<GraphExplorer />);
    await waitFor(() => {
      expect(screen.getByTestId('sigma-mock')).toBeTruthy();
    });
    // Open the inspector on Alpha, then pin its path.
    fireEvent.click(screen.getByRole('button', { name: /Alpha/ }));
    fireEvent.click(screen.getByRole('button', { name: 'Pin path' }));
    // The path strip now mirrors Alpha + its neighbor Bravo (connected via e1).
    const pathSection = screen.getByLabelText('Selected path');
    expect(pathSection.textContent).toContain('Alpha');
    expect(pathSection.textContent).toContain('Bravo');
  });

  it('expands a selected RID and cumulatively merges the returned neighbors', async () => {
    const expansion: GraphResult = {
      nodes: [
        { id: '#1:0', caption: 'Alpha', labels: ['Entity'] },
        { id: '#1:2', caption: 'Charlie', labels: ['Entity'] },
      ],
      edges: [{ id: '#5:1', source: '#1:0', target: '#1:2', rel_type: 'FACT' }],
      schema: POPULATED_RESULT.schema,
      query: 'SELECT expand(bothE()) FROM #1:0 LIMIT 200',
    };
    postGraphQuery.mockResolvedValueOnce(POPULATED_RESULT).mockResolvedValueOnce(expansion);

    render(<GraphExplorer />);
    await screen.findByTestId('sigma-mock');
    fireEvent.click(screen.getByRole('button', { name: /Alpha/ }));
    fireEvent.click(screen.getByRole('button', { name: 'Expand neighbors' }));

    await waitFor(() => {
      expect(screen.getByTestId('sigma-mock').textContent).toBe('canvas:3:2');
    });
    expect(postGraphQuery.mock.calls.at(-1)?.[0]).toMatchObject({
      op: 'expand',
      node_id: '#1:0',
    });
  });
});
