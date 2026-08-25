import { Suspense, lazy, useCallback, useEffect, useReducer, useRef, useState } from 'react';
import { CheckCircle2, Database, Network, X } from 'lucide-react';
import { useTranslation } from 'react-i18next';
import { fetchGraphSchema, postGraphQuery } from './graphApi';
import {
  initialIntentState,
  intentReducer,
  mergeGraphResults,
  rowsToClientGraph,
  toClientIntent,
  type IntentState,
} from './graphIntent';
import { GraphControls } from './GraphControls';
import { NodeInspector } from './NodeInspector';
import { PathStrip } from './PathStrip';
import type { GraphNode, GraphResult, GraphSchema } from './types';
import { ColumnResizeHandle } from '@/components/ColumnResizeHandle';
import { useColumnResize } from '@/components/useColumnResize';
import { Alert, AlertDescription } from '@/components/ui/alert';
import { Badge } from '@/components/ui/badge';
import { Button } from '@/components/ui/button';

// GraphExplorer is the lazy default export the AppShell mounts when surface==='graph'. Its
// separate Vite chunk keeps Cytoscape out of the main cockpit bundle. It is the three-column
// workspace shell (controls | canvas | inspector) with the path strip below the canvas, and
// owns the intent, selection and path state while the canvas owns rendering and layout.
//
// On mount the authenticated identity's overview loads. An empty result falls back to the
// schema readiness board, never a blank canvas. An HTTP 401 renders a visible auth-
// error state — never a silent blank canvas (B3 / threat T-27-03). Any other rejection renders
// the query/schema error state. The accessible parallel DOM remains available alongside the
// lazy canvas renderer.

const ArcadeGraphCanvas = lazy(() =>
  import('./ArcadeGraphCanvas').then((mod) => ({ default: mod.ArcadeGraphCanvas })),
);

type ViewStatus = 'loading' | 'populated' | 'empty' | 'error-query' | 'error-schema' | 'error-auth';

interface ViewState {
  readonly status: ViewStatus;
  readonly result: GraphResult | undefined;
  readonly schema: GraphSchema | undefined;
  readonly capped: boolean;
}

const INITIAL_VIEW: ViewState = {
  status: 'loading',
  result: undefined,
  schema: undefined,
  capped: false,
};

function isAuthError(err: unknown): boolean {
  return err instanceof Error && err.message === 'HTTP 401';
}

function resultIsEmpty(result: GraphResult): boolean {
  return result.nodes.length === 0 && result.edges.length === 0;
}

function isCapped(result: GraphResult): boolean {
  return result.truncated === true;
}

