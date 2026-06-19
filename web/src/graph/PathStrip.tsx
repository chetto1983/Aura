import { useRef } from 'react';
import { useTranslation } from 'react-i18next';
import type { GraphEdge, GraphNode } from './types';

// PathStrip — the surface below the canvas that doubles as (1) the D-10 path strip (the selected
// evidence path as an ordered run of `node —REL→ node` steps) and (2) the a11y PARALLEL DOM: a
// focusable, ordered node list (role="list") + an edge list that mirror the graph for screen
// readers and keyboard users (the non-WebGL fallback, D-03). `Enter` on a node-list item selects
// it = the same effect as a canvas click (hover is never the only access path), and arrow keys
// roam the list. Every interactive item is a 44px touch target. The canvas pixels are opaque to
// assistive tech — this DOM mirror is the standards-compliant access route.

function nodeCaption(node: GraphNode): string {
  return node.caption ?? node.id;
}

export interface PathStripProps {
  readonly nodes: readonly GraphNode[];
  readonly edges: readonly GraphEdge[];
  readonly pinnedPath: ReadonlySet<string>;
  readonly onSelectNode: (node: GraphNode) => void;
}

export function PathStrip({ nodes, edges, pinnedPath, onSelectNode }: PathStripProps) {
  const { t } = useTranslation();
  const itemRefs = useRef<(HTMLButtonElement | null)[]>([]);

  const pathNodes = nodes.filter((n) => pinnedPath.has(n.id));
  const captionById = new Map(nodes.map((n) => [n.id, nodeCaption(n)]));

  function focusItem(index: number) {
    const clamped = Math.max(0, Math.min(index, nodes.length - 1));
    itemRefs.current[clamped]?.focus();
  }

  function onKeyDown(event: React.KeyboardEvent, index: number, node: GraphNode) {
    if (event.key === 'ArrowDown' || event.key === 'ArrowRight') {
      event.preventDefault();
      focusItem(index + 1);
    } else if (event.key === 'ArrowUp' || event.key === 'ArrowLeft') {
      event.preventDefault();
      focusItem(index - 1);
    } else if (event.key === 'Enter' || event.key === ' ') {
      event.preventDefault();
      onSelectNode(node);
    }
  }

  return (
    <div className="flex flex-col gap-3 border-t border-border bg-surface p-4">
      <section aria-label={t('graph.path.label')}>
        <h3 className="mb-1 text-[13px] font-semibold uppercase tracking-wide text-text-muted">
          {t('graph.path.label')}
        </h3>
        {pathNodes.length > 0 ? (
          <ol className="flex flex-wrap items-center gap-1 text-[13px]">
            {pathNodes.map((node, i) => (
              <li key={node.id} className="flex items-center gap-1">
                <span className="rounded bg-accent px-2 py-0.5 font-semibold text-on-accent">
                  {nodeCaption(node)}
                </span>
                {i < pathNodes.length - 1 ? (
                  <span aria-hidden="true" className="text-text-muted">
                    —→
                  </span>
                ) : null}
              </li>
            ))}
          </ol>
        ) : (
          <p className="text-[13px] text-text-muted">{t('graph.path.empty')}</p>
        )}
      </section>

      <section>
        <h3 className="mb-1 text-[13px] font-semibold uppercase tracking-wide text-text-muted">
          {t('graph.a11y.nodeList', { count: nodes.length })}
        </h3>
        <div role="list" className="flex max-h-40 flex-col gap-1 overflow-y-auto">
          {nodes.map((node, i) => (
            <div key={node.id} role="listitem">
              <button
                type="button"
                ref={(el) => {
                  itemRefs.current[i] = el;
                }}
                onKeyDown={(e) => {
                  onKeyDown(e, i, node);
                }}
                onClick={() => {
                  onSelectNode(node);
                }}
                aria-pressed={pinnedPath.has(node.id)}
                className={`flex min-h-[44px] w-full items-center justify-between rounded-md border px-3 py-1 text-left text-[15.5px] ${
                  pinnedPath.has(node.id)
                    ? 'border-accent bg-accent text-on-accent'
                    : 'border-border bg-surface-2 text-text'
                }`}
              >
                <span className="break-words">{nodeCaption(node)}</span>
                <span className="ml-2 text-[13px] text-text-muted">
                  {(node.labels ?? [])[0] ?? ''}
                </span>
              </button>
            </div>
          ))}
        </div>
      </section>

      <section>
        <h3 className="mb-1 text-[13px] font-semibold uppercase tracking-wide text-text-muted">
          {t('graph.a11y.edgeList', { count: edges.length })}
        </h3>
        <div role="list" className="flex max-h-32 flex-col gap-1 overflow-y-auto text-[13px]">
          {edges.map((edge) => (
            <div
              key={edge.id}
              role="listitem"
              className="flex flex-wrap items-center gap-1.5 rounded-md px-1 py-0.5"
            >
              <span className="text-text">{captionById.get(edge.source) ?? edge.source}</span>
              <span className="rounded bg-surface-2 px-1.5 py-0.5 text-[12px] font-medium text-accent-text">
                {edge.rel_type ?? ''}
              </span>
              <span aria-hidden="true" className="text-text-muted">
                →
              </span>
              <span className="text-text">{captionById.get(edge.target) ?? edge.target}</span>
            </div>
          ))}
        </div>
      </section>
    </div>
  );
}
