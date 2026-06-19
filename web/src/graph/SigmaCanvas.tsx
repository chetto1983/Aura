import { Component, useEffect, type ErrorInfo, type ReactNode } from 'react';
import Graph from 'graphology';
import { SigmaContainer, useLoadGraph, useRegisterEvents, useSigma } from '@react-sigma/core';
import '@react-sigma/core/lib/style.css';
import forceAtlas2 from 'graphology-layout-forceatlas2';
import { useTranslation } from 'react-i18next';
import type { ClientEdge, ClientNode } from './types';
import { pinnedEdgeReducer, pinnedNodeReducer } from './SigmaCanvas_reducers';

// SigmaCanvas is the ONLY module in the cockpit that imports the WebGL renderer stack
// (sigma / @react-sigma/core / graphology / graphology-layout-forceatlas2). jsdom has no
// WebGL (Pitfall 4), so this component is exercised ONLY by the Task-4 Playwright e2e in a
// real browser; the Vitest component test (__tests__/SigmaCanvas.test.tsx) mocks
// @react-sigma/core and asserts the wrapper a11y + that node/edge props flow in. All the
// testable graph LOGIC lives in the pure plan-03 core (graphIntent.ts) + the pure reducers
// (SigmaCanvas_reducers.ts) — this file is the thin renderer boundary only.
//
// It owns rendering ONLY. The intent/seed/fetch/selection/path state lives in GraphExplorer
// (Task 2/3) and is handed in as props. The Pitfall-1 resize-remount crash workaround is the
// `sigmaKey` prop the parent increments on mode-swap + inspector open/close — passed straight
// to the SigmaContainer `key`, forcing a clean remount so the GPU programs recompile.

const POSITION_CACHE = new Map<string, { x: number; y: number }>();
let lastLayoutKey = '';

function layoutDataKey(nodes: readonly ClientNode[], edges: readonly ClientEdge[]): string {
  return `${nodes.map((n) => n.id).join(',')}|${String(edges.length)}`;
}

interface LoaderProps {
  readonly nodes: readonly ClientNode[];
  readonly edges: readonly ClientEdge[];
  readonly reducedMotion: boolean;
}

/**
 * GraphLoader builds the graphology Graph from the plan-03 client projection, runs
 * ForceAtlas2 ONCE per distinct dataset (cached positions so a re-render does not re-layout —
 * Pitfall: never re-layout on every render), and loads it into sigma. barnesHutOptimize kicks
 * in above 50 nodes (the llm_wiki proven setting). prefers-reduced-motion shortens the settle.
 */
function GraphLoader({ nodes, edges, reducedMotion }: LoaderProps) {
  const loadGraph = useLoadGraph();

  useEffect(() => {
    const dataKey = layoutDataKey(nodes, edges);
    const needsLayout = dataKey !== lastLayoutKey;
    const graph = new Graph();

    for (const node of nodes) {
      const cached = POSITION_CACHE.get(node.id);
      graph.addNode(node.id, {
        x: cached?.x ?? Math.random() * 100,
        y: cached?.y ?? Math.random() * 100,
        size: node.size,
        color: node.color,
        label: node.caption,
      });
    }
    for (const edge of edges) {
      if (!graph.hasNode(edge.source) || !graph.hasNode(edge.target)) continue;
      if (graph.hasEdge(edge.id)) continue;
      graph.addEdgeWithKey(edge.id, edge.source, edge.target, {
        label: edge.label,
        size: 1.5,
        color: '#5F6368',
      });
    }

    if (needsLayout && nodes.length > 1) {
      forceAtlas2.assign(graph, {
        iterations: reducedMotion ? 50 : 150,
        settings: {
          ...forceAtlas2.inferSettings(graph),
          barnesHutOptimize: nodes.length > 50,
        },
      });
      lastLayoutKey = dataKey;
      graph.forEachNode((id, attrs) => {
        POSITION_CACHE.set(id, { x: attrs.x as number, y: attrs.y as number });
      });
    }

    loadGraph(graph);
  }, [loadGraph, nodes, edges, reducedMotion]);

  return null;
}

interface EventProps {
  readonly onNodeClick: (nodeId: string) => void;
}

