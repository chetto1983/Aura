// WCAG 2.2 contrast gate for the shipped (operator-accepted, 2026-06-18) blue palette.
// Re-proves docs/cockpit-overhaul/03-design-system-SPEC.md AC-8 against the REAL token
// values in tokens.json (the Editorial-Graphite gold values in the spec body are superseded
// by decision — see 03 Implementation Ledger). Run: `node scripts/contrast-check.mjs`.
// Exits non-zero if any REQUIRED pair fails its WCAG AA threshold.
import { readFileSync } from 'node:fs';
import { fileURLToPath } from 'node:url';
import { dirname, join } from 'node:path';

const here = dirname(fileURLToPath(import.meta.url));
const tokens = JSON.parse(readFileSync(join(here, '..', 'tokens', 'tokens.json'), 'utf8'));
const dark = tokens.color.dark;

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

// Sanity self-check: pure white on pure black is the WCAG maximum, 21:1. If this drifts
// the luminance math is wrong and every verdict below is untrustworthy.
const SANITY = ratio('#FFFFFF', '#000000');
if (Math.abs(SANITY - 21) > 0.01) {
  console.error(`contrast math broken: white/black = ${SANITY.toFixed(2)}, expected 21.00`);
  process.exit(2);
}

const t = (k) => dark[k];
// kind: 'text' → AA 1.4.3 normal 4.5; 'large' → AA 1.4.3 large 3.0; 'ui' → AA 1.4.11 non-text 3.0.
// advisory: WCAG 1.4.11 only requires 3:1 for a boundary that is the SOLE indicator of a
// component. Panel-divider borders here are reinforced by a background step (surface vs bg)
// AND interactive elements carry the focus ring (ring/bg, which passes strongly), so a
// resting decorative divider below 3:1 is reported but does NOT fail the gate. Bump the token
// one lightness step only if border-strong ever becomes the sole indicator of a control.
const PAIRS = [
  ['text on bg', t('text'), t('bg'), 4.5, 'text', false],
  ['text on surface', t('text'), t('surface'), 4.5, 'text', false],
  ['text on surface-2', t('text'), t('surface-2'), 4.5, 'text', false],
  ['text-muted on bg', t('text-muted'), t('bg'), 4.5, 'text', false],
  ['text-muted on surface', t('text-muted'), t('surface'), 4.5, 'text', false],
  ['text-faint on bg', t('text-faint'), t('bg'), 4.5, 'text', false],
  ['text-faint on surface', t('text-faint'), t('surface'), 4.5, 'text', false],
  ['on-accent on accent (button)', t('on-accent'), t('accent'), 4.5, 'text', false],
  ['accent-text on bg (link)', t('accent-text'), t('bg'), 4.5, 'text', false],
  ['accent-text on surface', t('accent-text'), t('surface'), 4.5, 'text', false],
  ['success on bg (status)', t('success'), t('bg'), 3.0, 'large', false],
  ['warning on bg (status)', t('warning'), t('bg'), 3.0, 'large', false],
  ['danger on bg (status)', t('danger'), t('bg'), 3.0, 'large', false],
  ['danger on surface-2 (tool pill)', t('danger'), t('surface-2'), 3.0, 'large', false],
  // Phase-29 governance WRITE surfaces (soft-warning card, RISKY skill strip, denied-tool
  // marker, enabled/preserved affirmations) introduce these new fg/bg pairs.
  [
    'warning on surface-2 (soft-warning / RISKY strip)',
    t('warning'),
    t('surface-2'),
    3.0,
    'large',
    false,
  ],
  ['warning on surface-3 (install preview)', t('warning'), t('surface-3'), 3.0, 'large', false],
  [
    'success on surface-2 (enabled / secret-preserved)',
    t('success'),
    t('surface-2'),
    3.0,
    'large',
    false,
  ],
  ['danger on surface-3 (install preview error)', t('danger'), t('surface-3'), 3.0, 'large', false],
  ['text on surface-3 (mono preview / input well)', t('text'), t('surface-3'), 4.5, 'text', false],
  ['ring on bg (focus)', t('ring'), t('bg'), 3.0, 'ui', false],
  [
    'border-strong on surface (decorative divider)',
    t('border-strong'),
    t('surface'),
    3.0,
    'ui',
    true,
  ],
];

// Phase-27 label-family ramp (web/src/graph/graphIntent.ts BRAND_RAMP). Each node-fill family
// must read against the near-black canvas (--color-bg) at the ≥3:1 UI-graphics threshold, and
// the legend/label TEXT version of each at ≥4.5:1 — color is never the only encoding (WCAG
// 1.4.1 / D-03), but the fills + legend text must still clear the contrast gate. The ramp is
// the single source the mapper + this check share, so there is no second per-label palette.
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
for (const [name, hex] of GRAPH_RAMP) {
  // Node fill vs canvas bg: ≥3:1 (UI graphics).
  PAIRS.push([`${name} fill on bg`, hex, t('bg'), 3.0, 'ui', false]);
  // Legend/label text vs surface panel: ≥4.5:1 (normal text).
  PAIRS.push([`${name} text on surface`, hex, t('surface'), 4.5, 'text', false]);
}

let failures = 0;
const rows = PAIRS.map(([name, fg, bg, min, kind, advisory]) => {
  const r = ratio(fg, bg);
  const pass = r >= min;
  if (!pass && !advisory) failures += 1;
  const verdict = pass ? 'PASS' : advisory ? 'ADVISORY' : 'FAIL';
  return { name, fg, bg, ratio: r.toFixed(2), min: min.toFixed(1), kind, verdict };
});

const w = Math.max(...rows.map((r) => r.name.length));
const mark = { PASS: '✓', ADVISORY: '•', FAIL: '✗' };
for (const r of rows) {
  console.log(
    `${mark[r.verdict]} ${r.name.padEnd(w)}  ${r.fg} on ${r.bg}  ` +
      `${r.ratio.padStart(6)}:1  (AA ${r.kind} ≥ ${r.min})${r.verdict === 'ADVISORY' ? '  [1.4.11 decorative]' : ''}`,
  );
}
const required = rows.filter((r) => r.verdict !== 'ADVISORY');
console.log(
  `\n${required.filter((r) => r.verdict === 'PASS').length}/${required.length} required pairs pass WCAG AA ` +
    `(palette: tokens.json color.dark; ${rows.length - required.length} advisory).`,
);
if (failures > 0) {
  console.error(`${String(failures)} required pair(s) BELOW threshold.`);
  process.exit(1);
}
