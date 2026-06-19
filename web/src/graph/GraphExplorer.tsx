import { Suspense, lazy, useCallback, useEffect, useReducer, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { fetchGraphSchema, postGraphQuery } from './graphApi';
import {
  DEFAULT_EDGE_CAP,
  DEFAULT_NODE_CAP,
  initialIntentState,
  intentReducer,
  rowsToClientGraph,
  toClientIntent,
  type IntentState,
} from './graphIntent';
import { SeedFilterPanel } from './SeedFilterPanel';
import { NodeInspector } from './NodeInspector';
import { PathStrip } from './PathStrip';
import type { GraphNode, GraphResult, GraphSchema } from './types';

// GraphExplorer is the lazy default export the AppShell mounts when surface==='graph' (its own
// Vite chunk so the Sigma stack never lands in the main bundle — Pitfall 7). It is the
// three-CSS-grid-column workspace shell (SeedFilterPanel | canvas | inspector) with the path
// strip below the canvas. It owns the intent/selection/path state (the plan-03 intentReducer)
// and the `sigmaKey` counter (bumped on mount + inspector open/close to dodge the Sigma resize-
// remount crash, Pitfall 1).
//
// State machine for the default open (D-07/D-08, carry-forward from plans 01/02): on mount with
// a threadId it POSTs op:'seed'; an EMPTY result falls back to the schema overview (never a blank
// canvas). A fetch that REJECTS with `HTTP 401` (expired/absent session) renders a VISIBLE auth-
// error state — never a silent blank canvas (B3 / threat T-27-03). Any other rejection renders
// the query/schema error state. SigmaCanvas + the parallel-DOM surface are imported lazily/in
// Task 3; Task 2 slots a placeholder for the right pane + below-canvas surface.
//
// NodeInspector + PathStrip are wired in Task 3 (this file is edited there); the SigmaCanvas is
// lazy so the WebGL chunk loads only inside this already-lazy workspace.

const SigmaCanvas = lazy(() =>
  import('./SigmaCanvas').then((mod) => ({ default: mod.SigmaCanvas })),
);

export interface GraphExplorerProps {
  readonly threadId: string;
}

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
  return result.nodes.length >= DEFAULT_NODE_CAP || result.edges.length >= DEFAULT_EDGE_CAP;
}

