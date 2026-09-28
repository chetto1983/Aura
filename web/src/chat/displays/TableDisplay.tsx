import { useId, useMemo, useState } from 'react';
import { ChevronLeft, ChevronRight } from 'lucide-react';
import { useTranslation } from 'react-i18next';
import type { DisplayTable } from './types';
import { DisplayCardShell } from './DisplayCardShell';
import { useCopyAction } from './useCopyAction';
import { filterAndSort, toCSV, toTSV } from './tableData';
import { DataTable, type DataTableColumn } from '@/components/data-table';
import type { DataTableSort } from '@/components/data-table-data';
import { Button } from '@/components/ui/button';
import { Input } from '@/components/ui/input';
import { NativeSelect, NativeSelectOption } from '@/components/ui/native-select';

// TableDisplay (DISP-03 / D-14): a client-side sortable, filterable, copyable,
// CSV-exportable table over trusted `{columns, rows}`, paginated in-card (default
// 3 rows/page). Elements DataTable renders a semantic table at wide widths and
// labeled cards at narrow widths. Filtering and sorting happen over the full row
// set before pagination. Cell values render as escaped React text (T-26-13).

const PER_PAGE_OPTIONS = [3, 6, 9] as const;

export interface TableDisplayProps {
  readonly payload: { readonly table?: DisplayTable };
  readonly label?: string;
}

export function TableDisplay({ payload, label: labelOverride }: TableDisplayProps) {
  const { t, i18n } = useTranslation();
  const { copied, copy } = useCopyAction();
  const selectId = useId();
  const [filter, setFilter] = useState('');
  const [sort, setSort] = useState<DataTableSort | null>(null);
  const [page, setPage] = useState(0);
  const [perPage, setPerPage] = useState<number>(3);

  // Stabilise the derived arrays so the filter/sort memo doesn't recompute every
  // render off a fresh `?? []` reference (react-hooks/exhaustive-deps).
  const columns = useMemo(() => payload.table?.columns ?? [], [payload.table]);
  const rows = useMemo(() => payload.table?.rows ?? [], [payload.table]);
  const notice = payload.table?.notice;

  const filtered = useMemo(
    () => filterAndSort(rows, filter, sort, columns, i18n.language),
    [rows, filter, sort, columns, i18n.language],
  );
  const dataColumns: DataTableColumn[] = columns.map((label, index) => ({
    key: `c${String(index)}`,
    label,
    priority: index === 0 ? 'primary' : 'secondary',
  }));

  const label = labelOverride ?? t('display.type.table');
  const omittedRows = payload.table?.omitted_rows ?? 0;

  // No tabular data at all → the "No rows" empty state (Copywriting Contract).
  if (columns.length === 0 || rows.length === 0) {
    return (
      <DisplayCardShell label={label}>
        <EmptyState heading={t('display.table.emptyHeading')} body={t('display.table.emptyBody')} />
        {notice ? <p className="mt-2 text-xs text-text-muted">{notice}</p> : null}
        {omittedRows > 0 ? (
          <p className="mt-2 text-xs text-text-muted">
            {t('display.table.omittedRows', { count: omittedRows })}
          </p>
        ) : null}
      </DisplayCardShell>
    );
  }

  const total = filtered.length;
  const totalPages = Math.max(1, Math.ceil(total / perPage));
  const current = Math.min(page, totalPages - 1);
  const start = current * perPage;
  const visible = filtered.slice(start, start + perPage);
  const dataRows = visible.map((cells) =>
    Object.fromEntries(cells.map((value, index) => [`c${String(index)}`, value])),
  );
  const from = total === 0 ? 0 : start + 1;
  const to = Math.min(start + perPage, total);

  const actions = (
    <>
      <Button
        type="button"
        onClick={() => {
          copy(toTSV(columns, filtered));
        }}
        aria-label={t('display.table.copyAria')}
        variant="outline"
        className="px-3 text-[0.75rem] text-text-muted hover:text-text"
      >
        {copied ? t('display.table.copied') : t('display.table.copy')}
      </Button>
      <Button
        type="button"
        onClick={() => {
          copy(toCSV(columns, filtered));
        }}
        aria-label={t('display.table.exportAria')}
        variant="outline"
        className="px-3 text-[0.75rem] text-text-muted hover:text-text"
      >
        {t('display.table.exportCsv')}
      </Button>
    </>
  );

  return (
    <DisplayCardShell
      label={label}
      meta={t('display.table.rowCount', { count: rows.length })}
      actions={actions}
    >
      <div className="mb-3">
        <Input
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
          className="bg-surface text-sm"
        />
      </div>

      {total === 0 ? (
        <EmptyState
          heading={t('display.table.noMatchHeading')}
          body={t('display.table.noMatchBody', { query: filter.trim() })}
        />
      ) : (
        <>
          <DataTable
            columns={dataColumns}
            rows={dataRows}
            sort={sort}
            onSortChange={(next) => {
              setSort(next);
              setPage(0);
            }}
            caption={label}
            locale={i18n.language}
            sortByLabel={(column) => t('display.table.sortBy', { column })}
            ascendingLabel={t('display.table.sortedAsc')}
            descendingLabel={t('display.table.sortedDesc')}
          />
          {/* In-card pagination footer (D-PAGINATION): per-page + "X–Y of N" + prev/next,
              windowing ROWS at 3/page. Mirrors DisplayPagination's native chrome. */}
          <div className="mt-2 flex flex-wrap items-center justify-between gap-2">
            <div className="flex items-center gap-2">
              <label htmlFor={selectId} className="text-[0.75rem] font-medium text-text-muted">
                {t('display.pagination.perPage')}
              </label>
              <NativeSelect
                id={selectId}
                value={perPage}
                onChange={(e) => {
                  setPerPage(Number(e.target.value));
                  setPage(0);
                }}
                size="sm"
                className="w-16 bg-surface-2 text-xs"
              >
                {PER_PAGE_OPTIONS.map((n) => (
                  <NativeSelectOption key={n} value={n}>
                    {n}
                  </NativeSelectOption>
                ))}
              </NativeSelect>
            </div>

            <span aria-live="polite" className="text-[0.75rem] tabular-nums text-text-faint">
              {t('display.pagination.count', { from, to, total })}
            </span>

            {totalPages > 1 ? (
              <div className="flex items-center gap-1">
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
                <span className="text-[0.75rem] tabular-nums text-accent-text">{current + 1}</span>
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
          </div>
        </>
      )}
      {notice ? <p className="mt-2 text-xs text-text-muted">{notice}</p> : null}
      {omittedRows > 0 ? (
        <p className="mt-2 text-xs text-text-muted">
          {t('display.table.omittedRows', { count: omittedRows })}
        </p>
      ) : null}
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