function EvidenceReadinessBoard({
  schema,
  onRefresh,
}: {
  readonly schema: GraphSchema | undefined;
  readonly onRefresh: () => void;
}) {
  const { t } = useTranslation();
  const labels = schema?.labels ?? [];
  const relTypes = schema?.rel_types ?? [];
  const visibleLabels = labels.slice(0, 6);
  const visibleRelTypes = relTypes.slice(0, 5);
  const hiddenLabelCount = Math.max(0, labels.length - visibleLabels.length);
  const hiddenRelTypeCount = Math.max(0, relTypes.length - visibleRelTypes.length);

  return (
    <section
      aria-label={t('graph.empty.readinessAria')}
      className="flex h-full min-h-0 items-center justify-center overflow-y-auto p-4 text-left sm:p-6"
    >
      <div className="grid w-full max-w-5xl gap-5 lg:grid-cols-[minmax(0,1fr)_18rem]">
        <div className="min-w-0">
          <div className="mb-4 flex flex-wrap items-center gap-2">
            <Badge variant="success" className="gap-1.5">
              <CheckCircle2 aria-hidden="true" />
              {t('graph.empty.schemaOnline')}
            </Badge>
            <Badge variant="secondary">
              {t('graph.empty.nodeTypeCount', { count: labels.length })}
            </Badge>
            <Badge variant="secondary">
              {t('graph.empty.connectionTypeCount', { count: relTypes.length })}
            </Badge>
          </div>
          <h2 className="font-display text-[22px] font-semibold text-text">
            {t('graph.empty.heading')}
          </h2>
          <p className="mt-2 max-w-2xl text-[15px] leading-relaxed text-text-muted">
            {t('graph.empty.body')}
          </p>
          <div className="mt-5 flex flex-wrap gap-2">
            <Button type="button" onClick={onRefresh} className="min-w-0">
              <span className="block min-w-0 overflow-wrap-anywhere">
                {t('graph.cta.refreshMemory')}
              </span>
            </Button>
          </div>
        </div>

        <div className="grid min-w-0 content-start gap-3">
          <div className="rounded-lg border border-border bg-surface/80 p-3">
            <div className="mb-3 flex items-center gap-2 text-[13px] font-semibold uppercase text-text-muted">
              <Database aria-hidden="true" className="size-4 text-accent-text" />
              {t('graph.empty.nodeTypes')}
            </div>
            <div className="flex flex-wrap gap-2">
              {visibleLabels.map((label) => (
                <Badge
                  key={label}
                  variant="secondary"
                  className="max-w-full overflow-wrap-anywhere"
                >
                  {label}
                </Badge>
              ))}
              {hiddenLabelCount > 0 ? <Badge variant="secondary">+{hiddenLabelCount}</Badge> : null}
            </div>
          </div>

          <div className="rounded-lg border border-border bg-surface/80 p-3">
            <div className="mb-3 flex items-center gap-2 text-[13px] font-semibold uppercase text-text-muted">
              <Network aria-hidden="true" className="size-4 text-accent-text" />
              {t('graph.empty.connectionTypes')}
            </div>
            <div className="flex flex-wrap gap-2">
              {visibleRelTypes.map((relType) => (
                <Badge
                  key={relType}
                  variant="secondary"
                  className="max-w-full overflow-wrap-anywhere"
                >
                  {relType}
                </Badge>
              ))}
              {hiddenRelTypeCount > 0 ? (
                <Badge variant="secondary">+{hiddenRelTypeCount}</Badge>
              ) : null}
            </div>
          </div>
        </div>
      </div>
    </section>
  );
}

