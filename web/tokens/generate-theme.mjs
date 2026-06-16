import { readFileSync, writeFileSync } from 'node:fs';
import { dirname, resolve } from 'node:path';
import { fileURLToPath } from 'node:url';

const here = dirname(fileURLToPath(import.meta.url));
const tokens = JSON.parse(readFileSync(resolve(here, 'tokens.json'), 'utf8'));

const THEME_KEY = 'aura.theme';
const DENSITY_KEY = 'aura.density';
const defaultTheme = tokens.$meta.defaultTheme;
const defaultDensity = tokens.$meta.defaultDensity;

const themeVars = Object.entries(tokens.color[defaultTheme])
  .map(([k, v]) => `  --color-${k}: ${v};`)
  .join('\n');
const radiusVars = Object.entries(tokens.radius)
  .map(([k, v]) => `  --radius-${k}: ${v};`)
  .join('\n');
const fontVars = Object.entries(tokens.font)
  .map(([k, v]) => `  --font-${k}: ${v};`)
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
}
${densityBlocks}
:root[data-density="${defaultDensity}"] { color-scheme: ${defaultTheme}; }
`;
writeFileSync(resolve(here, '../src/styles/theme.css'), css);

const snippet = `<!-- GENERATED from tokens/tokens.json — keep localStorage keys in sync with src/theme/applyTheme.ts -->
<script>
  (function () {
    try {
      var r = document.documentElement;
      r.setAttribute('data-theme', localStorage.getItem('${THEME_KEY}') || '${defaultTheme}');
      r.setAttribute('data-density', localStorage.getItem('${DENSITY_KEY}') || '${defaultDensity}');
    } catch (e) {
      document.documentElement.setAttribute('data-theme', '${defaultTheme}');
      document.documentElement.setAttribute('data-density', '${defaultDensity}');
    }
  })();
</script>`;
writeFileSync(resolve(here, '../src/styles/head-snippet.generated.html'), snippet + '\n');

process.stdout.write('generate-theme: wrote src/styles/theme.css + head-snippet.generated.html\n');
