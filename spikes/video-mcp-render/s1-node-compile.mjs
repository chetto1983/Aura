// S1.2 — can toVideoJSON and its compile chain run in Node unchanged? Four attempts, each on
// web/src/videoStudio/videoflow.ts as it is:
//   1. Node's own TypeScript loader (type stripping, on by default in Node 24);
//   2. an esbuild bundle for Node with packages left to Node's resolver, so a failure names the
//      package file;
//   3. an esbuild bundle with the packages inlined too;
//   4. a bundle with @videoflow/renderer-browser swapped for an empty module — a probe for what
//      ELSE blocks, not a fix.
// 3 and 4 are compared with the JSON the browser compiled (s1-parity output, the first argument),
// ignoring the per-layer `id`, which VideoFlow draws at random on every compile, and the port.
// Usage (in WSL, where web/src is): node s1-node-compile.mjs out/vm/s1-videojson-chrome-offline.json
import { build } from 'esbuild';
import { readFileSync, writeFileSync } from 'node:fs';
import { parityProject } from './lib/project.mjs';

const ROOT = new URL('.', import.meta.url).pathname;
const VIDEOFLOW = `${ROOT}../../web/src/videoStudio/videoflow.ts`;
const urls = { assetUrl: (id) => `http://127.0.0.1:0/media/${encodeURIComponent(id)}?X-Amz-Signature=spike` };
const results = {};
const first = (error) => String(error?.stack ?? error).split('\n').slice(0, 4).join(' | ');
const browserJson = process.argv[2] && JSON.parse(readFileSync(process.argv[2], 'utf8'));

function canonical(json) {
  const strip = (layers) => layers.map(({ id: _id, children, ...layer }) => (children ? { ...layer, children: strip(children) } : layer));
  return JSON.stringify({ ...json, layers: strip(json.layers) }).replace(/127\.0\.0\.1:\d+/g, '127.0.0.1:PORT');
}

async function attempt(name, entry) {
  try {
    const mod = await import(entry);
    const json = await mod.toVideoJSON(parityProject(), urls);
    writeFileSync(`${ROOT}out/s1-videojson-node-${name}.json`, JSON.stringify(json, null, 2));
    results[name] = {
      ran: true,
      layers: json.layers.length,
      duration: json.duration,
      ...(browserJson ? { identicalToBrowserIgnoringIds: canonical(json) === canonical(browserJson) } : {}),
    };
  } catch (error) {
    results[name] = { error: first(error) };
  }
}

const bundle = (outfile, options) =>
  build({
    stdin: { contents: `export { toVideoJSON } from ${JSON.stringify(VIDEOFLOW)};`, resolveDir: ROOT, loader: 'ts' },
    outfile,
    bundle: true,
    format: 'esm',
    platform: 'node',
    target: 'node24',
    logLevel: 'error',
    ...options,
  });

await attempt('typeStripping', VIDEOFLOW);
await bundle(`${ROOT}out/node-videoflow.mjs`, { packages: 'external' });
await attempt('bundledPackagesExternal', `${ROOT}out/node-videoflow.mjs`);
await bundle(`${ROOT}out/node-videoflow-all.mjs`, {});
await attempt('bundledAll', `${ROOT}out/node-videoflow-all.mjs`);
await bundle(`${ROOT}out/node-videoflow-stub.mjs`, {
  alias: { '@videoflow/renderer-browser': `${ROOT}src/renderer-browser-stub.mjs` },
  external: ['@videoflow/core'],
});
await attempt('rendererStubbed', `${ROOT}out/node-videoflow-stub.mjs`);
writeFileSync(`${ROOT}out/s1-node-compile.json`, `${JSON.stringify(results, null, 2)}\n`);
console.log(JSON.stringify(results, null, 2));
