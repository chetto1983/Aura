import { useEffect, useId, useMemo, useRef, useState } from 'react';
import { createPortal } from 'react-dom';
import { ArrowDown, ArrowUp, ChevronLeft, ChevronRight, X } from 'lucide-react';
import { useTranslation } from 'react-i18next';
import { focusFirstDescendant, trapTabKey } from '../../a11y/focusTrap';
import type { DisplaySource } from './types';
import {
  anyIncomplete,
  filterAndSortSources,
  nextExplorerSort,
  safeHost,
  type SourceSort,
  type SourceSortKey,
} from './sourceExplorerData';
import { Alert, AlertDescription } from '@/components/ui/alert';
import { Badge } from '@/components/ui/badge';
import { Button } from '@/components/ui/button';
import { Input } from '@/components/ui/input';
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/components/ui/tabs';

// SourceExplorerSheet (D-03 / D-13 / DISP-05): the READ-ONLY fullscreen evidence
// dossier. It renders the SAME code-owned source registry the citations resolve
// against (citations show the cited subset; this Table shows everything consulted)
// across three view-only tabs — Table (sort/search/paginate), Metadata, and
// Configuration. It is reached two ways (one shared open-state, one registry): a
// citation chip click-through (CitationBubble onOpenSource → focusRefId) and the
// answer-level "Sources (N)" button (SourcesButton).
//
// SECURITY POSTURE (UI-SPEC §"Security & Safety Posture", T-26-19):
//   - NO PATCH/write/destructive control this milestone. No Re-Analyze, no Clear,
//     no Save — those governance-write surfaces are DEFERRED to Phase 29. The sheet
//     only reads the trusted-normalizer registry fields (T-26-20).
//   - A refId that does not resolve against the registry focuses nothing (T-26-21);
//     there is no fabricated source target.
//
// A11y (UI-SPEC Interaction & Accessibility Contract): role="dialog" + aria-modal,
// a focus trap that returns focus to the opener on close, Esc closes, the 18px
// Fraunces section title (the only display-face surface), 44px touch targets.

type ExplorerTab = 'table' | 'metadata' | 'configuration';

const TABS: readonly ExplorerTab[] = ['table', 'metadata', 'configuration'];

export interface SourceExplorerSheetProps {
  readonly open: boolean;
  readonly sources: readonly DisplaySource[];
  /** The citation/Sources entry point may request a specific source be focused. */
  readonly focusRefId?: string | undefined;
  readonly onClose: () => void;
}

export function SourceExplorerSheet({
  open,
  sources,
  focusRefId,
  onClose,
}: SourceExplorerSheetProps) {
  const { t } = useTranslation();
  const panelRef = useRef<HTMLDivElement>(null);
  const openerRef = useRef<HTMLElement | null>(null);

  // Focus trap + Esc + scroll-lock + restore-focus-on-close (Drawer precedent,
  // fullscreen variant). Effects here only touch external systems (the DOM + the
  // document listener), never component state — the view state lives in SheetBody
  // which mounts fresh per open, so there is no reset-on-open setState.
  useEffect(() => {
    if (!open) return;
    openerRef.current = document.activeElement as HTMLElement | null;
    const { overflow } = document.body.style;
    document.body.style.overflow = 'hidden';
    const panel = panelRef.current;
    focusFirstDescendant(panel);

    function onKeyDown(event: KeyboardEvent) {
      if (event.key === 'Escape') {
        event.preventDefault();
        onClose();
        return;
      }
      trapTabKey(event, panel);
    }

    document.addEventListener('keydown', onKeyDown);
    return () => {
      document.removeEventListener('keydown', onKeyDown);
      document.body.style.overflow = overflow;
      openerRef.current?.focus();
    };
  }, [open, onClose]);

  if (!open) return null;

  return createPortal(
    <div className="fixed inset-0 z-50" role="presentation">
      <button
        type="button"
        aria-label={t('source.closeAria')}
        className="absolute inset-0 cursor-default bg-bg/80 backdrop-blur-sm"
        onClick={onClose}
      />
      <div
        ref={panelRef}
        role="dialog"
        aria-modal="true"
        aria-label={t('source.title')}
        className="absolute inset-x-0 top-0 mx-auto flex h-[100dvh] w-full max-w-3xl flex-col overflow-hidden border-border bg-surface shadow-[var(--shadow-drawer)] sm:inset-4 sm:h-auto sm:max-h-[calc(100dvh-2rem)] sm:rounded-[var(--radius-lg)] sm:border"
      >
        <SheetBody sources={sources} initialRefId={focusRefId} onClose={onClose} />
      </div>
    </div>,
    document.body,
  );
}

