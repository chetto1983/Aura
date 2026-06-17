import { readFileSync, writeFileSync } from 'node:fs';
import { dirname, resolve } from 'node:path';
import { fileURLToPath } from 'node:url';

const here = dirname(fileURLToPath(import.meta.url));
const tokens = JSON.parse(readFileSync(resolve(here, 'tokens.json'), 'utf8'));

const THEME_KEY = 'aura.theme';
const DENSITY_KEY = 'aura.density';
const defaultTheme = tokens.$meta.defaultTheme;
const defaultDensity = tokens.$meta.defaultDensity;

function varsFrom(group, prefix) {
  return Object.entries(group)
    .filter(([k]) => !k.startsWith('_'))
    .map(([k, v]) => `  --${prefix}-${k}: ${v};`)
    .join('\n');
}

function colorVars(theme) {
  return Object.entries(theme)
    .filter(([k]) => !k.startsWith('_'))
    .map(([k, v]) => `  --color-${k}: ${v};`)
    .join('\n');
}

const themeVars = colorVars(tokens.color[defaultTheme]);
const radiusVars = varsFrom(tokens.radius, 'radius');
const fontVars = varsFrom(tokens.font, 'font');
const shadowVars = varsFrom(tokens.shadow ?? {}, 'shadow');
const motionVars = varsFrom(tokens.motion ?? {}, 'motion');
const themeBlocks = Object.entries(tokens.color)
  .map(
    ([theme, values]) => `:root[data-theme="${theme}"] {
  color-scheme: ${theme};
${colorVars(values)}
}`,
  )
  .join('\n');

const densityBlocks = Object.entries(tokens.density)
  .map(([tier, scalars]) => {
    const decls = Object.entries(scalars)
      .map(([k, v]) => `--${k}: ${v};`)
      .join(' ');
    return `:root[data-density="${tier}"] { ${decls} }`;
  })
  .join('\n');

const css = `/* GENERATED from tokens/tokens.json by generate-theme.mjs — do not hand-edit */
@theme {
${themeVars}
${radiusVars}
${fontVars}
${shadowVars}
${motionVars}
}
${themeBlocks}
${densityBlocks}
:root:not([data-theme]) { color-scheme: ${defaultTheme}; }
`;
writeFileSync(resolve(here, '../src/styles/theme.css'), css);

// The theme-before-paint script lives INLINE+SYNCHRONOUS in index.html (no-flash
// contract — it must run before first paint, so it cannot be a generated source
// file that gets bundled/deferred). tokens.json is the single source of the
// keys/defaults; assert the hand-maintained inline script still matches so a
// $meta change cannot silently leave index.html stale (drift gate, replaces the
// former unconsumed head-snippet.generated.html). Build fails on divergence.
const indexHtml = readFileSync(resolve(here, '../index.html'), 'utf8');
const required = [
  `localStorage.getItem('${THEME_KEY}') || '${defaultTheme}'`,
  `localStorage.getItem('${DENSITY_KEY}') || '${defaultDensity}'`,
  `setAttribute('data-theme', '${defaultTheme}')`,
  `setAttribute('data-density', '${defaultDensity}')`,
];
const missing = required.filter((needle) => !indexHtml.includes(needle));
if (missing.length > 0) {
  process.stderr.write(
    'generate-theme: index.html inline theme script is out of sync with tokens.json $meta.\n' +
      'Missing the following literals (update index.html to match the tokens):\n' +
      missing.map((m) => `  - ${m}`).join('\n') +
      '\n',
  );
  process.exit(1);
}

process.stdout.write(
  'generate-theme: wrote src/styles/theme.css; index.html inline theme script matches tokens.json $meta\n',
);
