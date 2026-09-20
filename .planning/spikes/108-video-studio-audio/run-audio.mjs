// Spike 108: what a multi-clip video lane costs VideoFlow to render, measured in Chrome.
//
// Drives the spike 107 harness (`../107-studio-timeline-editor/app`, dev server on :5207) through
// the scenarios added to its ComposeLab. Per render it records wall time, the exported blob's size,
// the JS heap before/during/after, and — the question Task 5 branches on — how many times the audio
// mixer calls `decodeAudioData`, against the number of distinct sources and of layers.
//
// The counter patches `BaseAudioContext.prototype`, not `AudioContext.prototype` as first planned:
// the mixer decodes on an `OfflineAudioContext`, and in Chrome neither subclass owns the method —
// both inherit it from BaseAudioContext, so a patch on AudioContext.prototype counts nothing.
//
// Usage: node run-audio.mjs [runs]      (107's dev server must be up, with clip-a/clip-b in media/)
import { execFileSync } from 'node:child_process';
import { mkdirSync, writeFileSync } from 'node:fs';
import { chromium } from 'file:///D:/Repo/Aura/web/node_modules/playwright/index.mjs';

const BASE = process.env.SPIKE_BASE ?? 'http://localhost:5207';
const RUNS = Number(process.argv[2] ?? 3);
const dir = process.env.SPIKE_OUT ?? 'out';
mkdirSync(dir, { recursive: true });

// --expose-gc: without a forced collection between renders the heap deltas are pure GC noise.
const browser = await chromium.launch({ channel: 'chrome', args: ['--js-flags=--expose-gc'] });
const page = await browser.newPage({ viewport: { width: 1200, height: 900 } });
page.setDefaultTimeout(300_000);

// Installed before any app code runs, so nothing decodes behind the counter's back.
await page.addInitScript(() => {
  const proto = BaseAudioContext.prototype;
  const original = proto.decodeAudioData;
  window.__decodes = [];
  proto.decodeAudioData = function (buf, ...rest) {
    const entry = { bytes: buf.byteLength, ctx: this.constructor.name, t0: performance.now() };
    window.__decodes.push(entry);
    return original.call(this, buf, ...rest).then((b) => {
      Object.assign(entry, {
        ms: Math.round(performance.now() - entry.t0),
        seconds: b.duration,
        channels: b.numberOfChannels,
        sampleRate: b.sampleRate,
        pcmBytes: b.length * b.numberOfChannels * 4,
        heapAfter: performance.memory.usedJSHeapSize,
      });
      return b;
    });
  };
});

const requests = [];
const consoleIssues = [];
page.on('request', (r) => requests.push({ url: r.url(), type: r.resourceType() }));
page.on('console', (m) => ['error', 'warning'].includes(m.type()) && consoleIssues.push(`${m.type()}: ${m.text().slice(0, 240)}`));
page.on('pageerror', (e) => consoleIssues.push(`pageerror: ${e.message.slice(0, 240)}`));
page.on('response', (r) => r.status() >= 400 && consoleIssues.push(`http ${r.status()}: ${r.url()}`));

await page.goto(`${BASE}/?tab=compose`);
await page.waitForFunction(() => window.__spike?.previewReady === true);

const heapMB = (b) => Math.round((b / 1048576) * 10) / 10;

// One render, with the decode log and a 50 ms heap sample cleared and collected around it.
async function measure(name, { worker = true, cache = false } = {}) {
  const r = await page.evaluate(async ([n, w, c]) => {
    window.__decodes.length = 0;
    window.gc();
    window.gc();
    const heap = [performance.memory.usedJSHeapSize];
    const tick = setInterval(() => heap.push(performance.memory.usedJSHeapSize), 50);
    try {
      const out = await window.__spike.render(n, { worker: w, cache: c });
      heap.push(performance.memory.usedJSHeapSize);
      const settled = performance.memory.usedJSHeapSize;
      window.gc();
      window.gc();
      return { ...out, heap, settled, heapCollected: performance.memory.usedJSHeapSize, decodes: window.__decodes.slice() };
    } finally {
      clearInterval(tick);
    }
  }, [name, worker, cache]);
  const sources = new Set(r.layers.filter((l) => l.type === 'video').map((l) => l.settings.source));
  const row = {
    scenario: name,
    worker,
    cache,
    cachedSources: r.cachedSources,
    ms: r.ms,
    bytes: r.bytes,
    duration: r.duration,
    layers: r.layers.length,
    videoLayers: r.layers.filter((l) => l.type === 'video').length,
    distinctSources: sources.size,
    decodeCalls: r.decodes.length,
    decodes: r.decodes,
    heapStartMB: heapMB(r.heap[0]),
    heapPeakMB: heapMB(Math.max(...r.heap)),
    heapEndMB: heapMB(r.heap.at(-1)),
    heapAfterGcMB: heapMB(r.heapCollected),
    heapGrowthMB: heapMB(Math.max(...r.heap) - r.heap[0]),
    heapAtLastDecodeMB: r.decodes.length ? heapMB(r.decodes.at(-1).heapAfter - r.heap[0]) : 0,
    pcmBytesDecoded: r.decodes.reduce((t, d) => t + (d.pcmBytes ?? 0), 0),
    heapSamples: r.heap.length,
  };
  console.log(
    `${name}${cache ? ' CACHED' : ''} worker=${worker}: ${r.ms} ms, ${r.bytes} B, ${r.layers.length} layers / ${sources.size} sources → ` +
      `${r.decodes.length} decodeAudioData (${Math.round(row.pcmBytesDecoded / 1024)} KB PCM), ` +
      `heap ${row.heapStartMB}→${row.heapPeakMB} MB (+${row.heapGrowthMB}, +${row.heapAtLastDecodeMB} at last decode, ${row.heapAfterGcMB} after gc)`,
  );
  return { row, base64: r.base64 };
}

