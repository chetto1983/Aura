// S1.4 — how each pipeline reaches its media, and which way survives a presigned GET from another
// origin. A short project (the 5 s clip + 5 s of music) is rendered with its media on a SECOND
// origin carrying a presigned-style query string, once without Access-Control-Allow-Origin and once
// with it, as a Garage bucket would answer with and without a CORS rule. Usage:
// node s1-media.mjs <browser>
import { writeFileSync } from 'node:fs';
import { browserPaths } from './lib/browsers.mjs';
import { frameAt, levelBetween, meanRgb, probe, windowPowers, writeJson } from './lib/measure.mjs';
import { compileInPage, renderPage, renderServer } from './lib/render.mjs';
import { ROOT, startServer } from './lib/server.mjs';

const [name = 'chrome'] = process.argv.slice(2);
const path = browserPaths()[name];
const QUERY = '?X-Amz-Algorithm=AWS4-HMAC-SHA256&X-Amz-Expires=900&X-Amz-Signature=spike';
writeFileSync(
  `${ROOT}fixtures/project-small.json`,
  JSON.stringify({
    id: 'small',
    name: 'small',
    size: { width: 1920, height: 1080 },
    fps: 30,
    sources: [
      { id: 's1', assetId: 'clip-1080p-5s.mp4', kind: 'video', duration: 5, size: { width: 1920, height: 1080 }, hasAudio: true },
      { id: 's2', assetId: 'music-bed.wav', kind: 'audio', duration: 50, size: { width: 0, height: 0 } },
    ],
    video: [{ id: 'c1', sourceId: 's1', duration: 5, sourceStart: 0, muted: false }],
    overlays: [],
    audio: [{ id: 'l1', items: [{ id: 'm', sourceId: 's2', anchor: { clipId: 'c1', offset: 0 }, sourceStart: 0, duration: 5, volume: 0.5, muted: false }] }],
  }),
);

const page = await startServer({ port: 0 });
const noCors = await startServer({ port: 0, cors: false });
const withCors = await startServer({ port: 0, cors: true });
const results = {};
const outcome = async (label, run, file) => {
  try {
    const value = await run();
    // A render that "succeeds" with a black picture or silence is the failure to look for.
    results[label] = {
      ok: true,
      seconds: Number(value.seconds.toFixed(1)),
      info: probe(file),
      frameMeanRgb: meanRgb(frameAt(file, 2.5)),
      level: levelBetween(windowPowers(file), 0.5, 4.5),
      logs: (value.logs ?? []).filter((l) => /error|fail|cors/i.test(l)).slice(0, 6),
    };
  } catch (error) {
    results[label] = { ok: false, error: String(error?.message ?? error).split('\n')[0].slice(0, 400), logs: error?.logs?.filter((l) => /error|fail|cors/i.test(l)).slice(0, 6) };
  }
};
try {
  for (const [label, media] of [['cross-origin-no-cors', noCors], ['cross-origin-cors', withCors]]) {
    media.log.length = 0;
    await outcome(`b ${label}`, () =>
      renderPage({ server: page, projectFile: 'project-small.json', upload: `s1-media-b-${label}.mp4`, browserPath: path, size: { width: 1920, height: 1080 }, mediaQuery: QUERY, mediaOrigin: media.origin }),
      `${ROOT}out/s1-media-b-${label}.mp4`,
    );
    results[`b ${label}`].mediaServerSaw = media.log.map((e) => `${e.method} ${e.path}${e.query ? '?…' : ''}`);
    // (a) gets the same URLs inside its compiled JSON: its page asks for them, and its route
    // handler fetches them from Node (ServerRenderer.js:633-655).
    media.log.length = 0;
    const json = JSON.parse(
      JSON.stringify(await compileInPage({ server: page, projectFile: 'project-small.json', browserPath: path, mediaQuery: QUERY })).replaceAll(page.origin, media.origin),
    );
    await outcome(`a ${label}`, () => renderServer(json, { output: `${ROOT}out/s1-media-a-${label}.mp4`, browserPath: name === 'chrome' ? undefined : path }), `${ROOT}out/s1-media-a-${label}.mp4`);
    results[`a ${label}`].mediaServerSaw = media.log.map((e) => `${e.method} ${e.path}${e.query ? '?…' : ''}`);
  }
} finally {
  await Promise.all([page.close(), noCors.close(), withCors.close()]);
}
writeJson(`${ROOT}out/s1-media-${name}.json`, results);
console.log(JSON.stringify(results, null, 2));