/** EventHandler binds clickNode (opens the inspector) + enter/leave (pointer cursor only). */
function EventHandler({ onNodeClick }: EventProps) {
  const registerEvents = useRegisterEvents();
  const sigma = useSigma();

  useEffect(() => {
    registerEvents({
      clickNode: ({ node }) => {
        onNodeClick(node);
      },
      enterNode: () => {
        sigma.getContainer().style.cursor = 'pointer';
      },
      leaveNode: () => {
        sigma.getContainer().style.cursor = 'default';
      },
    });
  }, [registerEvents, sigma, onNodeClick]);

  return null;
}

interface HighlightProps {
  readonly pinnedPath: ReadonlySet<string>;
}

/**
 * HighlightManager installs the pinned-path node/edge reducers on the live sigma instance and
 * refreshes. The reducers (SigmaCanvas_reducers.ts) dim everything off the pinned evidence path
 * and accent what is on it (D-08/D-10). An empty pinnedPath restores the label-family fills.
 */
function HighlightManager({ pinnedPath }: HighlightProps) {
  const sigma = useSigma();

  useEffect(() => {
    sigma.setSetting('nodeReducer', (nodeId, data) =>
      pinnedNodeReducer(nodeId, data, pinnedPath),
    );
    sigma.setSetting('edgeReducer', (edgeId, data) => {
      const graph = sigma.getGraph();
      const source = graph.source(edgeId);
      const target = graph.target(edgeId);
      return pinnedEdgeReducer(source, target, data, pinnedPath);
    });
    sigma.refresh();
  }, [sigma, pinnedPath]);

  return null;
}

class CanvasErrorBoundary extends Component<
  { readonly fallback: ReactNode; readonly children: ReactNode },
  { hasError: boolean }
> {
  constructor(props: { readonly fallback: ReactNode; readonly children: ReactNode }) {
    super(props);
    this.state = { hasError: false };
  }

  static getDerivedStateFromError(): { hasError: boolean } {
    return { hasError: true };
  }

  // A WebGL-context-loss must never white-screen the cockpit (Pitfall 1) — log + fall back.
  componentDidCatch(error: Error, info: ErrorInfo): void {
    console.warn('SigmaCanvas render error', error.message, info.componentStack);
  }

  render(): ReactNode {
    if (this.state.hasError) return this.props.fallback;
    return this.props.children;
  }
}

function usePrefersReducedMotion(): boolean {
  if (typeof window === 'undefined' || typeof window.matchMedia !== 'function') return false;
  return window.matchMedia('(prefers-reduced-motion: reduce)').matches;
}

export interface SigmaCanvasProps {
  readonly nodes: readonly ClientNode[];
  readonly edges: readonly ClientEdge[];
  readonly pinnedPath: ReadonlySet<string>;
  /** Increment on mode-swap + inspector open/close to remount sigma (Pitfall 1). */
  readonly sigmaKey: number;
  readonly onNodeClick: (nodeId: string) => void;
}

/**
 * SigmaCanvas — the WebGL renderer wrapper. role="img" + an aria-label naming the current
 * view (graph.a11y.canvasName) is the accessible name; the real interaction lives in the
 * parallel DOM (PathStrip + node/edge list), never the canvas pixels (D-03). The container is
 * remounted via key={sigmaKey} on every layout change so the GPU programs recompile cleanly.
 */
export function SigmaCanvas({ nodes, edges, pinnedPath, sigmaKey, onNodeClick }: SigmaCanvasProps) {
  const { t } = useTranslation();
  const reducedMotion = usePrefersReducedMotion();
  const canvasName = t('graph.a11y.canvasName', {
    nodeCount: nodes.length,
    edgeCount: edges.length,
  });

  return (
    <div role="img" aria-label={canvasName} className="relative h-full w-full bg-bg">
      <CanvasErrorBoundary
        fallback={
          <div className="grid h-full place-items-center p-4 text-sm text-text-muted">
            {t('graph.error.query')}
          </div>
        }
      >
        <SigmaContainer
          key={sigmaKey}
          style={{ height: '100%', width: '100%', background: 'transparent' }}
          settings={{ renderEdgeLabels: true, labelRenderedSizeThreshold: 6 }}
        >
          <GraphLoader nodes={nodes} edges={edges} reducedMotion={reducedMotion} />
          <EventHandler onNodeClick={onNodeClick} />
          <HighlightManager pinnedPath={pinnedPath} />
        </SigmaContainer>
      </CanvasErrorBoundary>
    </div>
  );
}