// INCONCLUSIVE, kept so nobody repeats it. Neither WorkingSetSize nor PrivatePageCount resolves an
// AudioBuffer's backing store: both read LOWER while 176 MB of PCM is held alive, because Chrome's
// own page-load allocations are still settling by tens of MB and audio memory does not land in the
// renderer's private commit. The decisive control is the `usedJSHeapSize` column below, which stays
// flat to 0.1 MB across all eight buffers; the PCM size itself is read off the real AudioBuffers.
// Headless is the discriminator — the operator's own Chrome is not — because `browser.process()`
// is not exposed on this Browser, so the tree cannot be walked from its root pid.
function browserPrivateMB() {
  const ps = "(Get-CimInstance Win32_Process -Filter \"Name='chrome.exe'\" | Where-Object { $_.CommandLine -like '*--headless*' } | Measure-Object -Property PrivatePageCount -Sum).Sum";
  const out = execFileSync('powershell.exe', ['-NoProfile', '-NonInteractive', '-Command', ps], { encoding: 'utf8' });
  return Math.round((Number(out.trim()) / 1048576) * 10) / 10;
}

// Control for the heap numbers: hold N decoded buffers of the 60 s stereo clip alive and watch
// `usedJSHeapSize`. If the PCM lived on the JS heap this would climb by ~22 MB per buffer.
// A fresh page, so the baseline is not the last render's leftovers.
const control = await browser.newPage();
await control.goto(`${BASE}/?tab=compose`);
await control.evaluate(() => { window.gc(); window.gc(); });
const privBaselineMB = browserPrivateMB();
const heapControl = await control.evaluate(async () => {
  const bytes = await (await fetch('/clip-long.mp4')).arrayBuffer();
  const ctx = new OfflineAudioContext(2, 48000, 48000);
  const held = [];
  const steps = [];
  window.gc();
  window.gc();
  const base = performance.memory.usedJSHeapSize;
  window.__held = held;
  for (let i = 0; i < 8; i++) {
    held.push(await ctx.decodeAudioData(bytes.slice(0)));
    window.gc();
    const pcm = held.reduce((t, b) => t + b.length * b.numberOfChannels * 4, 0);
    steps.push({ buffers: held.length, pcmBytes: pcm, heapDelta: performance.memory.usedJSHeapSize - base });
  }
  return { base, steps, decodedSeconds: held[0].duration, channels: held[0].numberOfChannels, sampleRate: held[0].sampleRate };
});
heapControl.privBaselineMB = privBaselineMB;
heapControl.privHoldingMB = browserPrivateMB();
await control.evaluate(() => { window.__held.length = 0; window.gc(); window.gc(); });
heapControl.privReleasedMB = browserPrivateMB();
console.log('\nheap control — 60 s stereo buffers held alive:');
for (const st of heapControl.steps) {
  console.log(`  ${st.buffers} buffer(s) = ${(st.pcmBytes / 1048576).toFixed(1)} MB PCM → usedJSHeapSize +${(st.heapDelta / 1048576).toFixed(1)} MB`);
}
console.log(`  browser private commit: ${heapControl.privBaselineMB} MB baseline → ${heapControl.privHoldingMB} MB holding 8 → ${heapControl.privReleasedMB} MB released`);

const renders = [];
const plan = [
  ...Array.from({ length: RUNS }, () => ['audio', true]),
  ['audio', false],
  ['audio-nudge', true],
  ['audio-mute-prop', true],
  ...[1, 2, 4, 8, 16].map((n) => [`split-${n}`, true]),
  ...[1, 4, 8].map((n) => [`long-${n}`, true]),
  // Same scenarios again, with `decodedBuffer` primed one buffer per distinct source.
  ['audio-mute-prop', true, true],
  ['split-16', true, true],
  ['long-8', true, true],
];
for (const [name, worker, cache = false] of plan) {
  const { row, base64 } = await measure(name, { worker, cache });
  const seen = renders.filter((x) => x.scenario === name && x.worker === worker && x.cache === cache).length;
  row.file = `${name}${cache ? '-cached' : ''}-${worker ? 'worker' : 'main'}-${seen}.mp4`;
  writeFileSync(`${dir}/${row.file}`, Buffer.from(base64, 'base64'));
  renders.push(row);
}
const isOff = (r) => !r.url.startsWith(BASE) && !r.url.startsWith('data:') && !r.url.startsWith('blob:');
const offOrigin = requests.filter(isOff);
const report = {
  measuredAt: new Date().toISOString(),
  userAgent: await page.evaluate(() => navigator.userAgent),
  renders,
  heapControl,
  offOrigin,
  requests: requests.length,
  consoleIssues,
};
writeFileSync(`${dir}/audio-report.json`, JSON.stringify(report, null, 2));
console.log(`\nrequests: ${requests.length}, off-origin: ${offOrigin.length}`);
console.log('console issues:', consoleIssues.slice(0, 10));
await browser.close();
