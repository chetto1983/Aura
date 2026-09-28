// Adapted from @assistant-ui/elements-data-table, https://r.assistant-ui.com/elements-data-table.json
// Registry snapshot fetched 2026-09-28, original SHA-256 E7B3B761F7E64EF081BAF1E8EA39AD9D6D26389AE49C888EA6234F07C01351E1.
// The registry snapshot has a fixed three-column model-usage shape; this adaptation uses the current standalone columns/rows/sort contract.
// MIT license and copyright notice: see LICENSE.assistant-ui in this directory.
'use client';

import { useState, type ComponentProps } from 'react';
import { compareDataTableValues, nextDataTableSort, type DataTableSort } from './data-table-data';
import { cn } from '@/lib/utils';

export interface DataTableColumn {
  key: string;
  label: string;
  priority?: 'primary' | 'secondary';
  sortable?: boolean;
}

export type DataTableRow = Readonly<Record<string, string>>;
export function DataTable({
  columns,
  rows,
  sort,
  onSortChange,
  caption,
  locale = 'en-US',
  sortByLabel = (label) => `Sort by ${label}`,
  ascendingLabel = 'ascending',
  descendingLabel = 'descending',
  className,
  ...props
}: Omit<ComponentProps<'div'>, 'children'> & {
  columns: readonly DataTableColumn[];
  rows: readonly DataTableRow[];
  sort?: DataTableSort | null;
  onSortChange?: (next: DataTableSort | null) => void;
  caption?: string;
  locale?: string;
  sortByLabel?: (label: string) => string;
  ascendingLabel?: string;
  descendingLabel?: string;
}) {
  const [internalSort, setInternalSort] = useState<DataTableSort | null>(null);
  const activeSort = sort === undefined ? internalSort : sort;
  const key = activeSort?.key;
  const sorted =
    key && columns.some((column) => column.key === key)
      ? [...rows].sort((a, b) => {
          const result = compareDataTableValues(a[key] ?? '', b[key] ?? '', locale);
          return activeSort.direction === 'desc' ? -result : result;
        })
      : rows;
  const primary = columns.find((column) => column.priority === 'primary') ?? columns[0];

  const changeSort = (column: DataTableColumn) => {
    const next = nextDataTableSort(activeSort, column.key);
    if (sort === undefined) setInternalSort(next);
    onSortChange?.(next);
  };

  return (
    <div data-slot="data-table" className={cn('@container w-full min-w-0', className)} {...props}>
      <div className="hidden max-w-full overflow-x-auto rounded-[var(--radius-md)] border border-border @md:block">
        <table className="min-w-full border-collapse text-left text-sm">
          {caption ? <caption className="sr-only">{caption}</caption> : null}
          <thead>
            <tr>
              {columns.map((column) => {
                const direction = activeSort?.key === column.key ? activeSort.direction : undefined;
                return (
                  <th
                    key={column.key}
                    scope="col"
                    aria-sort={
                      direction === 'asc'
                        ? 'ascending'
                        : direction === 'desc'
                          ? 'descending'
                          : 'none'
                    }
                    className="border-b border-border bg-surface-2 p-0 text-left"
                  >
                    {column.sortable === false ? (
                      <span className="block px-3 py-2 text-xs font-medium text-text-muted">
                        {column.label}
                      </span>
                    ) : (
                      <button
                        type="button"
                        onClick={() => {
                          changeSort(column);
                        }}
                        aria-label={sortByLabel(column.label)}
                        className="min-h-11 w-full px-3 py-2 text-left text-xs font-medium text-text-muted hover:text-text focus-visible:outline-2 focus-visible:outline-accent"
                      >
                        {column.label}
                        {direction ? (
                          <span className="sr-only">
                            {direction === 'asc' ? ascendingLabel : descendingLabel}
                          </span>
                        ) : null}
                      </button>
                    )}
                  </th>
                );
              })}
            </tr>
          </thead>
          <tbody>
            {sorted.map((row, index) => (
              <tr key={index} className="hover:bg-surface-2">
                {columns.map((column) => (
                  <td key={column.key} className="border-b border-border px-3 py-2 text-text-muted">
                    {row[column.key] ?? ''}
                  </td>
                ))}
              </tr>
            ))}
          </tbody>
        </table>
      </div>
      <div data-slot="data-table-cards" className="space-y-2 @md:hidden">
        <div className="flex flex-wrap gap-2">
          {columns
            .filter((column) => column.sortable !== false)
            .map((column) => (
              <button
                key={column.key}
                type="button"
                onClick={() => {
                  changeSort(column);
                }}
                aria-label={sortByLabel(column.label)}
                className="min-h-11 rounded-[var(--radius-md)] border border-border px-3 text-xs text-text-muted focus-visible:outline-2 focus-visible:outline-accent"
              >
                {column.label}
                {activeSort?.key === column.key
                  ? ` (${activeSort.direction === 'asc' ? ascendingLabel : descendingLabel})`
                  : ''}
              </button>
            ))}
        </div>
        <div role="list" className="space-y-2">
          {sorted.map((row, index) => (
            <div
              key={index}
              role="listitem"
              className="min-w-0 rounded-[var(--radius-md)] border border-border bg-surface p-3 text-sm"
            >
              {primary ? (
                <p className="break-words font-medium text-text">{row[primary.key] ?? ''}</p>
              ) : null}
              <dl className="mt-2 space-y-1">
                {columns
                  .filter((column) => column.key !== primary?.key)
                  .map((column) => (
                    <div key={column.key} className="flex min-w-0 justify-between gap-3">
                      <dt className="shrink-0 text-text-faint">{column.label}</dt>
                      <dd className="min-w-0 break-words text-right text-text-muted">
                        {row[column.key] ?? ''}
                      </dd>
                    </div>
                  ))}
              </dl>
            </div>
          ))}
        </div>
      </div>
    </div>
  );
}