export default function GraphExplorer({ threadId }: GraphExplorerProps) {
  const { t } = useTranslation();
  const [intent, dispatch] = useReducer(intentReducer, undefined, initialIntentState);
  const [view, setView] = useState<ViewState>(INITIAL_VIEW);
  const [selected, setSelected] = useState<GraphNode | undefined>(undefined);
  const [pinnedPath, setPinnedPath] = useState<ReadonlySet<string>>(new Set());
  // sigmaKey starts at 1 so the first mount + every inspector open/close remounts cleanly.
  const [sigmaKey, setSigmaKey] = useState(1);
  // Mobile-only: the seed/filter pane is a bottom sheet (canvas stays dominant). On lg it is
  // the permanent left column and this flag is inert.
  const [filtersOpen, setFiltersOpen] = useState(false);

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
    async (state: IntentState) => {
      setView((prev) => ({ ...prev, status: 'loading' }));
      try {
        const result = await postGraphQuery(toClientIntent(state));
        if (resultIsEmpty(result)) {
          await loadSchemaOverview();
          return;
        }
        setView({
          status: 'populated',
          result,
          schema: result.schema,
          capped: isCapped(result),
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

  // Default open: seed from the active thread, schema-overview fallback on empty.
  useEffect(() => {
    const seed: IntentState = { ...initialIntentState(), session: threadId };
    void runIntent(seed);
    setSelected(undefined);
    setPinnedPath(new Set());
    setSigmaKey((k) => k + 1);
  }, [threadId, runIntent]);

  const onSeed = useCallback(() => {
    const next = intentReducer(intent, { kind: 'setSeed', session: threadId });
    void runIntent(next);
  }, [intent, threadId, runIntent]);

  const onToggleLabel = useCallback((label: string) => {
    dispatch({ kind: 'toggleLabel', label });
  }, []);
  const onToggleRelType = useCallback((relType: string) => {
    dispatch({ kind: 'toggleRelType', relType });
  }, []);

  const retry = useCallback(() => {
    void runIntent({ ...intent, session: threadId });
  }, [intent, threadId, runIntent]);

  // Selecting a node (canvas click OR node-list Enter/tap — the non-hover access path, D-03)
  // opens the inspector and remounts sigma (Pitfall 1: inspector open/close bumps sigmaKey).
  const selectNode = useCallback((node: GraphNode) => {
    setSelected(node);
    setSigmaKey((k) => k + 1);
  }, []);

  const closeInspector = useCallback(() => {
    setSelected(undefined);
    setSigmaKey((k) => k + 1);
  }, []);

  // Pin path = the node + its directly-connected neighbors (client-side highlight, D-10). The
  // SigmaCanvas reducer accents this set and dims the rest; the path strip mirrors it.
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

  const seedPanel = (
    <SeedFilterPanel
      schema={view.schema}
      activeLabels={intent.labels}
      activeRelTypes={intent.relTypes}
      query={view.result?.query ?? ''}
      canSeed={threadId.length > 0}
      onSeed={onSeed}
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
      <div role="alert" className="grid h-full place-items-center p-8 text-center">
        <p className="max-w-md text-[15.5px] text-danger">{t('graph.error.auth')}</p>
      </div>
    ) : view.status === 'error-query' || view.status === 'error-schema' ? (
      <div role="alert" className="grid h-full place-items-center p-8 text-center">
        <div className="flex max-w-md flex-col items-center gap-3">
          <p className="text-[15.5px] text-danger">
            {view.status === 'error-schema' ? t('graph.error.schema') : t('graph.error.query')}
          </p>
          <button
            type="button"
            onClick={retry}
            className="min-h-[44px] rounded-md border border-border bg-surface-2 px-4 py-2 text-[13px] font-semibold text-text transition-colors hover:border-border-strong"
          >
            {t('graph.error.retry')}
          </button>
        </div>
      </div>
    ) : view.status === 'empty' ? (
      <div className="grid h-full place-items-center p-8 text-center">
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
    ) : (
      <Suspense
        fallback={
          <div role="status" className="grid h-full place-items-center text-sm text-text-muted">
            {t('graph.loading')}
          </div>
        }
      >
        <SigmaCanvas
          nodes={clientGraph.nodes}
          edges={clientGraph.edges}
          pinnedPath={pinnedPath}
          sigmaKey={sigmaKey}
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
  // DOMINANT element (flex-1, real min-height — never the old crushed sliver); seed/filter is a
  // bottom sheet behind the toolbar, the evidence list is a capped scroll region below, and the
  // inspector is a sheet that appears on selection. On lg it flips to seed | canvas | inspector
  // with the evidence strip under the canvas.
  return (
    <div className="relative flex h-full min-h-0 flex-col lg:grid lg:grid-cols-[15rem_minmax(0,1fr)_20rem] lg:grid-rows-[minmax(0,1fr)_auto]">
      {/* MOBILE control bar — keeps the canvas dominant; Seed + Filters live here, not in an
          always-on top strip. */}
      <div className="flex shrink-0 items-center gap-2 border-b border-border bg-surface px-3 py-2 lg:hidden">
        <button
          type="button"
          onClick={onSeed}
          disabled={threadId.length === 0}
          className="min-h-[40px] flex-1 rounded-md bg-accent px-3 py-1.5 text-[14px] font-semibold text-on-accent transition-opacity disabled:opacity-40"
        >
          {t('graph.cta.seedConversation')}
        </button>
        <button
          type="button"
          onClick={() => {
            setFiltersOpen(true);
          }}
          className="flex min-h-[40px] items-center gap-1.5 rounded-md border border-border bg-surface-2 px-3 py-1.5 text-[14px] font-semibold text-text transition-colors hover:border-border-strong"
        >
          {t('graph.filter.labels')}
          {activeFilterCount > 0 ? (
            <span className="rounded-full bg-accent px-1.5 text-[12px] font-bold text-on-accent">
              {activeFilterCount}
            </span>
          ) : null}
        </button>
      </div>

      {/* SEED / FILTER — desktop left column; mobile bottom sheet (filtersOpen). ONE instance. */}
      <aside
        aria-label={t('graph.filter.labels')}
        className={`min-h-0 lg:col-start-1 lg:row-start-1 lg:row-span-2 lg:flex lg:flex-col lg:border-r lg:border-border ${
          filtersOpen
            ? 'fixed inset-x-0 bottom-0 z-40 flex max-h-[80svh] flex-col rounded-t-2xl border-t border-border bg-surface shadow-2xl lg:inset-auto lg:z-auto lg:max-h-none lg:rounded-none lg:border-t-0 lg:shadow-none'
            : 'hidden lg:flex'
        }`}
      >
        <div className="flex items-center justify-between border-b border-border px-4 py-3 lg:hidden">
          <h2 className="font-display text-[17px] font-semibold text-text">
            {t('graph.filter.labels')}
          </h2>
          <button
            type="button"
            onClick={() => {
              setFiltersOpen(false);
            }}
            aria-label={t('display.closeAria')}
            className="min-h-[40px] min-w-[40px] rounded-md text-text-muted hover:text-text"
          >
            ✕
          </button>
        </div>
        <div className="min-h-0 flex-1 overflow-y-auto">{seedPanel}</div>
      </aside>

      {/* CANVAS — the dominant element. Mobile: flex-1 with a real min-height. Desktop: center cell. */}
      <section
        aria-label={t('graph.title')}
        className="relative min-h-[46svh] flex-1 bg-bg lg:col-start-2 lg:row-start-1 lg:min-h-0 lg:flex-none"
      >
        {canvasPane}
      </section>

      {/* INSPECTOR — desktop right column (empty state when nothing selected); mobile bottom sheet
          on selection. ONE instance (no duplicate DOM / strict-mode collision). */}
      <aside
        aria-label={t('graph.inspector.emptyHeading')}
        className={`min-h-0 lg:col-start-3 lg:row-start-1 lg:row-span-2 lg:static lg:block lg:overflow-y-auto lg:border-l lg:border-border lg:bg-surface ${
          selected !== undefined
            ? 'fixed inset-x-0 bottom-0 z-40 max-h-[78svh] overflow-y-auto border-t border-border bg-surface shadow-2xl lg:inset-auto lg:z-auto lg:max-h-none lg:border-t-0 lg:shadow-none'
            : 'hidden lg:block'
        }`}
      >
        <NodeInspector
          node={selected}
          query={view.result?.query ?? ''}
          onPinPath={pinPath}
          onClose={closeInspector}
        />
      </aside>

      {/* EVIDENCE — path strip + a11y parallel DOM. Mobile: a capped scroll region below the
          canvas (canvas stays dominant). Desktop: the strip under the canvas (col 2). */}
      <div className="max-h-[40svh] shrink-0 overflow-y-auto border-t border-border lg:col-start-2 lg:row-start-2 lg:max-h-none lg:shrink lg:overflow-visible">
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
          className="fixed inset-0 z-30 bg-black/50 lg:hidden"
        />
      ) : null}
      {selected !== undefined ? (
        <button
          type="button"
          aria-label={t('display.closeAria')}
          onClick={closeInspector}
          className="fixed inset-0 z-30 bg-black/50 lg:hidden"
        />
      ) : null}
    </div>
  );
}
