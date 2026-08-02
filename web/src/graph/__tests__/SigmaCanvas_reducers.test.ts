import { describe, expect, it } from 'vitest';
import {
  compactCanvasLabel,
  pinnedEdgeReducer,
  pinnedNodeReducer,
  REDUCER_COLORS,
  shouldRenderAmbientLabels,
  type EdgeDisplay,
  type NodeDisplay,
} from '../SigmaCanvas_reducers';

// Pure pinned-path highlight/dim reducers (no WebGL — jsdom-safe, Stryker-reachable). They
// drive the D-08/D-10 "evidence path bright, everything else dim" encoding.

describe('pinnedNodeReducer', () => {
  const base: NodeDisplay = { color: '#AECBFA', label: 'Alpha', size: 8 };

  it('returns the node unchanged when nothing is pinned (empty path)', () => {
    expect(pinnedNodeReducer('n1', base, new Set())).toEqual(base);
  });

  it('accents a node that is on the pinned path', () => {
    const out = pinnedNodeReducer('n1', base, new Set(['n1']));
    expect(out.color).toBe(REDUCER_COLORS.PATH_NODE_COLOR);
    expect(out.zIndex).toBe(1);
  });

  it('restores the compact canvas label for a focused node when ambient labels are hidden', () => {
    const out = pinnedNodeReducer(
      'n1',
      { ...base, label: null, canvasLabel: 'd:42' },
      new Set(['n1']),
    );
    expect(out.label).toBe('d:42');
  });

  it('dims + drops the label of a node that is off the pinned path', () => {
    const out = pinnedNodeReducer('n2', base, new Set(['n1']));
    expect(out.color).toBe(REDUCER_COLORS.DIM_COLOR);
    expect(out.label).toBeNull();
    expect(out.zIndex).toBe(0);
  });
});

describe('pinnedEdgeReducer', () => {
  const base: EdgeDisplay = { color: '#5F6368', size: 1.5 };

  it('returns the edge unchanged when nothing is pinned', () => {
    expect(pinnedEdgeReducer('a', 'b', base, new Set())).toEqual(base);
  });

  it('brightens an edge whose both endpoints are on the pinned path', () => {
    const out = pinnedEdgeReducer('a', 'b', base, new Set(['a', 'b']));
    expect(out.color).toBe(REDUCER_COLORS.PATH_EDGE_COLOR);
    expect(out.zIndex).toBe(1);
  });

  it('dims an edge with an endpoint off the pinned path', () => {
    const out = pinnedEdgeReducer('a', 'c', base, new Set(['a', 'b']));
    expect(out.color).toBe(REDUCER_COLORS.DIM_COLOR);
    expect(out.zIndex).toBe(0);
  });
});

describe('canvas label helpers', () => {
  it('compacts raw graph record IDs and UUID-heavy captions for the WebGL canvas', () => {
    expect(compactCanvasLabel('4:21de867b-135a-43b0-9598-b82a63a99e7d:d:63')).toBe('d:63');
    expect(compactCanvasLabel('21de867b-135a-43b0-9598-b82a63a99e7d')).toBe('21de867b...');
  });

  it('leaves short human captions alone', () => {
    expect(compactCanvasLabel('web_search')).toBe('web_search');
  });

  it('suppresses ambient labels for dense overviews', () => {
    expect(shouldRenderAmbientLabels(12, 20)).toBe(true);
    expect(shouldRenderAmbientLabels(75, 64)).toBe(false);
  });
});
