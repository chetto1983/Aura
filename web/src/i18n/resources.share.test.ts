import { describe, expect, it } from 'vitest';
import { shareEn, shareIt } from './resources.share';

// resources.share.test.ts — the R-03 key-tree parity gate for the share i18n module. Walks
// BOTH shareEn and shareIt RECURSIVELY and asserts on the full set of leaf key PATHS (e.g.
// "share.modal.tier.internal.label"), not on a leaf count: a flat count comparison would pass
// two trees of equal size holding entirely different keys, which is exactly the defect this
// gate exists to catch (mirrors web/src/i18n/__tests__/resources.parity.test.ts's whole-bundle
// version, scoped here to the share module alone so it fails BEFORE the module is even wired
// into resources.ts).

function flattenPaths(obj: unknown, prefix = ''): string[] {
  if (obj === null || typeof obj !== 'object') return [prefix];
  return Object.entries(obj as Record<string, unknown>).flatMap(([key, value]) => {
    const next = prefix === '' ? key : `${prefix}.${key}`;
    return flattenPaths(value, next);
  });
}

describe('resources.share key-tree parity (en <-> it)', () => {
  const enPaths = new Set(flattenPaths(shareEn));
  const itPaths = new Set(flattenPaths(shareIt));

  it('has zero leaf paths present in shareEn but missing from shareIt', () => {
    const missing = [...enPaths].filter((path) => !itPaths.has(path));
    expect(missing).toEqual([]);
  });

  it('has zero leaf paths present in shareIt but missing from shareEn', () => {
    const missing = [...itPaths].filter((path) => !enPaths.has(path));
    expect(missing).toEqual([]);
  });

  it('exports both shareEn and shareIt under exactly one top-level "share" namespace key', () => {
    expect(Object.keys(shareEn)).toEqual(['share']);
    expect(Object.keys(shareIt)).toEqual(['share']);
  });

  it('includes the public-warning honesty note (cache/search-engine copies survive revoke)', () => {
    expect(shareEn.share.modal.publicWarning).toMatch(/cach|search engine/i);
    expect(shareIt.share.modal.publicWarning).toMatch(/cache|motori di ricerca/i);
  });

  it('includes the stale-snapshot {{count}} interpolation in both languages', () => {
    expect(shareEn.share.shared.stale).toContain('{{count}}');
    expect(shareIt.share.shared.stale).toContain('{{count}}');
  });
});