interface SheetBodyProps {
  readonly sources: readonly DisplaySource[];
  readonly initialRefId: string | undefined;
  readonly onClose: () => void;
}

/** The inner view: mounts fresh per open so its state initializes (incl. the
 *  citation focusRefId) without a reset-on-open effect (no setState-in-effect). */
function SheetBody({ sources, initialRefId, onClose }: SheetBodyProps) {
  const { t } = useTranslation();
  // A focusRefId that resolves against the registry opens the Metadata view for it
  // (T-26-21: an unknown refId selects nothing and the table view stays).
  const resolved =
    initialRefId !== undefined && sources.some((s) => s.ref_id === initialRefId)
      ? initialRefId
      : undefined;
  const [tab, setTab] = useState<ExplorerTab>(resolved !== undefined ? 'metadata' : 'table');
  const [query, setQuery] = useState('');
  const [sort, setSort] = useState<SourceSort | null>(null);
  const [selectedRefId, setSelectedRefId] = useState<string | undefined>(resolved);

  const rows = useMemo(() => filterAndSortSources(sources, query, sort), [sources, query, sort]);
  const selected = useMemo(
    () => sources.find((s) => s.ref_id === selectedRefId),
    [sources, selectedRefId],
  );
  const showWarning = useMemo(() => anyIncomplete(sources), [sources]);

  const toggleSort = (key: SourceSortKey) => {
    setSort((prev) => nextExplorerSort(prev, key));
  };

  return (
    <>
      <header className="flex min-h-14 items-center justify-between gap-3 border-b border-border px-4">
        {/* The 18px Fraunces section title — the only display-face surface (UI-SPEC). */}
        <h2 className="font-display text-lg leading-tight text-text">
          {t('source.title')}
          <span className="ms-2 align-middle text-[0.75rem] font-normal text-text-faint">
            {t('source.rowCount', { count: sources.length })}
          </span>
        </h2>
        <Button
          type="button"
          onClick={onClose}
          aria-label={t('source.closeAria')}
          variant="ghost"
          size="icon"
          className="text-text-muted hover:text-text"
        >
          <X data-icon aria-hidden="true" />
        </Button>
      </header>

      <Tabs
        value={tab}
        onValueChange={(value) => {
          setTab(value as ExplorerTab);
        }}
        className="min-h-0 flex-1 gap-0"
      >
        {/* Read-only tab strip: accent underline on the active tab (Color rule #3). */}
        <div className="border-b border-border px-4">
          <TabsList
            aria-label={t('source.title')}
            className="w-fit gap-1 rounded-none border-0 bg-transparent p-0"
          >
            {TABS.map((id) => (
              <TabsTrigger
                key={id}
                value={id}
                onClick={() => {
                  setTab(id);
                }}
                className="rounded-none border-b-2 border-b-transparent px-3 text-xs data-[state=active]:border-b-accent data-[state=active]:bg-transparent data-[state=active]:text-accent-text"
              >
                {t(`source.tab.${id}`)}
              </TabsTrigger>
            ))}
          </TabsList>
        </div>

        {showWarning ? (
          <Alert className="rounded-none border-x-0 border-t-0 border-warning bg-warning/10 px-4 py-2 text-warning">
            <AlertDescription className="text-[0.75rem] text-warning">
              {t('source.warningBanner')}
            </AlertDescription>
          </Alert>
        ) : null}

        <TabsContent value={tab} className="min-h-0 flex-1 overflow-y-auto p-4">
          {sources.length === 0 ? (
            <div className="flex flex-col items-center gap-1 py-12 text-center">
              <p className="text-sm font-medium text-text">{t('source.empty.heading')}</p>
              <p className="text-[0.75rem] text-text-faint">{t('source.empty.body')}</p>
            </div>
          ) : tab === 'table' ? (
            <TableView
              rows={rows}
              query={query}
              sort={sort}
              onSearch={setQuery}
              onSort={toggleSort}
              onSelect={(refId) => {
                setSelectedRefId(refId);
                setTab('metadata');
              }}
            />
          ) : tab === 'metadata' ? (
            <MetadataView source={selected} />
          ) : (
            <ConfigurationView sources={sources} />
          )}
        </TabsContent>
      </Tabs>
    </>
  );
}

