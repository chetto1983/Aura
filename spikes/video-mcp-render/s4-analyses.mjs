// S4 — the Studio's two browser-only analyses (audioSpeech.ts detectSpeech, audioClean.ts
// denoiseSamples + cleanedFile), bundled unchanged, in the same headless page the export runs in.
// Scored exactly as Plan A scored them (spikes/video-studio-audio/src/s3.ts `score`, s4.ts `edges`)
// so the numbers line up with spikes/video-studio-audio/FINDINGS.md S3/S4. Usage:
// node s4-analyses.mjs <browser>
import { browserPaths, launch } from './lib/browsers.mjs';
import { writeJson } from './lib/measure.mjs';
import { ROOT, startServer } from './lib/server.mjs';

const [name = 'chrome'] = process.argv.slice(2);
const server = await startServer({ port: 0 });
const browser = await launch(browserPaths()[name]);
const requests = [];
const logs = [];
let results;
try {
  const page = await browser.newPage();
  page.on('request', (r) => requests.push(r.url()));
  page.on('console', (m) => logs.push(`${m.type()}: ${m.text()}`.slice(0, 300)));
  page.on('pageerror', (e) => logs.push(`pageerror: ${e.message}`.slice(0, 300)));
  await page.goto(`${server.origin}/dist/studio.html`);
  await page.waitForFunction(() => 'studio' in window);
  results = await page.evaluate(async () => {
    const { studio } = window;
    const RATE = studio.DENOISE_RATE;
    const rmsDb = (x, from, to) => {
      let sum = 0;
      for (let i = from; i < to; i += 1) sum += (x[i] ?? 0) ** 2;
      return 10 * Math.log10(sum / Math.max(1, to - from) + 1e-20);
    };
    // Plan A s3.ts score(): speech inside each window 50 ms from its edges, gaps 150 ms clear.
    const score = (out, windows) => {
      const at = (s) => Math.round(s * RATE);
      const speech = windows.map(([s, e]) => rmsDb(out, at(s + 0.05), at(e - 0.05)));
      const gaps = [];
      let from = 0;
      for (const [s, e] of windows) {
        gaps.push(rmsDb(out, at(from + 0.15), at(s - 0.15)));
        from = e;
      }
      const mean = (xs) => xs.reduce((a, b) => a + b, 0) / xs.length;
      return { speechDb: Number(mean(speech).toFixed(2)), gapDb: Number(mean(gaps).toFixed(2)) };
    };
    // Plan A s4.ts edges(): [start error, end error] in ms per truth window.
    const edges = (found, truth) => {
      const errors = truth.map(([ts, te]) => {
        const hit = found.find(([s, e]) => s < te && e > ts);
        return hit === undefined ? null : [Math.round((hit[0] - ts) * 1000), Math.round((hit[1] - te) * 1000)];
      });
      return {
        errorsMs: errors,
        misses: errors.filter((e) => e === null).length,
        falseWindows: found.filter(([s, e]) => !truth.some(([ts, te]) => s < te && e > ts)).length,
      };
    };
    const truth = (await (await fetch('/media/speech.truth.json')).json()).windows;
    const out = [];
    for (const file of ['speech.wav', 'speech-noisy.wav']) {
      const url = `/media/${file}`;
      let t = performance.now();
      const windows = await studio.detectSpeech(url);
      const speechSeconds = (performance.now() - t) / 1000;
      const base = await studio.decodeMono(url, RATE);
      t = performance.now();
      const clean = await studio.denoiseSamples(base.slice());
      const denoiseSeconds = (performance.now() - t) / 1000;
      t = performance.now();
      const ogg = await studio.cleanedFile(url, file);
      const encodeSeconds = (performance.now() - t) / 1000;
      const decoded = await new OfflineAudioContext(1, 1, RATE).decodeAudioData(await ogg.arrayBuffer());
      const b = score(base, truth);
      const c = score(clean, truth);
      const o = score(decoded.getChannelData(0), truth);
      out.push({
        file,
        seconds: Number((base.length / RATE).toFixed(3)),
        speech: { windows: windows.map(([s, e]) => [Number(s.toFixed(3)), Number(e.toFixed(3))]), ...edges(windows, truth), seconds: Number(speechSeconds.toFixed(2)) },
        denoise: {
          input: b,
          cleaned: c,
          floorDropDb: Number((b.gapDb - c.gapDb).toFixed(1)),
          speechLossDb: Number((b.speechDb - c.speechDb).toFixed(1)),
          seconds: Number(denoiseSeconds.toFixed(2)),
        },
        cleanedFile: {
          name: ogg.name,
          type: ogg.type,
          bytes: ogg.size,
          decodedRate: decoded.sampleRate,
          decodedSeconds: Number(decoded.duration.toFixed(3)),
          floorDropDb: Number((b.gapDb - o.gapDb).toFixed(1)),
          speechLossDb: Number((b.speechDb - o.speechDb).toFixed(1)),
          seconds: Number(encodeSeconds.toFixed(2)),
        },
      });
    }
    return out;
  });
} finally {
  await browser.close();
  await server.close();
}
const origins = [...new Set(requests.map((u) => new URL(u).origin))];
const assets = server.log.filter((e) => e.path.startsWith('/dist/assets/')).map((e) => e.path);
const record = { browser: name, version: browser.version(), results, origins, assets: [...new Set(assets)], logs: logs.slice(0, 20) };
writeJson(`${ROOT}out/s4-${name}.json`, record);
console.log(JSON.stringify(record, null, 2));
