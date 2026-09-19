// Q1: renders the VideoFlow composition (clip + text + keyframed photo) and the split scenario in
// Chrome, writes the MP4s to out/compose/, and logs every request the page makes. Any request whose
// origin is not the dev server's is a failure. Usage: node run-compose.mjs [runs]
import { mkdirSync, writeFileSync } from 'node:fs';
import { chromium } from 'file:///D:/Repo/Aura/web/node_modules/playwright/index.mjs';

const BASE = process.env.SPIKE_BASE ?? 'http://localhost:5207';
const RUNS = Number(process.argv[2] ?? 3);
const dir = process.env.SPIKE_OUT ?? 'out/compose';
mkdirSync(dir, { recursive: true });

const browser = await chromium.launch({ channel: 'chrome' });
const page = await browser.newPage({ viewport: { width: 1200, height: 900 } });
page.setDefaultTimeout(180_000);
const requests = [];
const consoleIssues = [];
page.on('request', (r) => requests.push({ url: r.url(), type: r.resourceType() }));
page.on('console', (m) => ['error', 'warning'].includes(m.type()) && consoleIssues.push(`${m.type()}: ${m.text().slice(0, 240)}`));
page.on('pageerror', (e) => consoleIssues.push(`pageerror: ${e.message.slice(0, 240)}`));
page.on('response', (r) => r.status() >= 400 && consoleIssues.push(`http ${r.status()}: ${r.url()}`));

await page.goto(`${BASE}/?tab=compose`);
await page.waitForFunction(() => window.__spike?.previewReady === true);
await page.screenshot({ path: `${dir}/preview-frame48.png` });

const renders = [];
for (const [name, worker] of [['base', true], ['base', false], ['split', true], ['bias', true]]) {
  for (let i = 0; i < (name === 'base' ? RUNS : 1); i++) {
    const r = await page.evaluate(([n, w]) => window.__spike.render(n, { worker: w }), [name, worker]);
    const file = `${dir}/${name}-${worker ? 'worker' : 'main'}-${i}.mp4`;
    writeFileSync(file, Buffer.from(r.base64, 'base64'));
    delete r.base64;
    renders.push({ name, worker, run: i, file, ...r });
    console.log(`${name} worker=${worker} run ${i}: ${r.ms} ms, ${r.bytes} B, duration ${r.duration}`);
  }
}

const isOff = (r) => !r.url.startsWith(BASE) && !r.url.startsWith('data:') && !r.url.startsWith('blob:');
const offOrigin = requests.filter(isOff);

// Negative control, in a fresh page: the same render with VideoFlow's stock loadFont must reach out.
const control = await browser.newPage();
const controlRequests = [];
control.on('request', (r) => controlRequests.push({ url: r.url(), type: r.resourceType() }));
await control.goto(`${BASE}/?tab=compose`);
await control.waitForFunction(() => window.__spike?.render);
const controlRender = await control.evaluate(() => window.__spike.render('base', { stockFonts: true }).then((r) => ({ ms: r.ms, bytes: r.bytes })));
const controlOffOrigin = controlRequests.filter(isOff);
console.log('control (stock fonts):', controlRender, 'off-origin:', [...new Set(controlOffOrigin.map((r) => r.url.split('?')[0]))]);
const report = { userAgent: await page.evaluate(() => navigator.userAgent), renders, requests, offOrigin, control: { render: controlRender, offOrigin: controlOffOrigin }, consoleIssues, log: await page.evaluate(() => window.__spikeLog.events) };
writeFileSync(`${dir}/report.json`, JSON.stringify(report, null, 2));
console.log(`requests: ${requests.length}, off-origin: ${offOrigin.length}`, offOrigin.map((r) => r.url));
console.log('console issues:', consoleIssues);
await browser.close();
