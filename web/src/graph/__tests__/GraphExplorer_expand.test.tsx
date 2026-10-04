import { describe, expect, it, vi, beforeEach } from 'vitest';
import { act, fireEvent, render, screen, waitFor } from '@testing-library/react';
import '../../i18n/i18n';
import type { GraphResult, GraphSchema } from '../types';

// Expanding a node adds its neighbours to the graph on screen. It used to swap the canvas for
// a loading line, lay everything out again from random positions, and say nothing when the
// neighbours were already shown, so on a real memory it looked like it did nothing
// (prd.md §9, 2026-10-04).

const postGraphQuery = vi.fn();
const fetchGraphSchema = vi.fn();

vi.mock('../graphApi', () => ({
  postGraphQuery: (...args: unknown[]) => postGraphQuery(...args) as Promise<GraphResult>,
  fetchGraphSchema: (...args: unknown[]) => fetchGraphSchema(...args) as Promise<GraphSchema>,
}));

vi.mock('../ArcadeGraphCanvas', () => ({
  ArcadeGraphCanvas: ({
    nodes,
    edges,
    focusId,
    onNodeDoubleClick,
  }: {
    nodes: readonly { id: string }[];
    edges: readonly unknown[];
    focusId: string | undefined;
    onNodeDoubleClick: (id: string) => void;
  }) => (
    <div data-testid="arcade-graph-mock" data-focus={focusId ?? ''}>
      canvas:{nodes.length}:{edges.length}
      {nodes.map((node) => (
        <button
          key={node.id}
          type="button"
          onClick={() => {
            onNodeDoubleClick(node.id);
          }}
        >
          dbl:{node.id}
        </button>
      ))}
    </div>
  ),
}));

const { default: GraphExplorer } = await import('../GraphExplorer');

const SCHEMA = { labels: ['Entity'], rel_types: ['FACT'] };

const OVERVIEW: GraphResult = {
  nodes: [
    { id: '#1:0', caption: 'Alpha', labels: ['Entity'], degree: 2 },
    { id: '#1:1', caption: 'Bravo', labels: ['Entity'], degree: 1 },
    { id: '#1:3', caption: 'Delta', labels: ['Entity'], degree: 0 },
  ],
  edges: [{ id: '#5:0', source: '#1:0', target: '#1:1', rel_type: 'FACT' }],
  schema: SCHEMA,
  query: 'SELECT FROM `FACT` LIMIT 200',
};

const ALPHA_WITH_CHARLIE: GraphResult = {
  nodes: [
    { id: '#1:0', caption: 'Alpha', labels: ['Entity'] },
    { id: '#1:1', caption: 'Bravo', labels: ['Entity'] },
    { id: '#1:2', caption: 'Charlie', labels: ['Entity'] },
  ],
  edges: [
    { id: '#5:0', source: '#1:0', target: '#1:1', rel_type: 'FACT' },
    { id: '#5:1', source: '#1:0', target: '#1:2', rel_type: 'FACT' },
  ],
  schema: SCHEMA,
  query: 'SELECT expand(bothE()) FROM #1:0 LIMIT 200',
};

function deferred<T>() {
  let resolve: (value: T) => void = () => undefined;
  let reject: (reason: unknown) => void = () => undefined;
  const promise = new Promise<T>((res, rej) => {
    resolve = res;
    reject = rej;
  });
  return { promise, resolve, reject };
}

async function openOn(caption: string) {
  render(<GraphExplorer />);
  await screen.findByTestId('arcade-graph-mock');
  fireEvent.click(screen.getByRole('button', { name: new RegExp(`^${caption}`) }));
}

function expandSelected() {
  fireEvent.click(screen.getByRole('button', { name: 'Expand neighbors' }));
}

function pathText(): string {
  return screen.getByLabelText('Selected path').textContent;
}

