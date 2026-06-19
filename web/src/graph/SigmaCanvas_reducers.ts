// SigmaCanvas_reducers.ts — the PURE highlight/dim reducer helpers split off
// SigmaCanvas.tsx (concern split, CLAUDE.md no-god-class). These take a sigma node/
// edge display-attribute object + the pinned-path membership and return the adjusted
// attributes (D-08/D-10: the pinned evidence path stays bright, everything else dims).
// They import NO sigma/graphology/@react-sigma — they are plain attribute transforms,
// so they are jsdom-unit-testable (Pitfall 4) and Stryker-reachable.

const DIM_COLOR = '#3C4043'; // --color-border — the dimmed fill/stroke (off-path context)
const PATH_NODE_COLOR = '#8AB4F8'; // --color-ring — the pinned-path accent (the single signal)
const PATH_EDGE_COLOR = '#8AB4F8';

/** A minimal mirror of sigma's node display data — only the fields the reducers touch. */
export interface NodeDisplay {
  color?: string;
  label?: string | null;
  size?: number;
  zIndex?: number;
  hidden?: boolean;
  [key: string]: unknown;
}

/** A minimal mirror of sigma's edge display data. */
export interface EdgeDisplay {
  color?: string;
  size?: number;
  zIndex?: number;
  hidden?: boolean;
  [key: string]: unknown;
}

/**
 * pinnedNodeReducer dims a node that is not on the pinned path and lifts the ones that
 * are (accent fill + a higher zIndex so the path reads on top). An EMPTY pinnedPath means
 * "nothing pinned" → the node renders unchanged (its label-family fill from the loader).
 */
export function pinnedNodeReducer(
  nodeId: string,
  data: NodeDisplay,
  pinnedPath: ReadonlySet<string>,
): NodeDisplay {
  if (pinnedPath.size === 0) return data;
  if (pinnedPath.has(nodeId)) {
    return { ...data, color: PATH_NODE_COLOR, zIndex: 1 };
  }
  return { ...data, color: DIM_COLOR, label: null, zIndex: 0 };
}

/**
 * pinnedEdgeReducer brightens an edge whose BOTH endpoints are on the pinned path and dims
 * every other edge. An empty pinnedPath leaves the edge unchanged.
 */
export function pinnedEdgeReducer(
  source: string,
  target: string,
  data: EdgeDisplay,
  pinnedPath: ReadonlySet<string>,
): EdgeDisplay {
  if (pinnedPath.size === 0) return data;
  if (pinnedPath.has(source) && pinnedPath.has(target)) {
    return { ...data, color: PATH_EDGE_COLOR, zIndex: 1 };
  }
  return { ...data, color: DIM_COLOR, hidden: false, zIndex: 0 };
}

export const REDUCER_COLORS = { DIM_COLOR, PATH_NODE_COLOR, PATH_EDGE_COLOR } as const;
