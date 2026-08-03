import { describe, expect, it } from 'vitest';
import {
  ARCADE_GRAPH_STYLE,
  ARCADE_LABEL_MAX_LENGTH,
  buildArcadeElements,
  compactCanvasLabel,
  contrastTextColor,
  studioCaption,
  studioNodeDiameter,
} from '../ArcadeGraphCanvas_data';
import type { ClientEdge, ClientNode } from '../types';

const NODES: readonly ClientNode[] = [
  { id: 'n1', caption: 'ArcadeDB', color: '#7FC9C3', size: 8, labels: ['Entity'] },
  { id: 'n2', caption: '5889', color: '#AECBFA', size: 6, labels: ['Entity'] },
];

const EDGES: readonly ClientEdge[] = [
  { id: 'e1', source: 'n1', target: 'n2', label: 'FACT', color: '#FDD663' },
  {
    id: 'dangling',
    source: 'n1',
    target: 'missing',
    label: 'DROPPED',
    color: '#AECBFA',
  },
];

describe('ArcadeDB Studio graph projection', () => {
  it("uses Studio's 25 + 6×length diameter rule with a 200 px cap", () => {
    expect(studioNodeDiameter('', 4)).toBe(25);
    expect(studioNodeDiameter('5889', 4)).toBe(49);
    expect(studioNodeDiameter('x'.repeat(100), 4)).toBe(200);
    expect(studioNodeDiameter('x', 20)).toBe(40);
  });

  it('caps human captions and compacts technical identifiers', () => {
    expect(studioCaption('  Aura memory  ')).toBe('Aura memory');
    expect(studioCaption('x'.repeat(ARCADE_LABEL_MAX_LENGTH))).toBe(
      'x'.repeat(ARCADE_LABEL_MAX_LENGTH),
    );
    expect(studioCaption('x'.repeat(ARCADE_LABEL_MAX_LENGTH + 1))).toBe(
      `${'x'.repeat(ARCADE_LABEL_MAX_LENGTH)}…`,
    );
    expect(compactCanvasLabel('4:21de867b-135a-43b0-9598-b82a63a99e7d:d:63')).toBe('d:63');
    expect(compactCanvasLabel('21de867b-135a-43b0-9598-b82a63a99e7d')).toBe('21de867b…');
    expect(compactCanvasLabel('  web_search  ')).toBe('web_search');
    expect(compactCanvasLabel('   ')).toBe('');
    expect(compactCanvasLabel('21de867b-135a-43b0-9598-b82a63a99e7d:suffix')).toBe(
      '21de867b…suffix',
    );
    expect(compactCanvasLabel('0123456789abcdef01234567')).toBe('01234567…234567');
    expect(compactCanvasLabel('web_search')).toBe('web_search');
  });

  it('selects readable text colors for light and dark node fills', () => {
    expect(contrastTextColor('#AECBFA')).toBe('#101114');
    expect(contrastTextColor('#000000')).toBe('#FFFFFF');
    expect(contrastTextColor('invalid')).toBe('#FFFFFF');
  });

  it('builds labelled nodes, directed edges and drops dangling relationships', () => {
    const elements = buildArcadeElements(NODES, EDGES);
    expect(elements).toHaveLength(3);
    expect(elements[0]).toMatchObject({
      group: 'nodes',
      data: { id: 'n1', label: 'ArcadeDB', size: 73, color: '#7FC9C3', textColor: '#101114' },
    });
    expect(elements[2]).toMatchObject({
      group: 'edges',
      data: {
        id: 'e1',
        source: 'n1',
        target: 'n2',
        label: 'FACT',
        color: '#FDD663',
      },
    });
  });

  it('pins native centered labels, arrows and outlined edge captions in style', () => {
    expect(ARCADE_GRAPH_STYLE).toEqual([
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
      { selector: '.dimmed', style: { opacity: 0.32 } },
      {
        selector: 'node:selected',
        style: { 'border-color': '#FFFFFF', 'border-width': 4 },
      },
    ]);
  });
});
