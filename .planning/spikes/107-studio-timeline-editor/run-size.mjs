// Bundle cost of every added library, from the harness's production build (cd app && npx vite build):
// gzip of each emitted chunk, and gzip of each npm package's rendered modules (a package can share a
// chunk with another). Also the biggest single modules, the size of signalsmith-stretch's inline
// WASM, and whether a second mediabunny copy ships inside VideoFlow's worker string.
// Usage: node run-size.mjs
import { readFileSync, readdirSync, writeFileSync } from 'node:fs';
import { gzipSync } from 'node:zlib';

const DIST = 'out/dist';
const gz = (s) => gzipSync(Buffer.from(s)).length;
const kb = (n) => +(n / 1024).toFixed(1);

const chunks = readdirSync(`${DIST}/assets`).filter((f) => f.endsWith('.js') || f.endsWith('.css'))
  .map((f) => { const s = readFileSync(`${DIST}/assets/${f}`); return { file: f, raw: kb(s.length), gzip: kb(gzipSync(s).length) }; })
  .sort((a, b) => b.gzip - a.gzip);

const modules = JSON.parse(readFileSync(`${DIST}/modules.json`, 'utf8'));
const byPkg = {};
const biggest = [];
for (const c of modules) {
  for (const m of c.modules) {
    (byPkg[m.pkg] ??= { chunks: new Set(), code: [] }).code.push(m.code);
    byPkg[m.pkg].chunks.add(c.file);
    biggest.push({ id: m.id, chunk: c.file, raw: kb(m.code.length), gzip: kb(gz(m.code)) });
  }
}
const packages = Object.entries(byPkg).map(([pkg, v]) => ({ pkg, raw: kb(v.code.join('\n').length), gzip: kb(gz(v.code.join('\n'))), chunks: [...v.chunks] })).sort((a, b) => b.gzip - a.gzip);
biggest.sort((a, b) => b.gzip - a.gzip);

const stretch = readFileSync('app/node_modules/signalsmith-stretch/SignalsmithStretch.mjs', 'utf8');
const b64 = stretch.match(/data:application\/octet-stream;base64,([A-Za-z0-9+/=]+)/)?.[1] ?? '';
const worker = readFileSync('app/node_modules/@videoflow/renderer-browser/dist/workerBundle.js', 'utf8');
const report = {
  chunks,
  packages,
  biggestModules: biggest.slice(0, 12),
  signalsmithWasm: { base64Chars: b64.length, wasmBytes: Buffer.from(b64, 'base64').length, wasmGzip: kb(gzipSync(Buffer.from(b64, 'base64')).length) },
  videoflowWorkerString: { raw: kb(worker.length), gzip: kb(gz(worker)), mediabunnyPathsInside: (worker.match(/mediabunny\/dist\/modules\/src\/[\w/-]+\.js/g) ?? []).length },
};
writeFileSync('out/size-report.json', JSON.stringify(report, null, 2));
console.log('packages (KB gzip):'); for (const p of packages) console.log(`  ${p.pkg.padEnd(34)} ${String(p.gzip).padStart(7)}  (raw ${p.raw})`);
console.log('biggest modules:'); for (const m of report.biggestModules) console.log(`  ${m.id.padEnd(70)} ${m.gzip} KB gz`);
console.log('signalsmith WASM:', report.signalsmithWasm, 'VideoFlow worker string:', report.videoflowWorkerString);
