import { describe, expect, it, vi, beforeEach } from 'vitest';
import { fireEvent, render, screen, waitFor } from '@testing-library/react';
import '../../i18n/i18n'; // side-effect: initialise i18next so t() resolves keys
import type { GraphResult, GraphSchema } from '../types';

// GraphExplorer is the lazy three-pane shell. SigmaCanvas is mocked (no WebGL in jsdom,
// Pitfall 4) and graphApi is mocked so we drive the seed→schema-overview fallback and the
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
    { id: 'n1', caption: 'Alpha', labels: ['Entity'], degree: 2 },
    { id: 'n2', caption: 'Bravo', labels: ['Document'], degree: 1 },
  ],
  edges: [{ id: 'e1', source: 'n1', target: 'n2', rel_type: 'MENTIONS' }],
  schema: { labels: ['Entity', 'Document'], rel_types: ['MENTIONS'] },
  query: 'MATCH (e:Entity) RETURN e',
};

const SCHEMA: GraphSchema = { labels: ['Entity', 'Document'], rel_types: ['MENTIONS'] };

describe('GraphExplorer (renderer + graphApi mocked)', () => {
  beforeEach(() => {
    postGraphQuery.mockReset();
    fetchGraphSchema.mockReset();
  });

  it('seeds from the threadId and falls back to the schema overview on an empty result', async () => {
    postGraphQuery.mockResolvedValue(EMPTY_RESULT);
    fetchGraphSchema.mockResolvedValue(SCHEMA);

    render(<GraphExplorer threadId="thread-123" />);

    // The empty seed result triggers the schema-overview empty-state (never a blank canvas).
    await waitFor(() => {
      expect(screen.getByText('No evidence graph yet')).toBeTruthy();
    });
    expect(postGraphQuery).toHaveBeenCalled();
    expect(fetchGraphSchema).toHaveBeenCalled();
  });

  it('renders an auth-error state on a 401', async () => {
    // B3 / T-27-03: an expired-session 401 from the seed fetch surfaces a VISIBLE auth-error
    // state (graph.error.auth), NOT a blank canvas and NOT a swallowed error.
    postGraphQuery.mockRejectedValue(new Error('HTTP 401'));

    render(<GraphExplorer threadId="thread-123" />);

    await waitFor(() => {
      expect(
        screen.getByText('Your session has expired. Sign in again to view the graph.'),
      ).toBeTruthy();
    });
    // The visible auth error is an alert, never a silent blank canvas.
    expect(screen.getByRole('alert')).toBeTruthy();
    expect(screen.queryByTestId('sigma-mock')).toBeNull();
  });

  it('renders the populated canvas with node/edge props on a non-empty seed result', async () => {
    postGraphQuery.mockResolvedValue(POPULATED_RESULT);

    render(<GraphExplorer threadId="thread-123" />);

    await waitFor(() => {
      expect(screen.getByTestId('sigma-mock').textContent).toBe('canvas:2:1');
    });
    expect(screen.getByTestId('graph-workspace').className).toContain(
      'lg:grid-cols-[18rem_minmax(0,1fr)]',
    );
    expect(screen.getByLabelText('Select a node').className).toContain('hidden');
    expect(screen.getByLabelText('Select a node').className).not.toContain('lg:block');
    // The schema-overview fallback was NOT taken (we had a real result).
    expect(fetchGraphSchema).not.toHaveBeenCalled();
  });

  it('renders the query/service error state on a non-401 rejection, and retry re-fetches', async () => {
    postGraphQuery.mockRejectedValueOnce(new Error('HTTP 503'));

    render(<GraphExplorer threadId="thread-123" />);

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

  it('shows the cap notice when the seed result hits the node/edge caps', async () => {
    const big: GraphResult = {
      nodes: Array.from({ length: 80 }, (_, i) => ({
        id: `n${String(i)}`,
        caption: `N${String(i)}`,
      })),
      edges: [],
      schema: SCHEMA,
      query: 'MATCH (n) RETURN n',
    };
    postGraphQuery.mockResolvedValue(big);

    render(<GraphExplorer threadId="thread-123" />);
    await waitFor(() => {
      expect(screen.getByText(/Showing the top/)).toBeTruthy();
    });
  });

  it('the seed CTA re-runs the intent (the SeedFilterPanel onSeed path)', async () => {
    postGraphQuery.mockResolvedValue(POPULATED_RESULT);

    render(<GraphExplorer threadId="thread-123" />);
    await waitFor(() => {
      expect(screen.getByTestId('sigma-mock')).toBeTruthy();
    });
    const calls = postGraphQuery.mock.calls.length;
    // The mobile-first layout renders the seed CTA twice (the mobile control bar + the
    // desktop seed panel) — both drive the same onSeed path; click the first.
    const [seedButton] = screen.getAllByRole('button', { name: 'Explore this conversation' });
    if (seedButton === undefined) throw new Error('seed CTA not rendered');
    fireEvent.click(seedButton);
    await waitFor(() => {
      expect(postGraphQuery.mock.calls.length).toBeGreaterThan(calls);
    });
  });

  it('toggling a filter label + selecting a node from the path strip opens the inspector', async () => {
    postGraphQuery.mockResolvedValue(POPULATED_RESULT);

    render(<GraphExplorer threadId="thread-123" />);
    await waitFor(() => {
      expect(screen.getByTestId('sigma-mock')).toBeTruthy();
    });
    // Filter toggle (the dispatch path).
    fireEvent.click(screen.getByRole('button', { name: 'Entity' }));
    // Select a node via the path-strip node list (the non-hover access path) → inspector opens.
    fireEvent.click(screen.getByRole('button', { name: /Alpha/ }));
    expect(screen.getByTestId('graph-workspace').className).toContain(
      'lg:grid-cols-[18rem_minmax(0,1fr)_20rem]',
    );
    expect(screen.getByText('Connections')).toBeTruthy(); // inspector degree label
  });

  it('falls to the schema-error state when the empty-seed schema fetch also fails', async () => {
    postGraphQuery.mockResolvedValue(EMPTY_RESULT);
    fetchGraphSchema.mockRejectedValue(new Error('HTTP 500'));

    render(<GraphExplorer threadId="thread-123" />);
    await waitFor(() => {
      expect(screen.getByText("Couldn't load the graph structure. Retry.")).toBeTruthy();
    });
  });

  it('surfaces an auth error when the empty-seed schema fetch returns 401', async () => {
    postGraphQuery.mockResolvedValue(EMPTY_RESULT);
    fetchGraphSchema.mockRejectedValue(new Error('HTTP 401'));

    render(<GraphExplorer threadId="thread-123" />);
    await waitFor(() => {
      expect(
        screen.getByText('Your session has expired. Sign in again to view the graph.'),
      ).toBeTruthy();
    });
  });

  it('pin path from the inspector highlights the node + its neighbors in the path strip', async () => {
    postGraphQuery.mockResolvedValue(POPULATED_RESULT);

    render(<GraphExplorer threadId="thread-123" />);
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
});
