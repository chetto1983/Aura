// Spike 108, independent of VideoFlow: ffmpeg reads the exported MP4s back.
//
//  1. ffprobe — container duration and streams of every render.
//  2. Audio — per-second RMS and a Goertzel reading of 440 Hz (clip-a) against 880 Hz (clip-b), so
//     each second of the lane says which source it came from, and whether the third clip is silent.
//  3. Frames — every output frame matched to the source frame it shows (argmin of the full-frame
//     difference over the source's own frames), for the SAME composition at 24 and at 30 fps, over
//     a 24 fps and a 30 fps source, with and without the 1e-4 s `sourceStart` nudge, so that frame
//     rate, sampling ratio and composition each move one at a time.
//
// Usage: node probe-audio.mjs        (after run-audio.mjs; reads and writes out/)
import { execFileSync } from 'node:child_process';
import { writeFileSync } from 'node:fs';
import { resolve } from 'node:path';

const W = 320;
const H = 180;
const FRAME = W * H * 3;
const OUT = process.env.SPIKE_OUT ?? 'out';
const winPath = (p) => resolve(p).replaceAll('\\', '/');

function ffmpeg(args, { entrypoint } = {}) {
  return execFileSync(
    'docker',
    ['run', '--rm', '-v', `${winPath(OUT)}:/m`, ...(entrypoint ? ['--entrypoint', entrypoint] : []), 'jrottenberg/ffmpeg:7.1-alpine', ...args],
    { env: { ...process.env, MSYS_NO_PATHCONV: '1' }, maxBuffer: 512 << 20 },
  );
}

const probe = (file) =>
  JSON.parse(
    ffmpeg(['-v', 'error', '-print_format', 'json', '-show_format', '-show_streams', `/m/${file}`], { entrypoint: 'ffprobe' }).toString(),
  );

// Mono float32 at 48 kHz, so a window can be read straight out of the buffer.
function pcm(file) {
  const raw = ffmpeg(['-v', 'error', '-i', `/m/${file}`, '-vn', '-ac', '1', '-ar', '48000', '-f', 'f32le', '-']);
  return new Float32Array(raw.buffer, raw.byteOffset, raw.byteLength / 4);
}

// Goertzel: the energy at one frequency, without an FFT over the whole window.
function toneEnergy(samples, from, to, freq, rate = 48000) {
  const n = to - from;
  const k = Math.round((n * freq) / rate);
  const w = (2 * Math.PI * k) / n;
  const coeff = 2 * Math.cos(w);
  let s1 = 0;
  let s2 = 0;
  for (let i = from; i < to; i++) {
    const s0 = samples[i] + coeff * s1 - s2;
    s2 = s1;
    s1 = s0;
  }
  return Math.sqrt(s1 * s1 + s2 * s2 - coeff * s1 * s2) / n;
}

function rms(samples, from, to) {
  let sum = 0;
  for (let i = from; i < to; i++) sum += samples[i] * samples[i];
  return Math.sqrt(sum / (to - from));
}

function audioSeconds(file) {
  const s = pcm(file);
  const rows = [];
  for (let sec = 0; sec * 48000 < s.length; sec++) {
    const from = sec * 48000;
    const to = Math.min(s.length, from + 48000);
    if (to - from < 4800) break;
    rows.push({
      sec,
      rms: Number(rms(s, from, to).toFixed(5)),
      e440: Number(toneEnergy(s, from, to, 440).toFixed(5)),
      e880: Number(toneEnergy(s, from, to, 880).toFixed(5)),
    });
  }
  return rows;
}

const frames = (file) => {
  const all = ffmpeg(['-v', 'error', '-i', `/m/${file}`, '-f', 'rawvideo', '-pix_fmt', 'rgb24', '-']);
  return Array.from({ length: all.length / FRAME }, (_, i) => all.subarray(i * FRAME, (i + 1) * FRAME));
};

function meanDiff(a, b) {
  let sum = 0;
  for (let i = 0; i < FRAME; i++) sum += Math.abs(a[i] - b[i]);
  return sum / FRAME;
}

const report = { probedAt: new Date().toISOString(), ffprobe: {}, audio: {}, frames: {} };
const RENDERS = ['audio-worker-0.mp4', 'audio-main-0.mp4', 'audio-nudge-worker-0.mp4', 'audio-mute-prop-worker-0.mp4'];

console.log('== ffprobe ==');
for (const file of [...RENDERS, 'clip-a.mp4', 'clip-b.mp4']) {
  const p = probe(file);
  const streams = p.streams.map((s) => `${s.codec_type}:${s.codec_name}${s.codec_type === 'video' ? ` ${s.width}x${s.height}@${s.r_frame_rate} ${s.nb_frames}f` : ` ${s.channels}ch@${s.sample_rate}`}`);
  report.ffprobe[file] = { duration: Number(p.format.duration), size: Number(p.format.size), streams };
  console.log(`${file}: ${p.format.duration} s, ${p.format.size} B — ${streams.join(' | ')}`);
}

console.log('\n== audio, per second: rms / 440 Hz / 880 Hz ==');
for (const file of RENDERS) {
  const rows = audioSeconds(file);
  report.audio[file] = rows;
  console.log(
    `${file}\n  ` +
      rows.map((r) => `s${r.sec}: rms ${r.rms} 440=${r.e440} 880=${r.e880}`).join('\n  '),
  );
}

