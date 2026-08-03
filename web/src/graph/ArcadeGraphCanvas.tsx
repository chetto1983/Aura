import { useEffect, useRef, useState } from 'react';
import cytoscape, { type Core, type EventObjectNode, type Layouts } from 'cytoscape';
import fcose from 'cytoscape-fcose';
import { useTranslation } from 'react-i18next';
import { ARCADE_GRAPH_STYLE, buildArcadeElements } from './ArcadeGraphCanvas_data';
import type { ClientEdge, ClientNode } from './types';

cytoscape.use(fcose);

function usePrefersReducedMotion(): boolean {
  if (typeof window === 'undefined' || typeof window.matchMedia !== 'function') return false;
  return window.matchMedia('(prefers-reduced-motion: reduce)').matches;
}

function applyPinnedPath(cy: Core, pinnedPath: ReadonlySet<string>): void {
  cy.elements().removeClass('dimmed path-node path-edge');
  if (pinnedPath.size === 0) return;

  cy.nodes().addClass('dimmed');
  cy.edges().addClass('dimmed');
  for (const nodeID of pinnedPath) {
    cy.getElementById(nodeID).removeClass('dimmed').addClass('path-node');
  }
  cy.edges().forEach((edge) => {
    if (!pinnedPath.has(edge.source().id()) || !pinnedPath.has(edge.target().id())) return;
    edge.removeClass('dimmed').addClass('path-edge');
  });
}

export interface ArcadeGraphCanvasProps {
  readonly nodes: readonly ClientNode[];
  readonly edges: readonly ClientEdge[];
  readonly pinnedPath: ReadonlySet<string>;
  readonly onNodeClick: (nodeId: string) => void;
}

/** Read-only renderer using the same Cytoscape + fCoSE stack as ArcadeDB Studio. */
export function ArcadeGraphCanvas({
  nodes,
  edges,
  pinnedPath,
  onNodeClick,
}: ArcadeGraphCanvasProps) {
  const { t } = useTranslation();
  const containerRef = useRef<HTMLDivElement>(null);
  const cytoscapeRef = useRef<Core | undefined>(undefined);
  const layoutRef = useRef<Layouts | undefined>(undefined);
  const clickRef = useRef(onNodeClick);
  const [renderFailed, setRenderFailed] = useState(false);
  const reducedMotion = usePrefersReducedMotion();

  const canvasName = t('graph.a11y.canvasName', {
    nodeCount: nodes.length,
    edgeCount: edges.length,
  });

  useEffect(() => {
    clickRef.current = onNodeClick;
  }, [onNodeClick]);

  useEffect(() => {
    const container = containerRef.current;
    if (container === null) return undefined;

    try {
      const cy = cytoscape({
        container,
        elements: [],
        style: ARCADE_GRAPH_STYLE,
        minZoom: 0.08,
        maxZoom: 4,
        boxSelectionEnabled: false,
      });
      cytoscapeRef.current = cy;
      cy.on('tap', 'node', (event: EventObjectNode) => {
        clickRef.current(event.target.id());
      });

      const observer = new ResizeObserver(() => {
        cy.resize();
        if (cy.nodes().length > 0) cy.fit(cy.elements(), 30);
      });
      observer.observe(container);

      return () => {
        observer.disconnect();
        layoutRef.current?.stop();
        cytoscapeRef.current = undefined;
        cy.destroy();
      };
    } catch (error) {
      console.warn(
        'ArcadeGraphCanvas render error',
        error instanceof Error ? error.message : String(error),
      );
      const failureTimer = window.setTimeout(() => {
        setRenderFailed(true);
      }, 0);
      return () => {
        window.clearTimeout(failureTimer);
      };
    }
  }, []);

  useEffect(() => {
    const cy = cytoscapeRef.current;
    if (cy === undefined) return;

    layoutRef.current?.stop();
    cy.startBatch();
    cy.elements().remove();
    cy.add(buildArcadeElements(nodes, edges));
    cy.endBatch();

    if (nodes.length === 0) return;
    const layout = cy.layout({
      name: 'fcose',
      quality: 'default',
      randomize: true,
      animate: !reducedMotion,
      animationDuration: reducedMotion ? 0 : 500,
      fit: true,
      padding: 14,
      nodeSeparation: 50,
      idealEdgeLength: 75,
      nodeRepulsion: 7000,
      packComponents: true,
      tile: true,
      tilingPaddingHorizontal: 16,
      tilingPaddingVertical: 16,
    } as cytoscape.LayoutOptions);
    layoutRef.current = layout;
    layout.run();
  }, [nodes, edges, reducedMotion]);

  useEffect(() => {
    const cy = cytoscapeRef.current;
    if (cy !== undefined) applyPinnedPath(cy, pinnedPath);
  }, [pinnedPath, nodes, edges]);

  if (renderFailed) {
    return (
      <div role="status" className="grid h-full place-items-center p-4 text-sm text-text-muted">
        {t('graph.error.query')}
      </div>
    );
  }

  return (
    <div
      role="img"
      aria-label={canvasName}
      className="relative h-full w-full overflow-hidden"
      style={{
        background:
          'radial-gradient(130% 120% at 50% -8%, color-mix(in oklab, var(--color-accent) 14%, var(--color-bg)) 0%, var(--color-bg) 58%)',
      }}
    >
      <div ref={containerRef} data-testid="arcade-graph-canvas" className="h-full w-full" />
    </div>
  );
}