export default function GraphExplorer() {
  const { t } = useTranslation();
  const [intent, dispatch] = useReducer(intentReducer, undefined, initialIntentState);
  const [view, setView] = useState<ViewState>(INITIAL_VIEW);
  const [selected, setSelected] = useState<GraphNode | undefined>(undefined);
  const [pinnedPath, setPinnedPath] = useState<ReadonlySet<string>>(new Set());
  // Mobile-only: the control pane is a bottom sheet (canvas stays dominant). On lg it is
  // the permanent left column and this flag is inert.
  const [filtersOpen, setFiltersOpen] = useState(false);
  // The filters column is the graph's sidebar, so it gets the chat rail's chrome and the same
  // drag-to-resize as the Settings/Governance rails. Its width was a hard 18rem track, which
  // is exactly the wrong thing on the surface whose labels are user data (node + relation type
  // names come from the graph, not from a translation file).
  const workspaceRef = useRef<HTMLDivElement | null>(null);
  const filtersWidth = useColumnResize({
    originRef: workspaceRef,
    storageKey: 'aura.graph.filtersWidth',
    defaultWidth: 288,
    min: 208,
    max: 448,
  });

  const loadSchemaOverview = useCallback(async () => {
    try {
      const schema = await fetchGraphSchema();
      setView({ status: 'empty', result: undefined, schema, capped: false });
    } catch (err) {
      setView((prev) => ({
        ...prev,
        status: isAuthError(err) ? 'error-auth' : 'error-schema',
      }));
    }
  }, []);

  const runIntent = useCallback(
    async (state: IntentState, append = false) => {
      setView((prev) => ({ ...prev, status: 'loading' }));
      try {
        const result = await postGraphQuery(toClientIntent(state));
        if (resultIsEmpty(result)) {
          if (append) {
            setView((prev) => ({
              ...prev,
              status: prev.result === undefined ? 'empty' : 'populated',
            }));
            return;
          }
          await loadSchemaOverview();
          return;
        }
        setView((prev) => {
          const nextResult =
            append && prev.result !== undefined ? mergeGraphResults(prev.result, result) : result;
          return {
            status: 'populated',
            result: nextResult,
            schema: nextResult.schema,
            capped: isCapped(nextResult),
          };
        });
      } catch (err) {
        if (isAuthError(err)) {
          // B3 / T-27-03: an expired session is a VISIBLE auth-error state, never a blank canvas.
          setView((prev) => ({ ...prev, status: 'error-auth' }));
          return;
        }
        setView((prev) => ({ ...prev, status: 'error-query' }));
      }
    },
    [loadSchemaOverview],
  );

  // The graph is scoped only by the authenticated identity. Conversation state is
  // deliberately absent from this component and cannot trigger or narrow a read.
  useEffect(() => {
    void runIntent(initialIntentState());
    setSelected(undefined);
    setPinnedPath(new Set());
  }, [runIntent]);

  const refreshGraph = useCallback(() => {
    const next = intentReducer(intent, { kind: 'refresh' });
    dispatch({ kind: 'refresh' });
    void runIntent(next);
  }, [intent, runIntent]);

  const onToggleLabel = useCallback(
    (label: string) => {
      const next = intentReducer(intent, { kind: 'toggleLabel', label });
      dispatch({ kind: 'toggleLabel', label });
      void runIntent(next);
    },
    [intent, runIntent],
  );
  const onToggleRelType = useCallback(
    (relType: string) => {
      const next = intentReducer(intent, { kind: 'toggleRelType', relType });
      dispatch({ kind: 'toggleRelType', relType });
      void runIntent(next);
    },
    [intent, runIntent],
  );

  const retry = useCallback(() => {
    void runIntent(intent, intent.op === 'expand');
  }, [intent, runIntent]);

  const expandNode = useCallback(
    (node: GraphNode) => {
      const next = intentReducer(intent, { kind: 'expand', nodeId: node.id });
      dispatch({ kind: 'expand', nodeId: node.id });
      void runIntent(next, true);
    },
    [intent, runIntent],
  );

  // Selecting a node (canvas click OR node-list Enter/tap — the non-hover access path, D-03)
  // opens the inspector; the canvas observes and refits the resized workspace.
  const selectNode = useCallback((node: GraphNode) => {
    setSelected(node);
  }, []);

  const closeInspector = useCallback(() => {
    setSelected(undefined);
  }, []);

  // Pin path = the node + its directly-connected neighbors (client-side highlight, D-10). The
  // The canvas accents this set and dims the rest; the path strip mirrors it.
  const pinPath = useCallback(
    (node: GraphNode) => {
      const path = new Set<string>([node.id]);
      for (const edge of view.result?.edges ?? []) {
        if (edge.source === node.id) path.add(edge.target);
        if (edge.target === node.id) path.add(edge.source);
      }
      setPinnedPath(path);
    },
    [view.result],
  );

  const clientGraph =
    view.result !== undefined ? rowsToClientGraph(view.result) : { nodes: [], edges: [] };
  const inspectorOpen = selected !== undefined;

  const controlPanel = (
    <GraphControls
      schema={view.schema}
      activeLabels={intent.labels}
      activeRelTypes={intent.relTypes}
      query={view.result?.query ?? ''}
      onRefresh={refreshGraph}
      onToggleLabel={onToggleLabel}
      onToggleRelType={onToggleRelType}
    />
  );

  const activeFilterCount = intent.labels.size + intent.relTypes.size;

  const canvasPane =
    view.status === 'loading' ? (
      <div role="status" className="grid h-full place-items-center text-sm text-text-muted">
        {t('graph.loading')}
      </div>
    ) : view.status === 'error-auth' ? (
      <div className="grid h-full place-items-center p-8 text-center">
        <Alert variant="destructive" className="max-w-md bg-surface">
          <AlertDescription>{t('graph.error.auth')}</AlertDescription>
        </Alert>
      </div>
    ) : view.status === 'error-query' || view.status === 'error-schema' ? (
      <div className="grid h-full place-items-center p-8 text-center">
        <div className="flex max-w-md flex-col items-center gap-3">
          <Alert variant="destructive" className="bg-surface">
            <AlertDescription>
              {view.status === 'error-schema' ? t('graph.error.schema') : t('graph.error.query')}
            </AlertDescription>
          </Alert>
          <Button type="button" variant="outline" onClick={retry}>
            {t('graph.error.retry')}
          </Button>
        </div>
      </div>
    ) : view.status === 'empty' ? (
      <>
        <EvidenceReadinessBoard schema={view.schema} onRefresh={refreshGraph} />
        <div hidden className="grid h-full place-items-center p-8 text-center">
          <div className="flex max-w-sm flex-col items-center gap-3">
            <span aria-hidden="true" className="text-4xl leading-none text-accent-text opacity-70">
              ◍
            </span>
            <h2 className="font-display text-[22px] font-semibold text-text">
              {t('graph.empty.heading')}
            </h2>
            <p className="text-[15.5px] leading-relaxed text-text-muted">{t('graph.empty.body')}</p>
          </div>
        </div>
      </>
    ) : (
      <Suspense
        fallback={
          <div role="status" className="grid h-full place-items-center text-sm text-text-muted">
            {t('graph.loading')}
          </div>
        }
      >
        <ArcadeGraphCanvas
          nodes={clientGraph.nodes}
          edges={clientGraph.edges}
          pinnedPath={pinnedPath}
          onNodeClick={(id) => {
            const node = view.result?.nodes.find((n) => n.id === id);
            if (node !== undefined) selectNode(node);
          }}
        />
        {view.capped ? (
          <p
            role="status"
            className="absolute bottom-3 left-1/2 -translate-x-1/2 rounded-full border border-border bg-surface-2/90 px-3 py-1 text-[13px] text-text-muted shadow-lg backdrop-blur"
          >
            {t('graph.cap.notice', { count: clientGraph.nodes.length })}
          </p>
        ) : null}
      </Suspense>
    );

  // Mobile-first layout (the lg: grid is the desktop 3-pane). On mobile the canvas is the
  // DOMINANT element (flex-1, real min-height — never the old crushed sliver); controls are a
  // bottom sheet behind the toolbar, the evidence list is a capped scroll region below, and the
  // inspector is a sheet that appears on selection. On lg it flips to controls | canvas | inspector
  // with the evidence strip under the canvas.
  return (
    <div className="graph-workspace-container h-full min-h-0">
      <div
        ref={workspaceRef}
        data-testid="graph-workspace"
        style={{ '--graph-filters-w': `${String(filtersWidth.width)}px` } as React.CSSProperties}
        className={`graph-workspace relative flex h-full min-h-0 flex-col overflow-hidden lg:grid lg:grid-rows-[minmax(0,1fr)_auto] ${
          inspectorOpen
            ? 'lg:grid-cols-[var(--graph-filters-w)_auto_minmax(0,1fr)_20rem]'
            : 'lg:grid-cols-[var(--graph-filters-w)_auto_minmax(0,1fr)]'
        }`}
      >
        {/* MOBILE control bar — keeps the canvas dominant; refresh + filters live here, not in an
          always-on top strip. */}
        <div className="graph-workspace__mobile-bar flex shrink-0 items-center gap-2 border-b border-border bg-surface px-3 py-2 lg:hidden">
          <Button
            type="button"
            onClick={refreshGraph}
            className="h-auto min-h-[44px] min-w-0 flex-1 px-3 py-2 text-[14px]"
          >
            <span className="block min-w-0 overflow-wrap-anywhere">
              {t('graph.cta.refreshMemory')}
            </span>
          </Button>
          <Button
            type="button"
            variant="outline"
            onClick={() => {
              setFiltersOpen(true);
            }}
            className="min-h-[44px] gap-1.5 px-3 py-2 text-[14px]"
          >
            {t('graph.filter.labels')}
            {activeFilterCount > 0 ? (
              <Badge className="px-1.5 text-[12px] font-bold">{activeFilterCount}</Badge>
            ) : null}
          </Button>
        </div>

        {/* CONTROLS — desktop left column; mobile bottom sheet (filtersOpen). ONE instance. */}
        <aside
          aria-label={t('graph.filter.labels')}
          data-open={filtersOpen ? 'true' : 'false'}
          className={`graph-workspace__filters min-h-0 lg:col-start-1 lg:row-start-1 lg:row-span-2 lg:flex lg:flex-col lg:border-r lg:border-border lg:bg-surface ${
            filtersOpen
              ? 'fixed inset-x-0 bottom-0 z-40 flex max-h-[80svh] flex-col rounded-t-2xl border-t border-border bg-surface shadow-2xl lg:inset-auto lg:z-auto lg:max-h-none lg:rounded-none lg:border-t-0 lg:shadow-none'
              : 'hidden lg:flex'
          }`}
        >
          <div className="flex items-center justify-between border-b border-border px-4 py-3 lg:hidden">
            <h2 className="font-display text-[17px] font-semibold text-text">
              {t('graph.filter.labels')}
            </h2>
            <Button
              type="button"
              variant="ghost"
              size="icon"
              onClick={() => {
                setFiltersOpen(false);
              }}
              aria-label={t('display.closeAria')}
              className="text-text-muted hover:text-text"
            >
              <X data-icon aria-hidden="true" />
            </Button>
          </div>
          <div className="min-h-0 flex-1 overflow-y-auto">{controlPanel}</div>
        </aside>

        {/* Its own grid track, so the handle never overlaps the filters or the canvas. The
          narrow regime here is a CONTAINER query, not `lg` — the class below is what that
          query hides, because `lg:block` can still be true inside a narrow container. */}
        <ColumnResizeHandle
          resize={filtersWidth}
          label={t('graph.filter.resize')}
          className="graph-workspace__resizer lg:col-start-2 lg:row-start-1 lg:row-span-2"
        />

        {/* CANVAS — the dominant element. Mobile: flex-1 with a real min-height. Desktop: center cell. */}
        <section
          aria-label={t('graph.title')}
          className="graph-workspace__canvas relative min-h-0 flex-1 bg-bg lg:col-start-3 lg:row-start-1 lg:flex-none"
        >
          {canvasPane}
        </section>

        {/* INSPECTOR — desktop right column only when selected; mobile bottom sheet on selection.
          ONE instance (no duplicate DOM / strict-mode collision). */}
        <aside
          aria-label={t('graph.inspector.emptyHeading')}
          data-open={inspectorOpen ? 'true' : 'false'}
          className={`graph-workspace__inspector min-h-0 lg:col-start-4 lg:row-start-1 lg:row-span-2 lg:static lg:overflow-y-auto lg:border-l lg:border-border lg:bg-surface ${
            inspectorOpen
              ? 'fixed inset-x-0 bottom-0 z-40 max-h-[78svh] overflow-y-auto border-t border-border bg-surface shadow-2xl lg:inset-auto lg:z-auto lg:max-h-none lg:border-t-0 lg:shadow-none'
              : 'hidden'
          }`}
        >
          <NodeInspector
            node={selected}
            query={view.result?.query ?? ''}
            onExpand={expandNode}
            onPinPath={pinPath}
            onClose={closeInspector}
          />
        </aside>

        {/* EVIDENCE — path strip + a11y parallel DOM. Mobile: a capped scroll region below the
          canvas (canvas stays dominant). Desktop: the strip under the canvas (col 2). */}
        <div className="graph-workspace__evidence max-h-[40svh] shrink-0 overflow-y-auto border-t border-border lg:col-start-3 lg:row-start-2 lg:max-h-[18vh] lg:shrink lg:overflow-y-auto">
          <PathStrip
            nodes={view.result?.nodes ?? []}
            edges={view.result?.edges ?? []}
            pinnedPath={pinnedPath}
            onSelectNode={selectNode}
          />
        </div>

        {/* Mobile sheet backdrops (lg:hidden). Tap to dismiss. */}
        {filtersOpen ? (
          <button
            type="button"
            aria-label={t('display.closeAria')}
            onClick={() => {
              setFiltersOpen(false);
            }}
            className="graph-workspace__backdrop fixed inset-0 z-30 bg-black/50 lg:hidden"
          />
        ) : null}
        {selected !== undefined ? (
          <button
            type="button"
            aria-label={t('display.closeAria')}
            onClick={closeInspector}
            className="graph-workspace__backdrop fixed inset-0 z-30 bg-black/50 lg:hidden"
          />
        ) : null}
      </div>
    </div>
  );
}