console.log('\n== frames ==');
// Every frame match is made against ONE clip's frames, so assert the a/b pairs are the same video.
const sourceFrames = {};
for (const [a, b] of [['clip-a.mp4', 'clip-b.mp4'], ['clip-a24.mp4', 'clip-b24.mp4']]) {
  const fa = frames(a);
  const fb = frames(b);
  const worst = Math.max(...fa.map((f, i) => meanDiff(f, fb[i])));
  sourceFrames[a] = fa;
  report.frames[`${a} vs ${b}`] = { maxMeanDiff: Number(worst.toFixed(4)), frames: fa.length };
  console.log(`${a} vs ${b}: ${fa.length} frames, worst mean per-channel difference ${worst.toFixed(4)}`);
}

// The controlled comparison. The lane is always the same three layers over 0-4 / 4-6 / 6-8 s; what
// varies is the project frame rate and the source frame rate, one at a time. Output frame n shows
// timeline second n/projectFps, so the source frame whose presentation interval contains it is
// kRaw = floor(n * srcFps / projectFps) for layer one, and kRaw - 4*srcFps for the other two (layer
// two is sourceStart 0 at timeline 4, layer three sourceStart 2 at timeline 6 — the same source
// time). Both ratios used here, 1 and 1.25, are exact in binary, so the floor is not a rounding
// artefact.
const CASES = [
  { file: 'audio-worker-0.mp4', label: '30 fps project / 30 fps source, no nudge', projectFps: 30, srcFps: 30, src: 'clip-a.mp4' },
  { file: 'audio-nudge-worker-0.mp4', label: '30 fps project / 30 fps source, nudge', projectFps: 30, srcFps: 30, src: 'clip-a.mp4' },
  { file: 'audio24-worker-0.mp4', label: '24 fps project / 30 fps source, no nudge', projectFps: 24, srcFps: 30, src: 'clip-a.mp4' },
  { file: 'audio24-nudge-worker-0.mp4', label: '24 fps project / 30 fps source, nudge', projectFps: 24, srcFps: 30, src: 'clip-a.mp4' },
  { file: 'audio24src-worker-0.mp4', label: '24 fps project / 24 fps source, no nudge', projectFps: 24, srcFps: 24, src: 'clip-a24.mp4' },
  { file: 'audio24src-nudge-worker-0.mp4', label: '24 fps project / 24 fps source, nudge', projectFps: 24, srcFps: 24, src: 'clip-a24.mp4' },
];

for (const c of CASES) {
  const src = sourceFrames[c.src];
  const out = frames(c.file);
  const expected = (n) => {
    const kRaw = Math.floor((n * c.srcFps) / c.projectFps);
    return n / c.projectFps < 4 ? kRaw : kRaw - 4 * c.srcFps;
  };
  const mismatches = [];
  const margins = [];
  out.forEach((f, n) => {
    const diffs = src.map((g) => meanDiff(f, g));
    const best = diffs.indexOf(Math.min(...diffs));
    const sorted = [...diffs].sort((a, b) => a - b);
    margins.push(sorted[1] - sorted[0]);
    if (best !== expected(n)) mismatches.push({ n, shows: best, expected: expected(n), off: best - expected(n) });
  });
  // A frame can only hit the boundary defect when its timeline second lands exactly on a source
  // frame boundary, i.e. n * srcFps is divisible by projectFps. That denominator is what makes two
  // frame rates comparable at all, so it is reported beside the count.
  const atRisk = out.filter((_, n) => (n * c.srcFps) % c.projectFps === 0).length;
  const perLayer = [
    ['one (0-4 s, sourceStart 0)', 0, 4 * c.projectFps],
    ['two (4-6 s, sourceStart 0)', 4 * c.projectFps, 6 * c.projectFps],
    ['three (6-8 s, sourceStart 2)', 6 * c.projectFps, 8 * c.projectFps],
  ].map(([name, from, to]) => ({ layer: name, frames: to - from, mismatches: mismatches.filter((m) => m.n >= from && m.n < to).length }));
  const row = {
    ...c,
    frames: out.length,
    atRiskFrames: atRisk,
    mismatchCount: mismatches.length,
    offsets: [...new Set(mismatches.map((m) => m.off))],
    perLayer,
    mismatches,
    pass: mismatches.length === 0,
    worstMargin: Number(Math.min(...margins).toFixed(4)),
  };
  report.frames[c.file] = row;
  console.log(
    `${c.label.padEnd(42)} ${String(mismatches.length).padStart(3)} / ${out.length} mismatches, ` +
      `${atRisk} frames on a source boundary, offsets ${JSON.stringify(row.offsets)}, ` +
      `per layer ${perLayer.map((l) => l.mismatches + '/' + l.frames).join(' ')} -> ${row.pass ? 'PASS' : 'FAIL'}`,
  );
}

// Spike 108's candidate fix: one decoded buffer per source instead of one per layer. Equal file
// sizes are not equal audio, so the two mixes are compared sample by sample.
console.log('\n== per-source cache vs per-layer decode ==');
report.cache = {};
for (const [plain, cached] of [
  ['audio-mute-prop-worker-0.mp4', 'audio-mute-prop-cached-worker-0.mp4'],
  ['split-16-worker-0.mp4', 'split-16-cached-worker-0.mp4'],
  ['long-8-worker-0.mp4', 'long-8-cached-worker-0.mp4'],
]) {
  const a = pcm(plain);
  const b = pcm(cached);
  let worst = 0;
  const n = Math.min(a.length, b.length);
  for (let i = 0; i < n; i++) worst = Math.max(worst, Math.abs(a[i] - b[i]));
  const row = { samples: n, lengthDelta: a.length - b.length, worstSampleDelta: worst, identical: worst === 0 && a.length === b.length };
  report.cache[cached] = row;
  console.log(`${plain} vs ${cached}: ${n} samples, length delta ${row.lengthDelta}, worst sample delta ${worst} → ${row.identical ? 'IDENTICAL' : 'DIFFERS'}`);
}

writeFileSync(`${OUT}/probe-report.json`, JSON.stringify(report, null, 2));
