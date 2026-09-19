// Q4: pitch-preserving speed. For every method at 0.5× and 2×: output duration (8 s / 2 s ±2 %),
// FFT peak (440 Hz ±5 Hz), purity and processing time, run 3 times (first run includes WASM/worklet
// start-up). Then the <video> element's preservesPitch as a live preview, and the retimed MP4s
// (signalsmith audio + copied video packets) which ffprobe and an independent ffmpeg decode + FFT
// check. Writes out/speed/. Usage: node run-speed.mjs
import { execFileSync } from 'node:child_process';
import { mkdirSync, writeFileSync } from 'node:fs';
import { resolve } from 'node:path';
import { chromium } from 'file:///D:/Repo/Aura/web/node_modules/playwright/index.mjs';
import { spectrumPeak } from './app/src/speed/fft.js';

const BASE = 'http://localhost:5207';
const OUT = 'out/speed';
mkdirSync(OUT, { recursive: true });
const browser = await chromium.launch({ channel: 'chrome', args: ['--autoplay-policy=no-user-gesture-required'] });
const page = await browser.newPage();
page.setDefaultTimeout(120_000);
const requests = [];
const issues = [];
page.on('request', (r) => requests.push(r.url()));
page.on('console', (m) => ['error', 'warning'].includes(m.type()) && issues.push(`${m.type()}: ${m.text().slice(0, 200)}`));
page.on('pageerror', (e) => issues.push(`pageerror: ${e.message.slice(0, 200)}`));
await page.goto(`${BASE}/?tab=speed`);
await page.waitForFunction(() => window.__spike?.stretch);

const offline = [];
for (const method of ['naive', 'signalsmith', 'soundtouch', 'videoflow']) {
  for (const speed of [0.5, 2]) {
    for (let run = 0; run < 3; run++) {
      try {
        const r = await page.evaluate(([m, s]) => window.__spike.stretch(m, s), [method, speed]);
        offline.push({ run, ...r });
        if (run === 2) console.log(`${method} ${speed}×: ${r.outDuration}s (want ${r.expected}) ${r.durationOk ? '✓' : '✗'}  peak ${r.peakHz} Hz ${r.pitchOk ? '✓' : '✗'}  purity ${r.purity}  ${r.ms} ms`);
      } catch (e) {
        offline.push({ method, speed, run, error: e.message.slice(0, 300) });
        console.log(`${method} ${speed}× run ${run}: ERROR ${e.message.slice(0, 200)}`);
        break;
      }
    }
  }
}

const preview = [];
for (const [rate, keep] of [[2, true], [0.5, true], [2, false]]) {
  const r = await page.evaluate(([a, b]) => window.__spike.previewPitch(a, b), [rate, keep]);
  preview.push(r);
  console.log(`preview <video> ${rate}× preservesPitch=${keep}: peak ${r.peakHz} Hz ${r.pitchOk ? '✓' : '✗'}`);
}

const winPath = (p) => resolve(p).replaceAll('\\', '/');
const docker = (args, entrypoint) => execFileSync('docker', ['run', '--rm', '-v', `${winPath('.')}:/m`, ...(entrypoint ? ['--entrypoint', entrypoint] : []), 'jrottenberg/ffmpeg:7.1-alpine', '-v', 'error', ...args], { env: { ...process.env, MSYS_NO_PATHCONV: '1' }, maxBuffer: 64 << 20 });
const muxed = [];
for (const speed of [0.5, 2]) {
  const r = await page.evaluate((s) => window.__spike.exportRetimed('signalsmith', s), speed);
  const file = `${OUT}/retimed-signalsmith-${speed}x.mp4`;
  writeFileSync(file, Buffer.from(r.base64, 'base64'));
  const probe = docker(['-show_entries', 'format=duration:stream=codec_name,duration,nb_frames,r_frame_rate,avg_frame_rate,sample_rate', '-of', 'json', `/m/${file}`], 'ffprobe').toString();
  const pcm = docker(['-i', `/m/${file}`, '-vn', '-ac', '1', '-f', 'f32le', '-acodec', 'pcm_f32le', '-']);
  const samples = new Float32Array(pcm.buffer, pcm.byteOffset, pcm.byteLength / 4);
  const info = JSON.parse(probe);
  const audio = info.streams.find((s) => s.codec_name === 'aac');
  const fft = spectrumPeak(samples, Number(audio.sample_rate));
  const row = { speed, muxMs: r.ms, bytes: r.bytes, ffprobe: info, decodedSeconds: +(samples.length / Number(audio.sample_rate)).toFixed(3), ...fft };
  row.ok = Math.abs(Number(info.format.duration) / (4 / speed) - 1) <= 0.02 && Math.abs(fft.peakHz - 440) <= 5;
  muxed.push(row);
  console.log(`mux ${speed}×: ${r.ms} ms, container ${info.format.duration}s, ${info.streams.map((s) => `${s.codec_name} ${s.duration}s${s.nb_frames ? ` ${s.nb_frames}f @${s.avg_frame_rate}` : ''}`).join(', ')}; ffmpeg-decoded peak ${fft.peakHz} Hz → ${row.ok ? 'PASS' : 'FAIL'}`);
}

const offOrigin = requests.filter((u) => !u.startsWith(BASE) && !u.startsWith('data:') && !u.startsWith('blob:'));
writeFileSync(`${OUT}/report.json`, JSON.stringify({ offline, preview, muxed, offOrigin, issues: [...new Set(issues)] }, null, 2));
console.log('off-origin:', offOrigin, 'issues:', [...new Set(issues)]);
await browser.close();
