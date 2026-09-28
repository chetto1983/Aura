// Pure table-data helpers for TableDisplay — extracted so the .tsx stays
// component-only (react-refresh/only-export-components) and the serialization /
// sort logic is directly unit- and mutation-testable (the toolStatus.ts idiom).

import { compareDataTableValues, type DataTableSort } from '@/components/data-table-data';

export type SortDir = 'asc' | 'desc';

export interface SortState {
  readonly col: number;
  readonly dir: SortDir;
}

/** A cell is mono/numeric-shaped when it is a finite number (id/numeric column). */
export function isNumericCell(value: string): boolean {
  return value.trim() !== '' && !Number.isNaN(Number(value));
}

/** A cell that parses cleanly as a finite number sorts numerically; otherwise locale string. */
export function compareCells(a: string, b: string, locale = 'en-US'): number {
  return compareDataTableValues(a, b, locale);
}

/** RFC-4180-ish CSV: wrap a field in quotes and double interior quotes when needed. */
export function csvField(value: string): string {
  if (/[",\n\r]/.test(value)) return `"${value.replace(/"/g, '""')}"`;
  return value;
}

export function toCSV(columns: readonly string[], rows: readonly (readonly string[])[]): string {
  const header = columns.map(csvField).join(',');
  const body = rows.map((r) => r.map(csvField).join(',')).join('\r\n');
  return body === '' ? header : `${header}\r\n${body}`;
}

export function toTSV(columns: readonly string[], rows: readonly (readonly string[])[]): string {
  const header = columns.join('\t');
  const body = rows.map((r) => r.join('\t')).join('\n');
  return body === '' ? header : `${header}\n${body}`;
}

/** Apply the filter query then the active sort, returning the row window source. */
export function filterAndSort(
  rows: readonly (readonly string[])[],
  filter: string,
  sort: SortState | DataTableSort | null,
  columns?: readonly string[],
  locale = 'en-US',
): readonly (readonly string[])[] {
  const q = filter.trim().toLowerCase();
  const matched = q === '' ? rows : rows.filter((r) => r.some((c) => c.toLowerCase().includes(q)));
  if (sort === null) return matched;
  const col = 'key' in sort ? (/^c\d+$/.test(sort.key) ? Number(sort.key.slice(1)) : -1) : sort.col;
  const columnCount = columns?.length ?? rows[0]?.length ?? 0;
  if (!Number.isInteger(col) || col < 0 || col >= columnCount) return matched;
  const sorted = [...matched].sort((a, b) => compareCells(a[col] ?? '', b[col] ?? '', locale));
  const direction = 'key' in sort ? sort.direction : sort.dir;
  if (direction === 'desc') sorted.reverse();
  return sorted;
}

/** Toggle the sort for a column: a new column starts asc; the same column flips dir. */
export function nextSort(prev: SortState | null, col: number): SortState {
  return prev?.col === col
    ? { col, dir: prev.dir === 'asc' ? 'desc' : 'asc' }
    : { col, dir: 'asc' };
}
