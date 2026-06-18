import { afterEach, describe, expect, it } from 'vitest';
import {
  __resetShikiForTest,
  DARK_THEME,
  LIGHT_THEME,
  highlightCode,
  resolveLang,
} from '../shiki';

// shiki.ts pure-logic coverage: resolveLang maps raw lang tokens (incl. aliases,
// case, whitespace) to a supported grammar key or null (→ plain text). The heavy
// highlighter path is dynamic-import-only (A2) — exercised by the build code-split
// and the CodeDisplay test (which mocks the loader).

describe('resolveLang (shiki lang allow-list)', () => {
  it('resolves canonical supported langs', () => {
    expect(resolveLang('javascript')).toBe('javascript');
    expect(resolveLang('typescript')).toBe('typescript');
    expect(resolveLang('python')).toBe('python');
    expect(resolveLang('go')).toBe('go');
    expect(resolveLang('json')).toBe('json');
  });

  it('maps known aliases to their canonical grammar', () => {
    expect(resolveLang('js')).toBe('javascript');
    expect(resolveLang('jsx')).toBe('javascript');
    expect(resolveLang('ts')).toBe('typescript');
    expect(resolveLang('tsx')).toBe('typescript');
    expect(resolveLang('py')).toBe('python');
    expect(resolveLang('sh')).toBe('bash');
    expect(resolveLang('zsh')).toBe('bash');
    expect(resolveLang('yml')).toBe('yaml');
    expect(resolveLang('md')).toBe('markdown');
    expect(resolveLang('golang')).toBe('go');
  });

  it('is case-insensitive and trims whitespace', () => {
    expect(resolveLang('  JavaScript ')).toBe('javascript');
    expect(resolveLang('TS')).toBe('typescript');
  });

  it('returns null for an unsupported lang or undefined (→ plain escaped text)', () => {
    expect(resolveLang('brainfuck')).toBeNull();
    expect(resolveLang('')).toBeNull();
    expect(resolveLang(undefined)).toBeNull();
  });

  it('exposes the dark + light theme names', () => {
    expect(DARK_THEME).toBe('github-dark-default');
    expect(LIGHT_THEME).toBe('github-light-default');
  });
});

// The lazy loader runs against the REAL Shiki JS-regex engine (no WASM, jsdom-safe).
// These exercise the dynamic-import() path the build code-splits into the shiki chunk.
describe('highlightCode (lazy escaped-span highlighter)', () => {
  afterEach(() => {
    __resetShikiForTest();
  });

  it('returns escaped-span HTML for a supported lang (dark theme)', async () => {
    const html = await highlightCode('const x = 1;', 'javascript', true);
    expect(html).not.toBeNull();
    expect(html).toContain('<pre');
    expect(html).toContain('<span');
  });

  it('HTML-escapes a <script> in the body — tokenize-as-text, never executes (T-26-16)', async () => {
    const html = await highlightCode('<script>alert(1)</script>', 'javascript', true);
    expect(html).not.toBeNull();
    // Shiki entity-escapes the opening `<` (here as the hex entity &#x3C;), so no
    // live <script> element is ever emitted — the body renders as escaped text spans.
    expect(html).toContain('&#x3C;');
    expect(html).not.toContain('<script>alert(1)</script>');
    expect(html).not.toContain('<script>alert');
  });

  it('resolves an alias and caches the loaded grammar across calls', async () => {
    const first = await highlightCode('print(1)', 'py', true);
    const second = await highlightCode('print(2)', 'python', true);
    expect(first).not.toBeNull();
    expect(second).not.toBeNull();
  });

  it('honors the light theme', async () => {
    const html = await highlightCode('const x = 1;', 'javascript', false);
    expect(html).not.toBeNull();
    expect(html).toContain('<span');
  });

  it('returns null for an unsupported lang (caller keeps the plain <pre>)', async () => {
    const html = await highlightCode('x = 1', 'brainfuck', true);
    expect(html).toBeNull();
  });

  // Exercise each grammar loader in the allow-list (covers every LANG_LOADERS entry).
  it.each([
    ['typescript', 'const n: number = 1;'],
    ['json', '{"a":1}'],
    ['go', 'package main'],
    ['bash', 'echo hi'],
    ['shell', 'ls -la'],
    ['sql', 'SELECT 1'],
    ['html', '<div>x</div>'],
    ['css', 'a{color:red}'],
    ['yaml', 'a: 1'],
    ['markdown', '# H'],
  ])('loads the %s grammar and emits highlighted spans', async (lang, code) => {
    const html = await highlightCode(code, lang, true);
    expect(html).not.toBeNull();
    expect(html).toContain('<span');
  });
}, 30000);
