import { useEffect, useRef, useState } from 'react';
import cytoscape, { type Core, type EventObjectNode, type Layouts } from 'cytoscape';
import fcose from 'cytoscape-fcose';
import { useTranslation } from 'react-i18next';
import { ARCADE_GRAPH_STYLE, buildArcadeElements, canvasChange } from './ArcadeGraphCanvas_data';
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

const LAYOUT_SPACING = {
  padding: 14,
  nodeSeparation: 50,
  idealEdgeLength: 75,
  nodeRepulsion: 7000,
};

// A new read replaces the canvas: every node is placed afresh.
function replaceGraph(
  cy: Core,
  elements: cytoscape.ElementDefinition[],
  animate: boolean,
): Layouts | undefined {
  cy.startBatch();
  cy.elements().remove();
  cy.add(elements);
  cy.endBatch();
  if (!elements.some((element) => element.group === 'nodes')) return undefined;
  const layout = cy.layout({
    name: 'fcose',
    quality: 'default',
    randomize: true,
    animate,
    animationDuration: animate ? 500 : 0,
    fit: true,
    packComponents: true,
    tile: true,
    tilingPaddingHorizontal: 16,
    tilingPaddingVertical: 16,
    ...LAYOUT_SPACING,
  } as cytoscape.LayoutOptions);
  layout.run();
  return layout;
}

// An expansion grows the canvas: the nodes already on it stay where the reader left them
// (fCoSE's fixedNodeConstraint, which needs randomize false and quality "proof"), and the new
// ones start at the expanded node and settle around it.
function growGraph(
  cy: Core,
  fresh: cytoscape.ElementDefinition[],
  anchorId: string | undefined,
  animate: boolean,
): Layouts {
  const anchor = anchorId === undefined ? cy.collection() : cy.getElementById(anchorId);
  const box = cy.nodes().boundingBox();
  const origin = anchor.nonempty()
    ? { ...anchor.position() }
    : { x: (box.x1 + box.x2) / 2, y: (box.y1 + box.y2) / 2 };
  const fixedNodeConstraint = cy
    .nodes()
    .map((node) => ({ nodeId: node.id(), position: { ...node.position() } }));
  cy.add(
    fresh.map((element) =>
      element.group === 'nodes' ? { ...element, position: { ...origin } } : element,
    ),
  );
  const layout = cy.layout({
    name: 'fcose',
    quality: 'proof',
    randomize: false,
    animate,
    animationDuration: animate ? 500 : 0,
    fit: false,
    fixedNodeConstraint,
    ...LAYOUT_SPACING,
  } as cytoscape.LayoutOptions);
  layout.run();
  if (anchor.nonempty()) cy.animate({ center: { eles: anchor } }, { duration: animate ? 300 : 0 });
  return layout;
}

export interface ArcadeGraphCanvasProps {
  readonly nodes: readonly ClientNode[];
  readonly edges: readonly ClientEdge[];
  readonly pinnedPath: ReadonlySet<string>;
  readonly onNodeClick: (nodeId: string) => void;
  /** Double-click expands a node, as in ArcadeDB Studio. */
  readonly onNodeDoubleClick: (nodeId: string) => void;
  /** The node the last expansion grew from: new nodes start there and the view centres on it. */
  readonly focusId: string | undefined;
}

/** Read-only renderer using the same Cytoscape + fCoSE stack as ArcadeDB Studio. */
export function ArcadeGraphCanvas({
  nodes,
  edges,
  pinnedPath,
  onNodeClick,
  onNodeDoubleClick,
  focusId,
}: ArcadeGraphCanvasProps) {
  const { t } = useTranslation();
  const containerRef = useRef<HTMLDivElement>(null);
  const cytoscapeRef = useRef<Core | undefined>(undefined);
  const layoutRef = useRef<Layouts | undefined>(undefined);
  const clickRef = useRef(onNodeClick);
  const doubleClickRef = useRef(onNodeDoubleClick);
  const focusRef = useRef(focusId);
  const [renderFailed, setRenderFailed] = useState(false);
  const reducedMotion = usePrefersReducedMotion();

  const canvasName = t('graph.a11y.canvasName', {
    nodeCount: nodes.length,
    edgeCount: edges.length,
  });

  useEffect(() => {
    clickRef.current = onNodeClick;
    doubleClickRef.current = onNodeDoubleClick;
    focusRef.current = focusId;
  }, [onNodeClick, onNodeDoubleClick, focusId]);

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
      cy.on('dbltap', 'node', (event: EventObjectNode) => {
        doubleClickRef.current(event.target.id());
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

    const elements = buildArcadeElements(nodes, edges);
    const change = canvasChange(
      cy.nodes().map((node) => node.id()),
      new Set(cy.elements().map((element) => element.id())),
      elements,
    );
    if (change.kind === 'keep') return;
    layoutRef.current?.stop();
    layoutRef.current =
      change.kind === 'grow'
        ? growGraph(cy, change.fresh, focusRef.current, !reducedMotion)
        : replaceGraph(cy, elements, !reducedMotion);
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
