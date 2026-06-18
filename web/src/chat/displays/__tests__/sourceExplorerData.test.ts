import { describe, expect, it } from 'vitest';
import {
  anyIncomplete,
  filterAndSortSources,
  nextExplorerSort,
  safeHost,
  sourceIncomplete,
} from '../sourceExplorerData';
import type { DisplaySource } from '../types';

// Exhaustive unit coverage for the pure Source Explorer logic (every sort key, the
// search predicate, completeness, and the safeHost parse paths) — the toolStatus.ts
// idiom: logic off the .tsx, directly mutation-tested.

interface Override {
  ref_id: string;
  index: number;
  title?: string | undefined;
  url?: string | undefined;
  snippet?: string | undefined;
  type?: DisplaySource['type'] | undefined;
  cited?: boolean;
  confidence?: number;
}

function s(over: Override): DisplaySource {
  const type = 'type' in over ? over.type : 'web_result';
  const title = 'title' in over ? over.title : `t${String(over.index)}`;
  const url = 'url' in over ? over.url : `https://h${String(over.index)}.test/p`;
  const snippet = 'snippet' in over ? over.snippet : 'snip';
  return {
    ref_id: over.ref_id,
    index: over.index,
    cited: over.cited ?? false,
    ...(type !== undefined ? { type } : {}),
    ...(title !== undefined ? { title } : {}),
    ...(url !== undefined ? { url } : {}),
    ...(snippet !== undefined ? { snippet } : {}),
    ...(over.confidence !== undefined ? { confidence: over.confidence } : {}),
  };
}

const A = s({ ref_id: 'a', index: 3, title: 'Charlie', type: 'document', url: 'https://c.test', cited: false });
const B = s({ ref_id: 'b', index: 1, title: 'Alpha', type: 'web_result', url: 'https://a.test', cited: true });
const C = s({ ref_id: 'c', index: 2, title: 'Bravo', type: 'code', url: 'https://b.test', cited: false });
const ALL: readonly DisplaySource[] = [A, B, C];

describe('filterAndSortSources — every sort key', () => {
  it('sorts by index asc/desc', () => {
    expect(filterAndSortSources(ALL, '', { key: 'index', dir: 'asc' }).map((x) => x.index)).toEqual([
      1, 2, 3,
    ]);
    expect(filterAndSortSources(ALL, '', { key: 'index', dir: 'desc' }).map((x) => x.index)).toEqual(
      [3, 2, 1],
    );
  });

  it('sorts by type', () => {
    expect(filterAndSortSources(ALL, '', { key: 'type', dir: 'asc' }).map((x) => x.type)).toEqual([
      'code',
      'document',
      'web_result',
    ]);
  });

  it('sorts by title', () => {
    expect(filterAndSortSources(ALL, '', { key: 'title', dir: 'asc' }).map((x) => x.title)).toEqual([
      'Alpha',
      'Bravo',
      'Charlie',
    ]);
  });

  it('sorts by source (url)', () => {
    expect(filterAndSortSources(ALL, '', { key: 'source', dir: 'asc' }).map((x) => x.url)).toEqual([
      'https://a.test',
      'https://b.test',
      'https://c.test',
    ]);
  });

  it('sorts by status (cited first asc)', () => {
    expect(filterAndSortSources(ALL, '', { key: 'status', dir: 'asc' })[0]?.ref_id).toBe('b');
  });

  it('returns the original order (copy) with no sort', () => {
    const out = filterAndSortSources(ALL, '', null);
    expect(out.map((x) => x.ref_id)).toEqual(['a', 'b', 'c']);
    expect(out).not.toBe(ALL); // a copy, not the same reference
  });

  it('breaks ties by index when key values are equal', () => {
    const x = s({ ref_id: 'x', index: 2, title: 'Same', cited: false });
    const y = s({ ref_id: 'y', index: 1, title: 'Same', cited: false });
    expect(filterAndSortSources([x, y], '', { key: 'title', dir: 'asc' }).map((r) => r.index)).toEqual(
      [1, 2],
    );
  });
});

describe('filterAndSortSources — search predicate', () => {
  it('matches title, url, snippet, type and ref_id (case-insensitive)', () => {
    expect(filterAndSortSources(ALL, 'charlie', null).map((x) => x.ref_id)).toEqual(['a']);
    expect(filterAndSortSources(ALL, 'A.TEST', null).map((x) => x.ref_id)).toEqual(['b']);
    expect(filterAndSortSources(ALL, 'snip', null)).toHaveLength(3);
    expect(filterAndSortSources(ALL, 'code', null).map((x) => x.ref_id)).toEqual(['c']);
  });

  it('ignores undefined string fields without throwing', () => {
    const bare = s({ ref_id: 'bare', index: 5, title: undefined, snippet: undefined, type: undefined });
    expect(filterAndSortSources([bare], 'nomatch', null)).toHaveLength(0);
    expect(filterAndSortSources([bare], 'bare', null)).toHaveLength(1); // ref_id matches
  });
});

describe('nextExplorerSort', () => {
  it('asc on a fresh column, toggles on the same column, asc on a new column', () => {
    expect(nextExplorerSort(null, 'index')).toEqual({ key: 'index', dir: 'asc' });
    expect(nextExplorerSort({ key: 'index', dir: 'asc' }, 'index')).toEqual({ key: 'index', dir: 'desc' });
    expect(nextExplorerSort({ key: 'index', dir: 'desc' }, 'index')).toEqual({ key: 'index', dir: 'asc' });
    expect(nextExplorerSort({ key: 'index', dir: 'asc' }, 'title')).toEqual({ key: 'title', dir: 'asc' });
  });
});

describe('completeness', () => {
  it('sourceIncomplete flags missing title or url', () => {
    expect(sourceIncomplete(s({ ref_id: 'a', index: 1 }))).toBe(false);
    expect(sourceIncomplete(s({ ref_id: 'a', index: 1, title: '' }))).toBe(true);
    expect(sourceIncomplete(s({ ref_id: 'a', index: 1, url: '' }))).toBe(true);
  });

  it('anyIncomplete is true iff some source is incomplete', () => {
    expect(anyIncomplete(ALL)).toBe(false);
    expect(anyIncomplete([...ALL, s({ ref_id: 'z', index: 9, title: '' })])).toBe(true);
  });
});

describe('safeHost', () => {
  it('extracts the hostname from a valid URL', () => {
    expect(safeHost('https://example.com/path?q=1')).toBe('example.com');
  });

  it('falls back to the raw string on a parse error', () => {
    expect(safeHost('not a url')).toBe('not a url');
  });
});
