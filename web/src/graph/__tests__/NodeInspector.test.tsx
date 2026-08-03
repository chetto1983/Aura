import { describe, expect, it, vi } from 'vitest';
import { fireEvent, render, screen, within } from '@testing-library/react';
import '../../i18n/i18n'; // side-effect: initialise i18next so t() resolves keys
import { NodeInspector } from '../NodeInspector';
import { sourcesForNode } from '../nodeSources';
import { PathStrip } from '../PathStrip';
import type { GraphEdge, GraphNode } from '../types';

// NodeInspector / PathStrip cover the GRAPH-03 / D-03 / D-09 + SC3 obligations as unit
// assertions (the full axe/keyboard run is the Task-4 Playwright spec): the read-only action
// set (no add-note, no editable SQL), the citations list distinct from the open-source
// action, the openSources cross-link, properties-as-text, and the non-hover access path
// (node-list Enter selects = a canvas click).

const openSources = vi.fn();
vi.mock('../../chat/displays/sourceExplorerControls', () => ({
  useSourceExplorer: () => ({ openSources }),
}));

function docNode(over: Partial<GraphNode> = {}): GraphNode {
  return {
    id: 'doc-1',
    caption: 'Evidence doc',
    labels: ['Document'],
    degree: 3,
    props: { url: 'https://example.test/a', title: 'Evidence doc' },
    ref_id: 'src-9',
    citations: ['Source A', 'Source B'],
    ...over,
  };
}

describe('NodeInspector (read-only action set + citations)', () => {
  it('inspector renders the read-only set and NO add-note / NO editable SQL input', () => {
    render(
      <NodeInspector
        node={docNode()}
        query="SELECT FROM `Entity` LIMIT 75"
        onExpand={vi.fn()}
        onPinPath={vi.fn()}
        onClose={vi.fn()}
      />,
    );
    expect(screen.getByRole('button', { name: 'Pin path' })).toBeTruthy();
    expect(screen.getByRole('button', { name: 'Open source' })).toBeTruthy();
    expect(screen.getByRole('button', { name: 'Expand neighbors' })).toBeTruthy();
    expect(screen.getByRole('button', { name: 'Show ArcadeDB SQL' })).toBeTruthy();
    // T-27-02: add-note is NOT rendered (graph WRITES deferred to Phase 29).
    expect(screen.queryByRole('button', { name: /add note/i })).toBeNull();
    // The ArcadeDB SQL is display-only: toggling reveals text, never an input.
    fireEvent.click(screen.getByRole('button', { name: 'Show ArcadeDB SQL' }));
    expect(screen.getByText('SELECT FROM `Entity` LIMIT 75')).toBeTruthy();
    expect(screen.queryByRole('textbox')).toBeNull();
  });

  it('inspector renders citations (a node with a list shows them; one without renders none)', () => {
    // SC3 / D-09: the citations list is a DISPLAYED field, distinct from the open-source action.
    const { unmount } = render(
      <NodeInspector
        node={docNode()}
        query=""
        onExpand={vi.fn()}
        onPinPath={vi.fn()}
        onClose={vi.fn()}
      />,
    );
    expect(screen.getByText('Source A')).toBeTruthy();
    expect(screen.getByText('Source B')).toBeTruthy();
    unmount();

    // A node with no citations renders the empty affordance, never crashes.
    render(
      <NodeInspector
        node={docNode({ citations: [] })}
        query=""
        onExpand={vi.fn()}
        onPinPath={vi.fn()}
        onClose={vi.fn()}
      />,
    );
    expect(screen.getByText('No citations for this node.')).toBeTruthy();
    expect(screen.queryByText('Source A')).toBeNull();
  });

  it('inspector open-source action calls openSources with the node sources + refId', () => {
    openSources.mockClear();
    render(
      <NodeInspector
        node={docNode()}
        query=""
        onExpand={vi.fn()}
        onPinPath={vi.fn()}
        onClose={vi.fn()}
      />,
    );
    fireEvent.click(screen.getByRole('button', { name: 'Open source' }));
    expect(openSources).toHaveBeenCalledTimes(1);
    const [sources, refId] = openSources.mock.calls[0] as [readonly { ref_id: string }[], string];
    expect(refId).toBe('src-9');
    expect(sources[0]?.ref_id).toBe('src-9');
  });

  it('inspector renders properties as TEXT (a script-like value is inert)', () => {
    render(
      <NodeInspector
        node={docNode({ props: { note: '<script>window.__x=1</script>' } })}
        query=""
        onExpand={vi.fn()}
        onPinPath={vi.fn()}
        onClose={vi.fn()}
      />,
    );
    // The script string is present as escaped text, not executed/injected.
    expect(screen.getByText('<script>window.__x=1</script>')).toBeTruthy();
    expect((window as unknown as Record<string, unknown>).__x).toBeUndefined();
  });

  it('inspector close button invokes onClose (focus-return to the opener)', () => {
    const onClose = vi.fn();
    render(
      <NodeInspector
        node={docNode()}
        query=""
        onExpand={vi.fn()}
        onPinPath={vi.fn()}
        onClose={onClose}
      />,
    );
    fireEvent.click(screen.getByRole('button', { name: /close/i }));
    expect(onClose).toHaveBeenCalledTimes(1);
  });

  it('inspector pin-path action raises onPinPath with the node', () => {
    const onPinPath = vi.fn();
    const node = docNode();
    render(
      <NodeInspector
        node={node}
        query=""
        onExpand={vi.fn()}
        onPinPath={onPinPath}
        onClose={vi.fn()}
      />,
    );
    fireEvent.click(screen.getByRole('button', { name: 'Pin path' }));
    expect(onPinPath).toHaveBeenCalledWith(node);
  });

  it('inspector expansion raises the selected ArcadeDB RID', () => {
    const onExpand = vi.fn();
    const node = docNode({ id: '#1:7' });
    render(
      <NodeInspector
        node={node}
        query=""
        onExpand={onExpand}
        onPinPath={vi.fn()}
        onClose={vi.fn()}
      />,
    );
    fireEvent.click(screen.getByRole('button', { name: 'Expand neighbors' }));
    expect(onExpand).toHaveBeenCalledWith(node);
  });

  it('inspector hides open-source for a non-document node + shows the empty state for none', () => {
    // A non-:Document/:Source/:Chunk node has no "open source" deep-link.
    render(
      <NodeInspector
        node={docNode({ labels: ['Entity'] })}
        query=""
        onExpand={vi.fn()}
        onPinPath={vi.fn()}
        onClose={vi.fn()}
      />,
    );
    expect(screen.queryByRole('button', { name: 'Open source' })).toBeNull();

    // No node selected → the empty inspector state.
    render(
      <NodeInspector
        node={undefined}
        query=""
        onExpand={vi.fn()}
        onPinPath={vi.fn()}
        onClose={vi.fn()}
      />,
    );
    expect(screen.getByText('Select a node')).toBeTruthy();
  });
});