interface TableViewProps {
  readonly rows: readonly DisplaySource[];
  readonly query: string;
  readonly sort: SourceSort | null;
  readonly onSearch: (value: string) => void;
  readonly onSort: (key: SourceSortKey) => void;
  readonly onSelect: (refId: string) => void;
}

const COLUMNS: readonly SourceSortKey[] = ['index', 'type', 'title', 'source', 'status'];
const ROWS_PER_PAGE = 9;

function TableView({ rows, query, sort, onSearch, onSort, onSelect }: TableViewProps) {
  const { t } = useTranslation();
  const searchId = useId();
  const [page, setPage] = useState(0);
  const totalPages = Math.max(1, Math.ceil(rows.length / ROWS_PER_PAGE));
  const current = Math.min(page, totalPages - 1);
  const start = current * ROWS_PER_PAGE;
  const visible = rows.slice(start, start + ROWS_PER_PAGE);

  return (
    <div className="flex flex-col gap-3">
      <div>
        <label htmlFor={searchId} className="sr-only">
          {t('source.searchPlaceholder')}
        </label>
        <Input
          id={searchId}
          type="search"
          value={query}
          onChange={(e) => {
            onSearch(e.target.value);
            setPage(0);
          }}
          placeholder={t('source.searchPlaceholder')}
          // omit-when-valid: read-only search has no invalid state (feedback_aria_invalid_omit_when_valid).
          aria-invalid={undefined}
          className="bg-surface text-sm"
        />
      </div>

      {rows.length === 0 ? (
        <div className="flex flex-col items-center gap-1 py-8 text-center">
          <p className="text-sm font-medium text-text">{t('source.empty.heading')}</p>
          <p className="text-[0.75rem] text-text-faint">{t('source.empty.body')}</p>
        </div>
      ) : (
        <>
          <div className="overflow-x-auto rounded-[var(--radius-md)] border border-border">
            <table className="min-w-full border-collapse text-left text-sm">
              <thead>
                <tr>
                  {COLUMNS.map((key) => {
                    const active = sort?.key === key;
                    const dir = active ? sort.dir : undefined;
                    return (
                      <th
                        key={key}
                        aria-sort={active ? (dir === 'asc' ? 'ascending' : 'descending') : 'none'}
                        className="border-b border-border bg-surface-2 p-0 text-left"
                      >
                        <Button
                          type="button"
                          variant="ghost"
                          onClick={() => {
                            onSort(key);
                            setPage(0);
                          }}
                          aria-label={t('source.sortBy', { column: t(`source.column.${key}`) })}
                          className={`h-auto min-h-11 w-full justify-start rounded-none px-3 text-[0.75rem] uppercase tracking-wider text-text-faint hover:text-text ${
                            active ? 'border-b-2 border-b-accent text-accent-text' : ''
                          }`}
                        >
                          <span>{t(`source.column.${key}`)}</span>
                          {active ? (
                            <span aria-hidden="true" className="text-accent-text">
                              {dir === 'asc' ? (
                                <ArrowUp data-icon aria-hidden="true" />
                              ) : (
                                <ArrowDown data-icon aria-hidden="true" />
                              )}
                            </span>
                          ) : null}
                        </Button>
                      </th>
                    );
                  })}
                </tr>
              </thead>
              <tbody>
                {visible.map((source) => (
                  <SourceRow key={source.ref_id} source={source} onSelect={onSelect} />
                ))}
              </tbody>
            </table>
          </div>

          {totalPages > 1 ? (
            <div className="flex items-center justify-end gap-1">
              <Button
                type="button"
                variant="ghost"
                size="icon"
                onClick={() => {
                  setPage((p) => Math.max(0, p - 1));
                }}
                disabled={current === 0}
                aria-label={t('display.pagination.previous')}
                className="min-h-11 min-w-11 text-text-muted hover:text-text"
              >
                <ChevronLeft data-icon aria-hidden="true" />
              </Button>
              <span aria-live="polite" className="text-[0.75rem] tabular-nums text-accent-text">
                {t('display.pagination.count', {
                  from: start + 1,
                  to: Math.min(start + ROWS_PER_PAGE, rows.length),
                  total: rows.length,
                })}
              </span>
              <Button
                type="button"
                variant="ghost"
                size="icon"
                onClick={() => {
                  setPage((p) => Math.min(totalPages - 1, p + 1));
                }}
                disabled={current >= totalPages - 1}
                aria-label={t('display.pagination.next')}
                className="min-h-11 min-w-11 text-text-muted hover:text-text"
              >
                <ChevronRight data-icon aria-hidden="true" />
              </Button>
            </div>
          ) : null}
        </>
      )}
    </div>
  );
}

