import { useState } from 'react';
import { Network, X } from 'lucide-react';
import { useTranslation } from 'react-i18next';
import { useSourceExplorer } from '../chat/displays/sourceExplorerControls';
import { isSourceNode, sourcesForNode } from './nodeSources';
import { nodeDisplayName } from './graphIntent';
import type { GraphNode } from './types';
import { Button } from '@/components/ui/button';

// NodeInspector — the RIGHT pane of the Frame-06 workspace. It renders the selected node's
// label / properties (as TEXT, never HTML — T-27-04 / HARDEN-08) / degree / neighbors /
// citations, plus the READ-ONLY action set (D-04/D-09): pin path (client-side highlight),
// open source, expand neighbors, and show the display-only ArcadeDB SQL.
// `add note` is NOT rendered — graph WRITES are deferred to Phase 29 (T-27-02). The citations
// list is a DISPLAYED field (the node.citations contract), DISTINCT from the open-source ACTION.
//
// The open-source action reuses the existing read-only SourceExplorerSheet via
// useSourceExplorer().openSources — it builds NO new sheet. Opening the inspector moves focus
// into it; the close button returns focus to the originating node-list item (GraphExplorer
// passes onClose). Every property value renders inside <span>{value}</span> so React escapes it.
// sourcesForNode/isSourceNode are the pure cross-link projection (nodeSources.ts).

function propEntries(node: GraphNode): readonly [string, string][] {
  const props = node.props ?? {};
  return Object.entries(props)
    .filter(([key]) => key !== 'embedding')
    .map(([key, value]) => [key, typeof value === 'string' ? value : JSON.stringify(value)]);
}

export interface NodeInspectorProps {
  readonly node: GraphNode | undefined;
  readonly query: string;
  readonly onExpand: (node: GraphNode) => void;
  readonly onPinPath: (node: GraphNode) => void;
  readonly onClose: () => void;
}

export function NodeInspector({ node, query, onExpand, onPinPath, onClose }: NodeInspectorProps) {
  const { t } = useTranslation();
  const { openSources } = useSourceExplorer();
  const [queryOpen, setQueryOpen] = useState(false);

  if (node === undefined) {
    return (
      <div className="grid h-full place-items-center p-6 text-center">
        <div className="flex flex-col gap-2">
          <h3 className="font-display text-[18px] font-semibold text-text">
            {t('graph.inspector.emptyHeading')}
          </h3>
          <p className="text-[15.5px] text-text-muted">{t('graph.inspector.emptyBody')}</p>
        </div>
      </div>
    );
  }

  const labels = node.labels ?? [];
  const citations = node.citations ?? [];
  const props = propEntries(node);

  return (
    <div className="flex h-full min-h-0 flex-col gap-4 overflow-y-auto p-4">
      <header className="flex items-start justify-between gap-2">
        <h3 className="font-display text-[18px] font-semibold text-text">
          {nodeDisplayName(node)}
        </h3>
        <Button
          type="button"
          variant="ghost"
          size="icon"
          onClick={onClose}
          aria-label={t('display.closeAria')}
          className="text-text-muted hover:text-text"
        >
          <X data-icon aria-hidden="true" />
        </Button>
      </header>

      <dl className="flex flex-col gap-2 text-[15.5px]">
        <div className="flex gap-2">
          <dt className="font-semibold text-text-muted">{t('graph.inspector.label')}</dt>
          <dd className="text-text">{labels.join(', ')}</dd>
        </div>
        <div className="flex gap-2">
          <dt className="font-semibold text-text-muted">{t('graph.inspector.degree')}</dt>
          <dd className="text-text">{node.degree ?? 0}</dd>
        </div>
      </dl>

      {props.length > 0 ? (
        <section className="flex flex-col gap-1">
          <h4 className="text-[13px] font-semibold uppercase tracking-wide text-text-muted">
            {t('graph.inspector.properties')}
          </h4>
          <dl className="flex flex-col gap-1 font-mono text-[13px]">
            {props.map(([key, value]) => (
              <div key={key} className="flex flex-col">
                <dt className="text-text-muted">{key}</dt>
                {/* Rendered as TEXT — React escapes it; never dangerouslySetInnerHTML. */}
                <dd className="break-words text-text">{value}</dd>
              </div>
            ))}
          </dl>
        </section>
      ) : null}

      <section className="flex flex-col gap-1">
        <h4 className="text-[13px] font-semibold uppercase tracking-wide text-text-muted">
          {t('graph.inspector.citations')}
        </h4>
        {citations.length > 0 ? (
          <ul className="flex flex-col gap-1 text-[15.5px]">
            {citations.map((citation, i) => (
              <li key={`${citation}-${String(i)}`} className="break-words text-text">
                {citation}
              </li>
            ))}
          </ul>
        ) : (
          <p className="text-[15.5px] text-text-muted">{t('graph.inspector.noCitations')}</p>
        )}
      </section>

      <footer className="mt-auto flex flex-col gap-2">
        <Button
          type="button"
          variant="outline"
          onClick={() => {
            onExpand(node);
          }}
        >
          <Network data-icon="inline-start" aria-hidden="true" />
          {t('graph.cta.expand')}
        </Button>
        <Button
          type="button"
          variant="outline"
          onClick={() => {
            onPinPath(node);
          }}
        >
          {t('graph.inspector.pinPath')}
        </Button>
        {isSourceNode(node) ? (
          <Button
            type="button"
            variant="outline"
            onClick={() => {
              openSources(sourcesForNode(node), node.ref_id ?? node.id);
            }}
            className="text-accent-text hover:text-accent-text"
          >
            {t('graph.inspector.openSource')}
          </Button>
        ) : null}
        {query.length > 0 ? (
          <Button
            type="button"
            variant="outline"
            aria-expanded={queryOpen}
            onClick={() => {
              setQueryOpen((v) => !v);
            }}
          >
            {t('graph.inspector.showQuery')}
          </Button>
        ) : null}
        {queryOpen && query.length > 0 ? (
          <div className="flex flex-col gap-1">
            {/* Read-only — rendered as TEXT, never an editable input (D-04/D-09). */}
            <pre className="overflow-x-auto whitespace-pre-wrap break-words rounded-md bg-bg p-2 font-mono text-[13px] text-text">
              {query}
            </pre>
            <p className="text-[13px] text-text-muted">{t('graph.query.note')}</p>
          </div>
        ) : null}
      </footer>
    </div>
  );
}
