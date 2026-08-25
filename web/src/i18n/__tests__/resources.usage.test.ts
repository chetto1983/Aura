import { readFileSync, readdirSync } from 'node:fs';
import { join, resolve } from 'node:path';
import { describe, expect, it } from 'vitest';
import { resources } from '../resources';

// i18n USAGE gate — every static `t('some.key')` in the app must resolve in BOTH locales.
//
// The parity gate beside this one proves en and it carry the same keys; it cannot see a key
// that neither locale has at the path the code asks for. That is how the MCP authorization
// panel shipped rendering `governance.mcp.authorization.title` as literal text to the
// operator: the strings existed, one level deeper, under `governance.mcp.detail.*`.
//
// Only STATIC literals are checked. A template key (`t(`governance.sections.${id}`)`) has no
// single value to look up here — those are covered by the component tests that render them.

const SRC = resolve(__dirname, '../..');
// The trailing character must not be a dot: `t('share.settings.tier.' + tier)` is a dynamic
// key wearing a static prefix, and reading it as static would report a phantom break.
const STATIC_T_CALL = /\bt\(\s*'([A-Za-z0-9_](?:[A-Za-z0-9_.]*[A-Za-z0-9_])?)'/g;

function sourceFiles(): string[] {
  return readdirSync(SRC, { recursive: true, encoding: 'utf8' })
    .filter((entry) => /\.tsx?$/.test(entry))
    .filter((entry) => !entry.includes('__tests__') && !/\.test\.tsx?$/.test(entry))
    .filter((entry) => !entry.startsWith('i18n'))
    .map((entry) => join(SRC, entry));
}

function flatten(obj: unknown, prefix = ''): string[] {
  if (obj === null || typeof obj !== 'object') return [prefix];
  return Object.entries(obj as Record<string, unknown>).flatMap(([key, value]) =>
    flatten(value, prefix === '' ? key : `${prefix}.${key}`),
  );
}

describe('i18n usage gate', () => {
  const enKeys = new Set(flatten(resources.en.translation));
  const itKeys = new Set(flatten(resources.it.translation));

  // i18next resolves a counted key through its `_one` / `_other` suffixes, so the bare path
  // is legitimately absent from the bundle.
  function resolves(key: string, bundle: ReadonlySet<string>): boolean {
    return bundle.has(key) || bundle.has(`${key}_one`) || bundle.has(`${key}_other`);
  }

  const used = new Map<string, string>();
  for (const file of sourceFiles()) {
    const text = readFileSync(file, 'utf8');
    for (const match of text.matchAll(STATIC_T_CALL)) {
      const key = match[1];
      if (key !== undefined && !used.has(key)) used.set(key, file);
    }
  }

  it('scanned a meaningful number of call sites', () => {
    // Guards the scanner itself: a regex that silently stops matching would make every
    // assertion below vacuously true.
    expect(used.size).toBeGreaterThan(200);
  });

  it('resolves every statically-referenced key in en', () => {
    const broken = [...used]
      .filter(([key]) => !resolves(key, enKeys))
      .map(([key, f]) => `${key} (${f})`);
    expect(broken).toEqual([]);
  });

  it('resolves every statically-referenced key in it', () => {
    const broken = [...used]
      .filter(([key]) => !resolves(key, itKeys))
      .map(([key, f]) => `${key} (${f})`);
    expect(broken).toEqual([]);
  });
});
