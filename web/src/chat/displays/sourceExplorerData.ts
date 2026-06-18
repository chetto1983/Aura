import type { DisplaySource } from './types';

// sourceExplorerData — the pure search/sort/completeness logic behind the read-only
// Source Explorer (the toolStatus.ts / tableData.ts idiom). Keeping it off the .tsx
// makes it directly unit- and mutation-testable and keeps the component file
// component-only (react-refresh/only-export-components).

export type SourceSortKey = 'index' | 'type' | 'title' | 'source' | 'status';
export type SourceSortDir = 'asc' | 'desc';

export interface SourceSort {
  readonly key: SourceSortKey;
  readonly dir: SourceSortDir;
}

/** A source is "incomplete" when it lacks the fields a fully-processed source carries. */
export function sourceIncomplete(source: DisplaySource): boolean {
  const hasTitle = source.title !== undefined && source.title.length > 0;
  const hasUrl = source.url !== undefined && source.url.length > 0;
  return !hasTitle || !hasUrl;
}

/** True when any source in the registry is incomplete (drives the warning banner). */
export function anyIncomplete(sources: readonly DisplaySource[]): boolean {
  return sources.some(sourceIncomplete);
}

function sortKeyValue(source: DisplaySource, key: SourceSortKey): string | number {
  switch (key) {
    case 'index':
      return source.index;
    case 'type':
      return source.type ?? '';
    case 'title':
      return (source.title ?? '').toLowerCase();
    case 'source':
      return (source.url ?? '').toLowerCase();
    case 'status':
      return source.cited ? 0 : 1;
  }
}

/** Pure search+sort over the registry — extracted so it is directly mutation-tested. */
export function filterAndSortSources(
  sources: readonly DisplaySource[],
  query: string,
  sort: SourceSort | null,
): DisplaySource[] {
  const q = query.trim().toLowerCase();
  const matched =
    q.length === 0
      ? [...sources]
      : sources.filter((s) =>
          [s.title, s.url, s.snippet, s.type, s.ref_id]
            .filter((v): v is string => typeof v === 'string')
            .some((v) => v.toLowerCase().includes(q)),
        );
  if (sort === null) return matched;
  const factor = sort.dir === 'asc' ? 1 : -1;
  return matched.sort((a, b) => {
    const av = sortKeyValue(a, sort.key);
    const bv = sortKeyValue(b, sort.key);
    if (av < bv) return -1 * factor;
    if (av > bv) return 1 * factor;
    return (a.index - b.index) * factor;
  });
}

/** Toggle the sort for a column: none → asc → desc → asc (sticky on the same col). */
export function nextExplorerSort(prev: SourceSort | null, key: SourceSortKey): SourceSort {
  if (prev?.key !== key) return { key, dir: 'asc' };
  return { key, dir: prev.dir === 'asc' ? 'desc' : 'asc' };
}

/** Best-effort hostname extraction; falls back to the raw string on a parse error. */
export function safeHost(url: string): string {
  try {
    return new URL(url).hostname;
  } catch {
    return url;
  }
}
