import { useId, useMemo, useState } from 'react';
import { useTranslation } from 'react-i18next';
import type { DisplayTable } from './types';
import { DisplayCardShell } from './DisplayCardShell';
import { useCopyAction } from './useCopyAction';
import { filterAndSort, isNumericCell, nextSort, toCSV, toTSV, type SortState } from './tableData';

// TableDisplay (DISP-03 / D-14): a client-side sortable, filterable, copyable,
// CSV-exportable table over the trusted `{columns, rows}` payload, paginated in-card
// (default 3 rows/page). It keeps a single native <table> (one <thead>, a windowed
// <tbody>) so sort headers, row semantics, and a11y reads stay correct; the in-card
// pagination footer mirrors DisplayPagination's chrome (prev/next, "X–Y of N",
// per-page) but windows ROWS, which a generic children-paginator can't do inside a
// <tbody>. All values render as React-escaped <td> text — no markdown, no HTML
// (T-26-13). Numeric/id cells use the mono face. Accent is reserved for the active
// sort-column underline + the active page number only (Color rule).

const PER_PAGE_OPTIONS = [3, 6, 9] as const;

export interface TableDisplayProps {
  readonly payload: { readonly table?: DisplayTable };
}

export function TableDisplay({ payload }: TableDisplayProps) {
  const { t } = useTranslation();
  const { copied, copy } = useCopyAction();
  const selectId = useId();
  const [filter, setFilter] = useState('');
  const [sort, setSort] = useState<SortState | null>(null);
  const [page, setPage] = useState(0);
  const [perPage, setPerPage] = useState<number>(3);

  // Stabilise the derived arrays so the filter/sort memo doesn't recompute every
  // render off a fresh `?? []` reference (react-hooks/exhaustive-deps).
  const columns = useMemo(() => payload.table?.columns ?? [], [payload.table]);
  const rows = useMemo(() => payload.table?.rows ?? [], [payload.table]);

  const filtered = useMemo(() => filterAndSort(rows, filter, sort), [rows, filter, sort]);

  const toggleSort = (col: number) => {
    setPage(0);
    setSort((prev) => nextSort(prev, col));
  };

  const label = t('display.type.table');

  // No tabular data at all → the "No rows" empty state (Copywriting Contract).
  if (columns.length === 0 || rows.length === 0) {
    return (
      <DisplayCardShell label={label}>
        <EmptyState heading={t('display.table.emptyHeading')} body={t('display.table.emptyBody')} />
      </DisplayCardShell>
    );
  }

  const total = filtered.length;
  const totalPages = Math.max(1, Math.ceil(total / perPage));
  const current = Math.min(page, totalPages - 1);
  const start = current * perPage;
  const visible = filtered.slice(start, start + perPage);
  const from = total === 0 ? 0 : start + 1;
  const to = Math.min(start + perPage, total);

  const actions = (
    <>
      <button
        type="button"
        onClick={() => {
          copy(toTSV(columns, filtered));
        }}
        aria-label={t('display.table.copyAria')}
        className="flex min-h-11 items-center rounded-[var(--radius-md)] border border-border px-2 text-[0.75rem] font-medium text-text-muted hover:text-text focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-accent"
      >
        {copied ? t('display.table.copied') : t('display.table.copy')}
      </button>
      <button
        type="button"
        onClick={() => {
          copy(toCSV(columns, filtered));
        }}
        aria-label={t('display.table.exportAria')}
        className="flex min-h-11 items-center rounded-[var(--radius-md)] border border-border px-2 text-[0.75rem] font-medium text-text-muted hover:text-text focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-accent"
      >
        {t('display.table.exportCsv')}
      </button>
    </>
  );

  return (
    <DisplayCardShell
      label={label}
      meta={t('display.table.rowCount', { count: rows.length })}
      actions={actions}
    >
      <div className="mb-3">
        <input
          type="text"
          value={filter}
          onChange={(e) => {
            setFilter(e.target.value);
            setPage(0);
          }}
          placeholder={t('display.table.filterPlaceholder')}
          aria-label={t('display.table.filterPlaceholder')}
          // omit-when-valid: the filter has no invalid state, so the attr is dropped
          // (React removes `undefined`) — feedback_aria_invalid_omit_when_valid.
          aria-invalid={undefined}
          className="min-h-11 w-full rounded-[var(--radius-md)] border border-border bg-surface px-3 text-sm text-text outline-none focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-accent"
        />
      </div>

      {total === 0 ? (
        <EmptyState
          heading={t('display.table.noMatchHeading')}
          body={t('display.table.noMatchBody', { query: filter.trim() })}
        />
      ) : (
        <>
          <div className="overflow-x-auto rounded-[var(--radius-md)] border border-border">
            <table className="min-w-full border-collapse text-left text-sm">
              <thead>
                <tr>
                  {columns.map((col, ci) => {
                    const active = sort?.col === ci;
                    const dir = active ? sort.dir : undefined;
                    return (
                      <th
                        key={ci}
                        aria-sort={active ? (dir === 'asc' ? 'ascending' : 'descending') : 'none'}
                        className="border-b border-border bg-surface-2 p-0 text-left"
                      >
                        <button
                          type="button"
                          onClick={() => {
                            toggleSort(ci);
                          }}
                          aria-label={t('display.table.sortBy', { column: col })}
                          className={`flex min-h-11 w-full items-center gap-1 px-3 text-[0.75rem] font-medium uppercase text-text-faint hover:text-text focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-accent ${active ? 'border-b-2 border-b-border-strong text-text' : ''}`}
                        >
                          <span>{col}</span>
                          {active ? (
                            <span aria-hidden="true" className="text-accent-text">
                              {dir === 'asc' ? '↑' : '↓'}
                            </span>
                          ) : null}
                          <span className="sr-only">
                            {active
                              ? t(
                                  dir === 'asc'
                                    ? 'display.table.sortedAsc'
                                    : 'display.table.sortedDesc',
                                )
                              : ''}
                          </span>
                        </button>
                      </th>
                    );
                  })}
                </tr>
              </thead>
              <tbody>
                {visible.map((row, ri) => (
                  <tr key={start + ri}>
                    {columns.map((_, ci) => {
                      const value = row[ci] ?? '';
                      return (
                        <td
                          key={ci}
                          className={`border-b border-border px-3 py-2 text-text-muted ${isNumericCell(value) ? 'font-mono tabular-nums' : ''}`}
                        >
                          {value}
                        </td>
                      );
                    })}
                  </tr>
                ))}
              </tbody>
            </table>
          </div>

          {/* In-card pagination footer (D-PAGINATION): per-page + "X–Y of N" + prev/next,
              windowing ROWS at 3/page. Mirrors DisplayPagination's native chrome. */}
          <div className="mt-2 flex flex-wrap items-center justify-between gap-2">
            <div className="flex items-center gap-2">
              <label htmlFor={selectId} className="text-[0.75rem] font-medium text-text-muted">
                {t('display.pagination.perPage')}
              </label>
              <select
                id={selectId}
                value={perPage}
                onChange={(e) => {
                  setPerPage(Number(e.target.value));
                  setPage(0);
                }}
                className="min-h-11 rounded-[var(--radius-md)] border border-border bg-surface-2 px-2 text-xs text-text outline-none focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-accent"
              >
                {PER_PAGE_OPTIONS.map((n) => (
                  <option key={n} value={n}>
                    {n}
                  </option>
                ))}
              </select>
            </div>

            <span aria-live="polite" className="text-[0.75rem] tabular-nums text-text-faint">
              {t('display.pagination.count', { from, to, total })}
            </span>

            {totalPages > 1 ? (
              <div className="flex items-center gap-1">
                <button
                  type="button"
                  onClick={() => {
                    setPage((p) => Math.max(0, p - 1));
                  }}
                  disabled={current === 0}
                  aria-label={t('display.pagination.previous')}
                  className="flex min-h-11 min-w-11 items-center justify-center rounded-[var(--radius-md)] text-text-muted hover:text-text disabled:opacity-30 focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-accent"
                >
                  <span aria-hidden="true">‹</span>
                </button>
                <span className="text-[0.75rem] tabular-nums text-accent-text">{current + 1}</span>
                <button
                  type="button"
                  onClick={() => {
                    setPage((p) => Math.min(totalPages - 1, p + 1));
                  }}
                  disabled={current >= totalPages - 1}
                  aria-label={t('display.pagination.next')}
                  className="flex min-h-11 min-w-11 items-center justify-center rounded-[var(--radius-md)] text-text-muted hover:text-text disabled:opacity-30 focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-accent"
                >
                  <span aria-hidden="true">›</span>
                </button>
              </div>
            ) : null}
          </div>
        </>
      )}
    </DisplayCardShell>
  );
}

function EmptyState({ heading, body }: { readonly heading: string; readonly body: string }) {
  return (
    <div className="flex flex-col items-center gap-1 py-8 text-center">
      <p className="text-sm font-medium text-text">{heading}</p>
      <p className="text-[0.75rem] text-text-faint">{body}</p>
    </div>
  );
}
