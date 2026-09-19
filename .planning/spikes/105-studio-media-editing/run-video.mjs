// Drives the Video lab in real browsers and writes every trimmed clip to out/<browser>/ so
// ffprobe (probe.sh) can check it independently of Mediabunny. Usage: node run-video.mjs [channel...]
import { existsSync, mkdirSync, readFileSync, writeFileSync } from 'node:fs';
import { chromium, firefox } from 'file:///D:/Repo/Aura/web/node_modules/playwright/index.mjs';

const BASE = 'http://localhost:5199';
const channels = process.argv.slice(2).length ? process.argv.slice(2) : ['chrome', 'msedge', 'chromium'];

const CASES = [
  ['clip-h264-aac.mp4', { start: 2.5, end: 6, mode: 'exact' }],
  ['clip-h264-aac.mp4', { start: 2.5, end: 6, mode: 'expand' }],
  ['clip-h264-aac.mp4', { start: 2.5, end: 6, mode: 'shrink' }],
  ['clip-h264-aac.mp4', { start: 0, end: 4, mode: 'expand' }],
  ['clip-h264-aac.mp4', { start: 1, end: 5, mode: 'exact', rotate: 90, width: 640, mute: true }],
  ['clip-h264-silent.mp4', { start: 1.5, end: 5, mode: 'exact' }],
  ['clip-vp9-opus.webm', { start: 2.5, end: 6, mode: 'exact' }],
  ['clip-vp9-opus.webm', { start: 2.5, end: 6, mode: 'expand' }],
  ['clip-hevc-aac.mp4', { start: 2.5, end: 6, mode: 'exact' }],
  ['clip-hevc-aac.mp4', { start: 2.5, end: 6, mode: 'expand' }],
  ['clip-rot90.mp4', { start: 2.5, end: 6, mode: 'exact' }],
  ['clip-long-1080p.mp4', { start: 10, end: 40, mode: 'expand' }],
  ['clip-long-1080p.mp4', { start: 10, end: 40, mode: 'exact' }],
];

const REPORT = 'out/video-report.json';
const report = existsSync(REPORT) ? JSON.parse(readFileSync(REPORT, 'utf8')) : {};
for (const channel of channels) {
  const dir = `out/${channel}`;
  mkdirSync(dir, { recursive: true });
  const browser = channel === 'firefox' ? await firefox.launch() : await chromium.launch(channel === 'chromium' ? {} : { channel });
  const page = await browser.newPage();
  page.setDefaultTimeout(300_000);
  await page.goto(`${BASE}/?tab=video`);
  await page.waitForFunction(() => window.__spike?.trimSample);
  const rows = [];
  const caps = await page.evaluate(() => window.__spike.capabilities());
  for (const [name, opts] of CASES) {
    const label = `${name.replace(/\.\w+$/, '')}-${opts.mode}-${opts.start}-${opts.end}${opts.rotate ? '-rot' : ''}`;
    try {
      const { result, base64 } = await page.evaluate(([n, o]) => window.__spike.trimSample(n, o), [name, opts]);
      const ext = name.endsWith('.webm') ? 'webm' : 'mp4';
      writeFileSync(`${dir}/${label}.${ext}`, Buffer.from(base64, 'base64'));
      rows.push({ label, ok: true, ms: result.ms, outBytes: result.outBytes, discarded: result.discarded, out: result.out });
      console.log(`${channel} ✓ ${label} ${result.ms} ms dur=${result.out.duration.toFixed(3)} ${result.out.tracks.map((t) => `${t.type}:${t.codec}@${t.first.toFixed(3)}`).join(' ')}`);
    } catch (e) {
      rows.push({ label, ok: false, error: String(e.message).slice(0, 300) });
      console.log(`${channel} ✗ ${label} ${String(e.message).slice(0, 200)}`);
    }
  }
  report[channel] = { userAgent: await page.evaluate(() => navigator.userAgent), caps, rows };
  await browser.close();
  writeFileSync(REPORT, JSON.stringify(report, null, 2));
}
