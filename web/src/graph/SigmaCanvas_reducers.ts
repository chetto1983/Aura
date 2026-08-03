// SigmaCanvas_reducers.ts — the PURE highlight/dim reducer helpers split off
// SigmaCanvas.tsx (concern split, CLAUDE.md no-god-class). These take a sigma node/
// edge display-attribute object + the pinned-path membership and return the adjusted
// attributes (D-08/D-10: the pinned evidence path stays bright, everything else dims).
// They import NO sigma/graphology/@react-sigma — they are plain attribute transforms,
// so they are jsdom-unit-testable (Pitfall 4) and Stryker-reachable.

import { studioCaption } from './SigmaCanvas_labels';

const DIM_COLOR = '#3C4043'; // --color-border — the dimmed fill/stroke (off-path context)
const PATH_NODE_COLOR = '#8AB4F8'; // --color-ring — the pinned-path accent (the single signal)
const PATH_EDGE_COLOR = '#8AB4F8';

/** A minimal mirror of sigma's node display data — only the fields the reducers touch. */
export interface NodeDisplay {
  color?: string;
  label?: string | null;
  canvasLabel?: string;
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
 * are (accent fill + a higher zIndex so the path reads on top). Captions survive the dim:
 * Studio-style names are graph content, not decoration to erase during path focus.
 */
export function pinnedNodeReducer(
  nodeId: string,
  data: NodeDisplay,
  pinnedPath: ReadonlySet<string>,
): NodeDisplay {
  if (pinnedPath.size === 0) return data;
  if (pinnedPath.has(nodeId)) {
    return {
      ...data,
      color: PATH_NODE_COLOR,
      label: data.canvasLabel ?? data.label ?? null,
      zIndex: 1,
    };
  }
  return { ...data, color: DIM_COLOR, zIndex: 0 };
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

const UUID_RE = /[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}/i;
const LONG_HEX_RE = /[0-9a-f]{24,}/i;
const ELEMENT_ID_RE = /^(\d+):([0-9a-f-]{36}):([A-Za-z]+):(\d+)$/;
export function compactCanvasLabel(caption: string): string {
  const trimmed = caption.trim();
  if (trimmed === '') return '';

  const elementId = ELEMENT_ID_RE.exec(trimmed);
  if (elementId !== null) return `${elementId[3] ?? ''}:${elementId[4] ?? ''}`;

  const uuid = UUID_RE.exec(trimmed);
  if (uuid !== null) {
    const suffix = trimmed.slice(Math.max(uuid.index + uuid[0].length, trimmed.length - 6));
    return suffix === '' ? `${uuid[0].slice(0, 8)}...` : `${uuid[0].slice(0, 8)}...${suffix}`;
  }

  const longHex = LONG_HEX_RE.exec(trimmed);
  if (longHex !== null) return `${trimmed.slice(0, 8)}...${trimmed.slice(-6)}`;

  return studioCaption(trimmed);
}
