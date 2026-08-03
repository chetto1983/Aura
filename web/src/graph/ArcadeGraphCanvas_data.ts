import type cytoscape from 'cytoscape';
import type { ClientEdge, ClientNode } from './types';

export const ARCADE_NODE_BASE_DIAMETER = 25;
export const ARCADE_NODE_MAX_DIAMETER = 200;
export const ARCADE_LABEL_MAX_LENGTH = 45;

const UUID_RE = /[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}/i;
const LONG_HEX_RE = /[0-9a-f]{24,}/i;
const ELEMENT_ID_RE = /^(\d+):([0-9a-f-]{36}):([A-Za-z]+):(\d+)$/;

export function studioCaption(caption: string): string {
  const trimmed = caption.trim();
  if (trimmed.length <= ARCADE_LABEL_MAX_LENGTH) return trimmed;
  return `${trimmed.slice(0, ARCADE_LABEL_MAX_LENGTH)}…`;
}

export function compactCanvasLabel(caption: string): string {
  const trimmed = caption.trim();
  if (trimmed === '') return '';

  const elementId = ELEMENT_ID_RE.exec(trimmed);
  if (elementId !== null) return `${elementId[3] ?? ''}:${elementId[4] ?? ''}`;

  const uuid = UUID_RE.exec(trimmed);
  if (uuid !== null) {
    const suffix = trimmed.slice(Math.max(uuid.index + uuid[0].length, trimmed.length - 6));
    return suffix === '' ? `${uuid[0].slice(0, 8)}…` : `${uuid[0].slice(0, 8)}…${suffix}`;
  }

  const longHex = LONG_HEX_RE.exec(trimmed);
  if (longHex !== null) return `${trimmed.slice(0, 8)}…${trimmed.slice(-6)}`;

  return studioCaption(trimmed);
}

/** ArcadeDB Studio's node-size rule. ClientNode.size is a radius carrying degree signal. */
export function studioNodeDiameter(label: string, degreeRadius: number): number {
  const captionDiameter = Math.min(
    ARCADE_NODE_BASE_DIAMETER + 6 * label.length,
    ARCADE_NODE_MAX_DIAMETER,
  );
  return Math.max(degreeRadius * 2, captionDiameter);
}

function relativeLuminance(hex: string): number | undefined {
  const match = /^#([0-9a-f]{6})$/i.exec(hex);
  if (match === null) return undefined;
  const packed = match[1] ?? '';
  const channel = (offset: number) => {
    const value = Number.parseInt(packed.slice(offset, offset + 2), 16) / 255;
    return value <= 0.04045 ? value / 12.92 : ((value + 0.055) / 1.055) ** 2.4;
  };
  return 0.2126 * channel(0) + 0.7152 * channel(2) + 0.0722 * channel(4);
}

export function contrastTextColor(background: string): '#101114' | '#FFFFFF' {
  const luminance = relativeLuminance(background);
  if (luminance === undefined) return '#FFFFFF';
  const darkContrast = (luminance + 0.05) / 0.05;
  const lightContrast = 1.05 / (luminance + 0.05);
  return darkContrast >= lightContrast ? '#101114' : '#FFFFFF';
}

export function buildArcadeElements(
  nodes: readonly ClientNode[],
  edges: readonly ClientEdge[],
): cytoscape.ElementDefinition[] {
  const nodeIDs = new Set(nodes.map((node) => node.id));
  const elements: cytoscape.ElementDefinition[] = nodes.map((node) => {
    const label = compactCanvasLabel(node.caption);
    return {
      group: 'nodes',
      data: {
        id: node.id,
        label,
        size: studioNodeDiameter(label, node.size),
        color: node.color,
        textColor: contrastTextColor(node.color),
      },
    };
  });

  for (const edge of edges) {
    if (!nodeIDs.has(edge.source) || !nodeIDs.has(edge.target)) continue;
    elements.push({
      group: 'edges',
      data: {
        id: edge.id,
        source: edge.source,
        target: edge.target,
        label: studioCaption(edge.label),
        color: edge.color,
      },
    });
  }
  return elements;
}

/** Studio's graph stylesheet adapted to Aura's dark tokens. */
export const ARCADE_GRAPH_STYLE: cytoscape.StylesheetJson = [
  {
    selector: 'node',
    style: {
      label: 'data(label)',
      width: 'data(size)',
      height: 'data(size)',
      shape: 'ellipse',
      'background-color': 'data(color)',
      'border-width': 1,
      'border-color': '#64748B',
      color: 'data(textColor)',
      'font-family': '"Atkinson Hyperlegible Next", ui-sans-serif, system-ui, sans-serif',
      'font-size': 16,
      'font-weight': 600,
      'text-valign': 'center',
      'text-halign': 'center',
      'text-wrap': 'wrap',
      'text-max-width': 'data(size)',
      'text-overflow-wrap': 'whitespace',
      'z-index-compare': 'manual',
      'z-index': 2,
      'overlay-opacity': 0,
    },
  },
  {
    selector: 'edge',
    style: {
      width: 1.4,
      label: 'data(label)',
      color: 'data(color)',
      'font-family': '"Atkinson Hyperlegible Next", ui-sans-serif, system-ui, sans-serif',
      'font-size': 12,
      'font-weight': 600,
      'line-color': 'data(color)',
      'target-arrow-color': 'data(color)',
      'target-arrow-shape': 'triangle',
      'arrow-scale': 0.85,
      'curve-style': 'bezier',
      'text-rotation': 'autorotate',
      'text-outline-color': '#131314',
      'text-outline-width': 3,
      'text-outline-opacity': 1,
      'z-index-compare': 'manual',
      'z-index': 1,
      'overlay-opacity': 0,
    },
  },
  {
    selector: 'node.path-node',
    style: {
      'background-color': '#8B5CF6',
      'border-color': '#E6E8EC',
      'border-width': 3,
      'z-index': 5,
    },
  },
  {
    selector: 'edge.path-edge',
    style: {
      'line-color': '#C4B5FD',
      'target-arrow-color': '#C4B5FD',
      'z-index': 4,
    },
  },
  {
    selector: '.dimmed',
    style: { opacity: 0.32 },
  },
  {
    selector: 'node:selected',
    style: {
      'border-color': '#FFFFFF',
      'border-width': 4,
    },
  },
];
