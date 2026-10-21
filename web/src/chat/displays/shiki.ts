import type { HighlighterCore } from 'shiki/core';

// shiki.ts (D-10 / A2): the LAZY code-highlighter loader. Everything heavy — the
// Shiki fine-grained core, the JS regex engine (no oniguruma WASM), the grammars,
// and the themes — is pulled behind dynamic import() so it lands in SEPARATE Vite
// chunks, NEVER in the embedded critical-path bundle. CodeDisplay renders a plain
// escaped <pre> first and upgrades to highlighted HTML only once this resolves.
//
// SECURITY — HARDEN-08 / T-26-16: Shiki TOKENIZES-AS-TEXT. codeToHtml HTML-escapes
// the code content and wraps tokens in <span style=…> — it NEVER executes the code
// and never emits live markup from the source. A <script> in the body becomes
// escaped text spans, not a script element.

// The selective grammar allow-list — only the langs likely to appear in tool/shell
// output. Each is a dynamic import so unused grammars never enter any chunk that
// loads at startup. An unrecognised lang falls back to `text` (plain escaped).
const LANG_LOADERS: Record<string, () => Promise<{ default: unknown }>> = {
  javascript: () => import('@shikijs/langs/javascript'),
  typescript: () => import('@shikijs/langs/typescript'),
  json: () => import('@shikijs/langs/json'),
  python: () => import('@shikijs/langs/python'),
  bash: () => import('@shikijs/langs/bash'),
  shell: () => import('@shikijs/langs/shellscript'),
  go: () => import('@shikijs/langs/go'),
  sql: () => import('@shikijs/langs/sql'),
  html: () => import('@shikijs/langs/html'),
  css: () => import('@shikijs/langs/css'),
  yaml: () => import('@shikijs/langs/yaml'),
  markdown: () => import('@shikijs/langs/markdown'),
  rust: () => import('@shikijs/langs/rust'),
  c: () => import('@shikijs/langs/c'),
  cpp: () => import('@shikijs/langs/cpp'),
  csharp: () => import('@shikijs/langs/csharp'),
  java: () => import('@shikijs/langs/java'),
  xml: () => import('@shikijs/langs/xml'),
  toml: () => import('@shikijs/langs/toml'),
  ruby: () => import('@shikijs/langs/ruby'),
  php: () => import('@shikijs/langs/php'),
  swift: () => import('@shikijs/langs/swift'),
  kotlin: () => import('@shikijs/langs/kotlin'),
  dart: () => import('@shikijs/langs/dart'),
  r: () => import('@shikijs/langs/r'),
  lua: () => import('@shikijs/langs/lua'),
  perl: () => import('@shikijs/langs/perl'),
  powershell: () => import('@shikijs/langs/powershell'),
  dockerfile: () => import('@shikijs/langs/dockerfile'),
  makefile: () => import('@shikijs/langs/makefile'),
  ini: () => import('@shikijs/langs/ini'),
  scss: () => import('@shikijs/langs/scss'),
  vue: () => import('@shikijs/langs/vue'),
  svelte: () => import('@shikijs/langs/svelte'),
  terraform: () => import('@shikijs/langs/terraform'),
  graphql: () => import('@shikijs/langs/graphql'),
  diff: () => import('@shikijs/langs/diff'),
};

const LANG_ALIASES: Record<string, string> = {
  js: 'javascript',
  jsx: 'javascript',
  ts: 'typescript',
  tsx: 'typescript',
  py: 'python',
  sh: 'bash',
  zsh: 'bash',
  yml: 'yaml',
  md: 'markdown',
  golang: 'go',
  rs: 'rust',
  h: 'c',
  hpp: 'cpp',
  cc: 'cpp',
  cxx: 'cpp',
  cs: 'csharp',
  rb: 'ruby',
  kt: 'kotlin',
  kts: 'kotlin',
  pl: 'perl',
  ps1: 'powershell',
  tf: 'terraform',
  gql: 'graphql',
  conf: 'ini',
  htm: 'html',
};

export const DARK_THEME = 'github-dark-default';
export const LIGHT_THEME = 'github-light-default';

/** Resolve a raw lang token to a supported grammar key, or null (→ plain text). */
export function resolveLang(lang: string | undefined): string | null {
  if (lang === undefined) return null;
  const key = lang.trim().toLowerCase();
  const canonical = LANG_ALIASES[key] ?? key;
  return canonical in LANG_LOADERS ? canonical : null;
}

let highlighterPromise: Promise<HighlighterCore> | null = null;
const loadedLangs = new Set<string>();

async function getHighlighter(): Promise<HighlighterCore> {
  highlighterPromise ??= (async () => {
    const [{ createHighlighterCore }, { createJavaScriptRegexEngine }, dark, light] =
      await Promise.all([
        import('shiki/core'),
        import('shiki/engine/javascript'),
        import('@shikijs/themes/github-dark-default'),
        import('@shikijs/themes/github-light-default'),
      ]);
    return createHighlighterCore({
      themes: [dark.default, light.default],
      langs: [],
      engine: createJavaScriptRegexEngine(),
    });
  })();
  return highlighterPromise;
}

async function ensureLang(highlighter: HighlighterCore, lang: string): Promise<void> {
  if (loadedLangs.has(lang)) return;
  const loader = LANG_LOADERS[lang];
  if (loader === undefined) return;
  const mod = await loader();
  await highlighter.loadLanguage(mod.default as never);
  loadedLangs.add(lang);
}

/**
 * Highlight a code body to ESCAPED-span HTML. Returns null when the lang is not in
 * the allow-list (the caller keeps its plain escaped <pre>) so we never run an
 * arbitrary grammar. The output is sanitize-safe HTML (escaped text + styled spans).
 */
export async function highlightCode(
  code: string,
  lang: string | undefined,
  isDark: boolean,
): Promise<string | null> {
  if (!canHighlight(code)) return null;
  const resolved = resolveLang(lang);
  if (resolved === null) return null;
  const highlighter = await getHighlighter();
  await ensureLang(highlighter, resolved);
  return highlighter.codeToHtml(code, {
    lang: resolved,
    theme: isDark ? DARK_THEME : LIGHT_THEME,
  });
}

export function canHighlight(code: string): boolean {
  return code.length <= 64 * 1024 && code.split('\n').length <= 2000 && !/[^\n]{8193}/.test(code);
}

/** Test-only: reset the memoised highlighter + loaded-lang cache. */
export function __resetShikiForTest(): void {
  highlighterPromise = null;
  loadedLangs.clear();
}
