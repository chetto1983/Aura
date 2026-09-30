// S1.3 — the parity project rendered by (a) renderer-server on the JSON our toVideoJSON made, and
// by (b) our export page, on the same browser. Usage: node s1-parity.mjs <browser> <label> [ab|a|b] [capture-off]
// `label` names the run (e.g. online / offline: the container's network is the variable);
// `capture-off` passes renderer-server `elementCapture: false` (its per-layer rasterizer).
import { analyseFilm, audioDistance, framesDistance, summary } from './lib/analyse.mjs';
import { browserPaths } from './lib/browsers.mjs';
import { writeJson } from './lib/measure.mjs';
import { compileInPage, renderPage, renderServer } from './lib/render.mjs';
import { ROOT, startServer } from './lib/server.mjs';

const [name = 'chrome', label = 'run', only = 'ab', capture] = process.argv.slice(2);
const path = browserPaths()[name];
if (path === undefined) throw new Error(`${name} is not installed`);
const OUT = `${ROOT}out`;
const server = await startServer({ port: 0 });
const record = { browser: name, label };
try {
  const json = await compileInPage({ server, projectFile: 'project.json', browserPath: path });
  writeJson(`${OUT}/s1-videojson-${name}-${label}.json`, json);
  record.layers = json.layers.map((l) => `${l.type}:${l.settings.name}`);
  const films = {};
  if (only.includes('a')) {
    const file = `${OUT}/s1-a-${name}-${label}.mp4`;
    const run = await renderServer(json, {
      output: file,
      browserPath: name === 'chrome' ? undefined : path,
      elementCapture: capture === 'capture-off' ? false : undefined,
    });
    films.a = analyseFilm(file, `${OUT}/s1-a-${name}-${label}`);
    record.a = { seconds: Number(run.seconds.toFixed(1)), fonts: run.fonts, requests: summarise(run.requests, server.origin), logs: run.logs.slice(0, 40), ...summary(films.a) };
  }
  if (only.includes('b')) {
    const upload = `s1-b-${name}-${label}.mp4`;
    server.log.length = 0;
    const run = await renderPage({ server, projectFile: 'project.json', upload, browserPath: path, size: { width: 1920, height: 1080 } });
    films.b = analyseFilm(`${OUT}/${upload}`, `${OUT}/s1-b-${name}-${label}`);
    record.b = { ...run, seconds: Number(run.seconds.toFixed(1)), mediaRequests: mediaLog(server.log), ...summary(films.b) };
  }
  if (films.a && films.b) {
    record.audioAvsB = audioDistance(films.a.powers, films.b.powers);
    record.framesAvsB = framesDistance(films.a, films.b);
  }
} finally {
  await server.close();
}
writeJson(`${OUT}/s1-parity-${name}-${label}.json`, record);
console.log(JSON.stringify(record, null, 2));

function summarise(requests, origin) {
  const external = requests.filter((url) => !url.includes(origin) && !url.startsWith('https://videoflow.local/'));
  return { total: requests.length, local: requests.length - external.length, external: [...new Set(external)].slice(0, 30) };
}

function mediaLog(log) {
  const byPath = {};
  for (const entry of log.filter((e) => e.path.startsWith('/media/'))) {
    const key = `${entry.method} ${entry.path}${entry.query ? '?…' : ''}`;
    byPath[key] = (byPath[key] ?? 0) + 1;
  }
  return byPath;
}