describe('GraphExplorer expansion', () => {
  beforeEach(() => {
    postGraphQuery.mockReset();
    fetchGraphSchema.mockReset();
  });

  it('keeps the canvas while expanding, then reports and marks what it added', async () => {
    const pending = deferred<GraphResult>();
    postGraphQuery.mockResolvedValueOnce(OVERVIEW).mockReturnValueOnce(pending.promise);
    await openOn('Alpha');
    expandSelected();

    expect(screen.getByTestId('arcade-graph-mock').textContent).toContain('canvas:3:1');
    expect(screen.getByRole('status', { name: '' }).textContent).toBe('Expanding…');
    expect(screen.queryByText('Loading your ArcadeDB memory graph…')).toBeNull();

    await act(async () => {
      pending.resolve(ALPHA_WITH_CHARLIE);
      await pending.promise;
    });
    expect(screen.getByTestId('arcade-graph-mock').textContent).toContain('canvas:4:2');
    expect(screen.getByTestId('arcade-graph-mock').dataset.focus).toBe('#1:0');
    expect(screen.getByText('1 new neighbor added')).toBeTruthy();
    expect(pathText()).toContain('Alpha');
    expect(pathText()).toContain('Bravo');
    expect(pathText()).toContain('Charlie');
    expect(pathText()).not.toContain('Delta');
  });

  it('says so when every neighbour is already on the graph, and still marks them', async () => {
    postGraphQuery.mockResolvedValueOnce(OVERVIEW).mockResolvedValueOnce({
      ...OVERVIEW,
      nodes: OVERVIEW.nodes.slice(0, 2),
    });
    await openOn('Alpha');
    expandSelected();

    expect(await screen.findByText('Its only neighbor is already on the graph')).toBeTruthy();
    expect(screen.getByTestId('arcade-graph-mock').textContent).toContain('canvas:3:1');
    expect(pathText()).toContain('Bravo');
  });

  it('says so when the node has no connections', async () => {
    postGraphQuery
      .mockResolvedValueOnce(OVERVIEW)
      .mockResolvedValueOnce({ nodes: [], edges: [], schema: SCHEMA, query: '' });
    await openOn('Delta');
    expandSelected();

    expect(await screen.findByText('This node has no connections')).toBeTruthy();
    expect(screen.getByTestId('arcade-graph-mock').textContent).toContain('canvas:3:1');
  });

  it('keeps the graph when an expansion fails', async () => {
    postGraphQuery.mockResolvedValueOnce(OVERVIEW).mockRejectedValueOnce(new Error('HTTP 502'));
    await openOn('Alpha');
    expandSelected();

    expect(await screen.findByText('Could not expand this node')).toBeTruthy();
    expect(screen.getByTestId('arcade-graph-mock').textContent).toContain('canvas:3:1');
  });

  it('shows the session error when an expansion is refused for auth', async () => {
    postGraphQuery.mockResolvedValueOnce(OVERVIEW).mockRejectedValueOnce(new Error('HTTP 401'));
    await openOn('Alpha');
    expandSelected();

    await waitFor(() => {
      expect(screen.queryByTestId('arcade-graph-mock')).toBeNull();
    });
  });

  it('expands a node on double click', async () => {
    postGraphQuery.mockResolvedValueOnce(OVERVIEW).mockResolvedValueOnce(ALPHA_WITH_CHARLIE);
    render(<GraphExplorer />);
    await screen.findByTestId('arcade-graph-mock');
    fireEvent.click(screen.getByRole('button', { name: 'dbl:#1:0' }));

    expect(await screen.findByText('1 new neighbor added')).toBeTruthy();
    expect(postGraphQuery.mock.calls.at(-1)?.[0]).toMatchObject({ op: 'expand', node_id: '#1:0' });
  });

  it('runs one expansion at a time', async () => {
    const pending = deferred<GraphResult>();
    postGraphQuery.mockResolvedValueOnce(OVERVIEW).mockReturnValueOnce(pending.promise);
    await openOn('Alpha');
    expandSelected();
    expandSelected();
    fireEvent.click(screen.getByRole('button', { name: 'dbl:#1:1' }));

    expect(postGraphQuery).toHaveBeenCalledTimes(2);
    await act(async () => {
      pending.resolve(ALPHA_WITH_CHARLIE);
      await pending.promise;
    });
  });

  it('clears the expansion report when the overview is read again', async () => {
    postGraphQuery
      .mockResolvedValueOnce(OVERVIEW)
      .mockResolvedValueOnce(ALPHA_WITH_CHARLIE)
      .mockResolvedValueOnce(OVERVIEW);
    await openOn('Alpha');
    expandSelected();
    expect(await screen.findByText('1 new neighbor added')).toBeTruthy();

    const [refresh] = screen.getAllByRole('button', { name: 'Load memory graph' });
    if (refresh === undefined) throw new Error('no refresh button');
    fireEvent.click(refresh);
    await waitFor(() => {
      expect(screen.queryByText('1 new neighbor added')).toBeNull();
    });
  });
});
