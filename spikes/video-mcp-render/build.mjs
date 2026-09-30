// Bundles the probe pages with esbuild. The Studio modules are imported from web/src UNCHANGED, and
// their packages resolve from web/node_modules, so the page runs the cockpit's own versions. Two
// Vite behaviours are reproduced and nothing else:
// - `?url` imports: the file is copied under dist/assets and the import is its URL (Vite's
//   "explicit URL import", https://vite.dev/guide/assets#explicit-url-imports);
// - node builtins reached only from a Node branch (fvad.js `await import("module")`) stay external,
//   as Vite externalizes them for the browser.
// The cockpit's fonts are copied to dist/fonts, where `LOCAL_FONTS` (videoflow.ts:37) points.
import { build } from 'esbuild';
import { copyFileSync, cpSync, mkdirSync, writeFileSync } from 'node:fs';
import { basename } from 'node:path';
import { fileURLToPath } from 'node:url';

const ROOT = fileURLToPath(new URL('.', import.meta.url));
const WEB = `${ROOT}../../web`;
const DIST = `${ROOT}dist`;
mkdirSync(`${DIST}/assets`, { recursive: true });

const urlImports = {
  name: 'url-imports',
  setup(b) {
    b.onResolve({ filter: /\?url$/ }, async (args) => {
      const resolved = await b.resolve(args.path.slice(0, -'?url'.length), {
        resolveDir: args.resolveDir,
        kind: 'import-statement',
      });
      if (resolved.errors.length > 0) return { errors: resolved.errors };
      return { path: resolved.path, namespace: 'url-import' };
    });
    b.onLoad({ filter: /.*/, namespace: 'url-import' }, (args) => {
      const name = basename(args.path);
      copyFileSync(args.path, `${DIST}/assets/${name}`);
      return { contents: `export default ${JSON.stringify(`/dist/assets/${name}`)};`, loader: 'js' };
    });
  },
};

const page = (entry, out) =>
  build({
    entryPoints: [`${ROOT}src/${entry}`],
    outfile: `${DIST}/${out}`,
    bundle: true,
    format: 'esm',
    platform: 'browser',
    target: 'es2022',
    external: ['module', 'fs', 'path', 'url'],
    plugins: [urlImports],
    logLevel: 'warning',
  });

await page('studio.ts', 'studio.js');
cpSync(`${WEB}/public/fonts`, `${DIST}/fonts`, { recursive: true });
const html = (script) =>
  `<!doctype html><html><head><meta charset="utf-8"></head><body>${script ? `<script type="module" src="/dist/${script}"></script>` : ''}</body></html>\n`;
writeFileSync(`${DIST}/blank.html`, html());
writeFileSync(`${DIST}/studio.html`, html('studio.js'));
console.log(`built ${DIST}`);
