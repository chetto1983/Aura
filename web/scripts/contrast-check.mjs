// WCAG 2.2 contrast gate for Aura theme tokens.
// Run: `node scripts/contrast-check.mjs`.
// Exits non-zero if any REQUIRED text/UI pair fails its AA threshold.
import { readFileSync } from 'node:fs';
import { fileURLToPath } from 'node:url';
import { dirname, join } from 'node:path';

const here = dirname(fileURLToPath(import.meta.url));
const tokens = JSON.parse(readFileSync(join(here, '..', 'tokens', 'tokens.json'), 'utf8'));
const THEMES = ['light', 'dark'];

function channel(c) {
  const s = c / 255;
  return s <= 0.03928 ? s / 12.92 : ((s + 0.055) / 1.055) ** 2.4;
}

function luminance(hex) {
  const h = hex.replace('#', '');
  const r = parseInt(h.slice(0, 2), 16);
  const g = parseInt(h.slice(2, 4), 16);
  const b = parseInt(h.slice(4, 6), 16);
  return 0.2126 * channel(r) + 0.7152 * channel(g) + 0.0722 * channel(b);
}

function ratio(fg, bg) {
  const a = luminance(fg);
  const b = luminance(bg);
  const [hi, lo] = a > b ? [a, b] : [b, a];
  return (hi + 0.05) / (lo + 0.05);
}

const SANITY = ratio('#FFFFFF', '#000000');
if (Math.abs(SANITY - 21) > 0.01) {
  console.error(`contrast math broken: white/black = ${SANITY.toFixed(2)}, expected 21.00`);
  process.exit(2);
}

const GRAPH_RAMP = [
  ['graph ramp source/info', '#AECBFA'],
  ['graph ramp agent/success', '#81C995'],
  ['graph ramp claim/warning', '#FDD663'],
  ['graph ramp conflict/danger', '#F28B82'],
  ['graph ramp blue-step', '#A7C7E7'],
  ['graph ramp topic/violet', '#B4A7D6'],
  ['graph ramp entity/teal', '#7FC9C3'],
  ['graph ramp violet-step', '#D7AEFB'],
];

function themePairs(themeName) {
  const color = tokens.color[themeName];
  const t = (key) => color[key];
  const prefix = (name) => `${themeName}: ${name}`;
  const textOn = (name, fg, bg) => [prefix(name), fg, bg, 4.5, 'text', false];
  const uiOn = (name, fg, bg) => [prefix(name), fg, bg, 3.0, 'ui', false];
  const pairs = [
    textOn('text on bg', t('text'), t('bg')),
    textOn('text on surface', t('text'), t('surface')),
    textOn('text on surface-2', t('text'), t('surface-2')),
    textOn('text on surface-3', t('text'), t('surface-3')),
    textOn('text-muted on bg', t('text-muted'), t('bg')),
    textOn('text-muted on surface', t('text-muted'), t('surface')),
    textOn('text-muted on surface-2', t('text-muted'), t('surface-2')),
    textOn('text-faint on bg', t('text-faint'), t('bg')),
    textOn('text-faint on surface', t('text-faint'), t('surface')),
    textOn('on-accent on accent', t('on-accent'), t('accent')),
    textOn('on-accent on accent-pressed', t('on-accent'), t('accent-pressed')),
    textOn('accent-text on bg', t('accent-text'), t('bg')),
    textOn('accent-text on surface', t('accent-text'), t('surface')),
    textOn('accent-text on surface-2', t('accent-text'), t('surface-2')),
    textOn('accent-text on surface-3', t('accent-text'), t('surface-3')),
    textOn('success on bg', t('success'), t('bg')),
    textOn('success on surface-2', t('success'), t('surface-2')),
    textOn('success on surface-3', t('success'), t('surface-3')),
    textOn('warning on bg', t('warning'), t('bg')),
    textOn('warning on surface-2', t('warning'), t('surface-2')),
    textOn('warning on surface-3', t('warning'), t('surface-3')),
    textOn('danger on bg', t('danger'), t('bg')),
    textOn('danger on surface-2', t('danger'), t('surface-2')),
    textOn('danger on surface-3', t('danger'), t('surface-3')),
    textOn('info on bg', t('info'), t('bg')),
    textOn('info on surface-2', t('info'), t('surface-2')),
    textOn('info on surface-3', t('info'), t('surface-3')),
    textOn('destructive foreground on danger', t('bg'), t('danger')),
    uiOn('ring on bg', t('ring'), t('bg')),
    uiOn('ring on surface', t('ring'), t('surface')),
    uiOn('ring on surface-2', t('ring'), t('surface-2')),
    uiOn('border-strong on bg', t('border-strong'), t('bg')),
    uiOn('border-strong on surface', t('border-strong'), t('surface')),
    uiOn('border-strong on surface-2', t('border-strong'), t('surface-2')),
    uiOn('border-strong on surface-3', t('border-strong'), t('surface-3')),
  ];

  // The Sigma graph canvas is intentionally dark in both app themes.
  for (const [name, hex] of GRAPH_RAMP) {
    pairs.push(uiOn(`${name} fill on graph bg`, hex, tokens.color.dark.bg));
    pairs.push(textOn(`${name} label on graph surface`, hex, tokens.color.dark.surface));
  }

  return pairs;
}

const PAIRS = THEMES.flatMap(themePairs);
let failures = 0;
const rows = PAIRS.map(([name, fg, bg, min, kind, advisory]) => {
  const r = ratio(fg, bg);
  const pass = r >= min;
  if (!pass && !advisory) failures += 1;
  const verdict = pass ? 'PASS' : advisory ? 'ADVISORY' : 'FAIL';
  return { name, fg, bg, ratio: r.toFixed(2), min: min.toFixed(1), kind, verdict };
});

const w = Math.max(...rows.map((r) => r.name.length));
const mark = { PASS: 'ok', ADVISORY: '..', FAIL: '!!' };
for (const r of rows) {
  console.log(
    `${mark[r.verdict]} ${r.name.padEnd(w)}  ${r.fg} on ${r.bg}  ` +
      `${r.ratio.padStart(6)}:1  (AA ${r.kind} >= ${r.min})`,
  );
}
const required = rows.filter((r) => r.verdict !== 'ADVISORY');
console.log(
  `\n${required.filter((r) => r.verdict === 'PASS').length}/${required.length} required pairs pass WCAG AA.`,
);
if (failures > 0) {
  console.error(`${String(failures)} required pair(s) BELOW threshold.`);
  process.exit(1);
}