function SourceRow({
  source,
  onSelect,
}: {
  readonly source: DisplaySource;
  readonly onSelect: (refId: string) => void;
}) {
  const { t } = useTranslation();
  const title = source.title ?? t('source.untitled');
  const host = source.url !== undefined && source.url.length > 0 ? safeHost(source.url) : '';
  return (
    <tr className="hover:bg-surface-3">
      <td className="border-b border-border px-3 py-2 font-mono tabular-nums text-[0.75rem] text-text-faint">
        {source.index}
      </td>
      <td className="border-b border-border px-3 py-2 text-[0.75rem] text-text-faint">
        {source.type !== undefined ? t(`display.type.${source.type}`) : '—'}
      </td>
      <td className="border-b border-border px-3 py-2 text-text">
        <Button
          type="button"
          variant="ghost"
          onClick={() => {
            onSelect(source.ref_id);
          }}
          aria-label={t('source.openRowAria', { n: source.index })}
          className="h-auto min-h-0 max-w-xs justify-start truncate p-0 text-left text-sm font-normal hover:bg-transparent hover:underline"
        >
          {title}
        </Button>
      </td>
      <td className="border-b border-border px-3 py-2 text-[0.75rem] text-text-faint">
        <span className="block max-w-[10rem] truncate font-mono">{host}</span>
      </td>
      <td className="border-b border-border px-3 py-2">
        <StatusTag cited={source.cited} />
      </td>
    </tr>
  );
}

function StatusTag({ cited }: { readonly cited: boolean }) {
  const { t } = useTranslation();
  return (
    <Badge variant={cited ? 'default' : 'secondary'} className="text-[0.75rem]">
      {cited ? t('source.status.cited') : t('source.status.consulted')}
    </Badge>
  );
}

function MetadataView({ source }: { readonly source: DisplaySource | undefined }) {
  const { t } = useTranslation();
  if (source === undefined) {
    return (
      <div className="flex flex-col gap-1">
        <h3 className="font-display text-lg leading-tight text-text">
          {t('source.metadata.heading')}
        </h3>
        <p className="text-sm text-text-muted">{t('source.metadata.selectPrompt')}</p>
      </div>
    );
  }
  const none = t('source.metadata.none');
  const rows: readonly (readonly [string, string])[] = [
    [t('source.metadata.refId'), source.ref_id],
    [t('source.metadata.type'), source.type ?? none],
    [t('source.metadata.url'), source.url ?? none],
    [
      t('source.metadata.confidence'),
      source.confidence !== undefined ? source.confidence.toFixed(2) : none,
    ],
    [t('source.metadata.snippet'), source.snippet ?? none],
  ];
  return (
    <div className="flex flex-col gap-3">
      <h3 className="font-display text-lg leading-tight text-text">
        {source.title ?? t('source.untitled')}
      </h3>
      <p className="text-[0.75rem] text-text-faint">{t('source.readOnlyNotice')}</p>
      <dl className="flex flex-col gap-2">
        {rows.map(([label, value]) => (
          <div key={label} className="flex flex-col gap-0.5 border-b border-border pb-2">
            <dt className="text-[0.75rem] font-medium uppercase tracking-wider text-text-faint">
              {label}
            </dt>
            <dd className="break-words font-mono text-xs text-text-muted">{value}</dd>
          </div>
        ))}
      </dl>
    </div>
  );
}

function ConfigurationView({ sources }: { readonly sources: readonly DisplaySource[] }) {
  const { t } = useTranslation();
  const cited = sources.filter((s) => s.cited).length;
  const consulted = sources.length - cited;
  const rows: readonly (readonly [string, number])[] = [
    [t('source.configuration.total'), sources.length],
    [t('source.configuration.cited'), cited],
    [t('source.configuration.consulted'), consulted],
  ];
  return (
    <div className="flex flex-col gap-3">
      <h3 className="font-display text-lg leading-tight text-text">
        {t('source.configuration.heading')}
      </h3>
      <p className="text-[0.75rem] text-text-faint">{t('source.configuration.readOnly')}</p>
      <dl className="flex flex-col gap-2">
        {rows.map(([label, value]) => (
          <div
            key={label}
            className="flex items-center justify-between border-b border-border pb-2 text-sm"
          >
            <dt className="text-text-muted">{label}</dt>
            <dd className="font-mono tabular-nums text-text">{value}</dd>
          </div>
        ))}
      </dl>
    </div>
  );
}