describe('PathStrip (a11y parallel DOM + path mirror)', () => {
  const NODES: readonly GraphNode[] = [
    { id: 'n1', caption: 'Alpha', labels: ['Entity'] },
    { id: 'n2', caption: 'Bravo', labels: ['Document'] },
  ];
  const EDGES: readonly GraphEdge[] = [
    { id: 'e1', source: 'n1', target: 'n2', rel_type: 'MENTIONS' },
  ];

  it('path strip exposes role="list" node + edge lists with the counts', () => {
    render(<PathStrip nodes={NODES} edges={EDGES} pinnedPath={new Set()} onSelectNode={vi.fn()} />);
    const lists = screen.getAllByRole('list');
    expect(lists.length).toBeGreaterThanOrEqual(2); // node list + edge list
    expect(screen.getByText('Nodes (2) — use arrow keys, Enter to inspect')).toBeTruthy();
    expect(screen.getByText('Connections (1)')).toBeTruthy();
  });

  it('path strip node-list Enter opens the inspector (the non-hover access path, D-03)', () => {
    const onSelectNode = vi.fn();
    render(
      <PathStrip nodes={NODES} edges={EDGES} pinnedPath={new Set()} onSelectNode={onSelectNode} />,
    );
    const alphaItem = screen.getByRole('button', { name: /Alpha/ });
    fireEvent.keyDown(alphaItem, { key: 'Enter' });
    expect(onSelectNode).toHaveBeenCalledWith(NODES[0]);
  });

  it('path strip mirrors the pinned path as ordered node steps', () => {
    render(
      <PathStrip
        nodes={NODES}
        edges={EDGES}
        pinnedPath={new Set(['n1', 'n2'])}
        onSelectNode={vi.fn()}
      />,
    );
    const pathSection = screen.getByLabelText('Selected path');
    expect(within(pathSection).getByText('Alpha')).toBeTruthy();
    expect(within(pathSection).getByText('Bravo')).toBeTruthy();
  });

  it('path strip arrow keys roam the node list (keyboard traversal, D-03)', () => {
    render(<PathStrip nodes={NODES} edges={EDGES} pinnedPath={new Set()} onSelectNode={vi.fn()} />);
    const alpha = screen.getByRole('button', { name: /Alpha/ });
    const bravo = screen.getByRole('button', { name: /Bravo/ });
    alpha.focus();
    fireEvent.keyDown(alpha, { key: 'ArrowDown' });
    expect(document.activeElement).toBe(bravo);
    fireEvent.keyDown(bravo, { key: 'ArrowUp' });
    expect(document.activeElement).toBe(alpha);
    // Space also selects (non-hover path).
    const onSelectNode = vi.fn();
    render(
      <PathStrip nodes={NODES} edges={EDGES} pinnedPath={new Set()} onSelectNode={onSelectNode} />,
    );
    fireEvent.keyDown(screen.getAllByRole('button', { name: /Alpha/ })[1] as Element, { key: ' ' });
    expect(onSelectNode).toHaveBeenCalledWith(NODES[0]);
  });

  it('path strip shows the empty path message when nothing is pinned', () => {
    render(<PathStrip nodes={NODES} edges={EDGES} pinnedPath={new Set()} onSelectNode={vi.fn()} />);
    expect(screen.getByText('No path selected')).toBeTruthy();
  });
});

describe('sourcesForNode (pure cross-link projection)', () => {
  it('projects the node url + refId into a single DisplaySource', () => {
    const sources = sourcesForNode(docNode());
    expect(sources).toHaveLength(1);
    expect(sources[0]?.ref_id).toBe('src-9');
    expect(sources[0]?.url).toBe('https://example.test/a');
  });

  it('omits url when the node has none, and falls back to id when ref_id is absent', () => {
    const sources = sourcesForNode({ id: 'bare-1', labels: ['Document'] });
    expect(sources[0]?.ref_id).toBe('bare-1'); // ref_id falls back to id
    expect(sources[0]?.url).toBeUndefined(); // no url prop → omitted
    expect(sources[0]?.title).toBe('bare-1'); // caption falls back to refId
  });
});
