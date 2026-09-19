// Q1 frame check, independent of VideoFlow: ffmpeg (docker) grabs frames from the exported MP4s and
// from the source clip at the matching source time, and the difference shows where layers were drawn.
//  - base  @1.02 s and @3.52 s: two changed regions must appear, one at the text anchor, one at the
//    photo's keyframed position (which moves between the two times); the text region holds the white
//    glyphs on the black box.
//  - timing: every output frame is matched to the source frame it shows (burned-in clock, which no
//    layer covers). base and bias: n shows n. split: n < 48 shows n, n ≥ 48 shows n + 12.
// Usage: node run-frames.mjs   (after run-compose.mjs)
import { execFileSync } from 'node:child_process';
import { mkdirSync, writeFileSync } from 'node:fs';
import { resolve } from 'node:path';

const W = 320;
const H = 180;
const OUT = 'out/compose';
const winPath = (p) => resolve(p).replaceAll('\\', '/');
mkdirSync(`${OUT}/frames`, { recursive: true });

function ffmpeg(args) {
  return execFileSync('docker', ['run', '--rm', '-v', `${winPath('.')}:/m`, 'jrottenberg/ffmpeg:7.1-alpine', '-v', 'error', ...args], {
    env: { ...process.env, MSYS_NO_PATHCONV: '1' },
    maxBuffer: 64 << 20,
  });
}

function grab(file, t, png) {
  ffmpeg(['-y', '-ss', String(t), '-i', `/m/${file}`, '-frames:v', '1', `/m/${png}`]);
  return ffmpeg(['-ss', String(t), '-i', `/m/${file}`, '-frames:v', '1', '-f', 'rawvideo', '-pix_fmt', 'rgb24', '-']);
}

const px = (buf, x, y) => [buf[(y * W + x) * 3], buf[(y * W + x) * 3 + 1], buf[(y * W + x) * 3 + 2]];

// Connected regions (4-neighbour) of pixels whose largest channel difference exceeds 60.
function changedRegions(a, b) {
  const mask = new Uint8Array(W * H);
  for (let i = 0; i < W * H; i++) {
    const d = Math.max(Math.abs(a[i * 3] - b[i * 3]), Math.abs(a[i * 3 + 1] - b[i * 3 + 1]), Math.abs(a[i * 3 + 2] - b[i * 3 + 2]));
    mask[i] = d > 60 ? 1 : 0;
  }
  const seen = new Uint8Array(W * H);
  const regions = [];
  for (let s = 0; s < W * H; s++) {
    if (!mask[s] || seen[s]) continue;
    const stack = [s];
    seen[s] = 1;
    let area = 0, x0 = W, y0 = H, x1 = 0, y1 = 0;
    while (stack.length) {
      const i = stack.pop();
      const x = i % W, y = (i / W) | 0;
      area++;
      x0 = Math.min(x0, x); x1 = Math.max(x1, x); y0 = Math.min(y0, y); y1 = Math.max(y1, y);
      for (const j of [i - 1, i + 1, i - W, i + W]) {
        if (j < 0 || j >= W * H || seen[j] || !mask[j] || Math.abs((j % W) - x) > 1) continue;
        seen[j] = 1;
        stack.push(j);
      }
    }
    if (area >= 40) regions.push({ area, bbox: [x0, y0, x1, y1] });
  }
  return regions.sort((p, q) => q.area - p.area);
}

const contains = (r, [x, y]) => x >= r.bbox[0] && x <= r.bbox[2] && y >= r.bbox[1] && y <= r.bbox[3];

function meanDiff(a, b, [x0, y0, x1, y1]) {
  let sum = 0, n = 0;
  for (let y = y0; y < y1; y++) for (let x = x0; x < x1; x++) {
    const p = px(a, x, y), q = px(b, x, y);
    sum += Math.abs(p[0] - q[0]) + Math.abs(p[1] - q[1]) + Math.abs(p[2] - q[2]);
    n += 3;
  }
  return sum / n;
}

const report = { base: [], timing: [] };
const TEXT_ANCHOR = [Math.round(0.3 * W), Math.round(0.82 * H)];
for (const t of [1.02, 3.52]) {
  const out = grab(`${OUT}/base-worker-0.mp4`, t, `${OUT}/frames/base-${t}.png`);
  const src = grab('media/clip.mp4', t, `${OUT}/frames/src-${t}.png`);
  const regions = changedRegions(out, src);
  // photo keyframes: position [0.2,0.3] → [0.8,0.4] linear over 0–3 s, then held.
  const k = Math.min(t / 3, 1);
  const photoCenter = [Math.round((0.2 + 0.6 * k) * W), Math.round((0.3 + 0.1 * k) * H)];
  const textRegion = regions.find((r) => contains(r, TEXT_ANCHOR));
  const photoRegion = regions.find((r) => r !== textRegion && contains(r, photoCenter));
  let white = 0;
  if (textRegion) {
    const [x0, y0, x1, y1] = textRegion.bbox;
    for (let y = y0; y <= y1; y++) for (let x = x0; x <= x1; x++) if (px(out, x, y).every((c) => c > 225)) white++;
  }
  const row = { t, photoCenter, textRegion, photoRegion, whiteGlyphPixels: white, regions: regions.slice(0, 5) };
  row.pass = Boolean(textRegion && photoRegion && white > 30);
  report.base.push(row);
  console.log(`base @${t}: text ${JSON.stringify(textRegion?.bbox)} white=${white}, photo@${photoCenter} ${JSON.stringify(photoRegion?.bbox)} → ${row.pass ? 'PASS' : 'FAIL'}`);
}

// Which source frame does every output frame show? All frames of both files are decoded once and each
// output frame is matched to the source frame with the smallest clock-region difference (argmin is
// robust to the constant colour shift a re-encode adds). Base: frame n must show n. Split: n below 48
// shows n, from 48 on n + 12 (0.5 s × 24 fps trimmed off the right half's head).
const CLOCK = [4, 4, 100, 34];
const FRAME = W * H * 3;
const frames = (file) => { const all = ffmpeg(['-i', `/m/${file}`, '-f', 'rawvideo', '-pix_fmt', 'rgb24', '-']); return Array.from({ length: all.length / FRAME }, (_, i) => all.subarray(i * FRAME, (i + 1) * FRAME)); };
const sourceFrames = frames('media/clip.mp4');
for (const [name, expect] of [['base-worker-0', (n) => n], ['base-main-0', (n) => n], ['split-worker-0', (n) => (n < 48 ? n : n + 12)], ['bias-worker-0', (n) => n]]) {
  const outFrames = frames(`${OUT}/${name}.mp4`);
  const mismatches = [];
  outFrames.forEach((out, n) => {
    const diffs = sourceFrames.map((f) => meanDiff(out, f, CLOCK));
    const best = diffs.indexOf(Math.min(...diffs));
    if (best !== expect(n)) mismatches.push({ n, shows: best, expected: expect(n) });
  });
  const row = { file: name, frames: outFrames.length, mismatches, pass: mismatches.length === 0 };
  report.timing.push(row);
  console.log(`${name}: ${outFrames.length} frames, ${mismatches.length} mismatches ${JSON.stringify(mismatches.slice(0, 6))} → ${row.pass ? 'PASS' : 'FAIL'}`);
}

writeFileSync(`${OUT}/frames-report.json`, JSON.stringify(report, null, 2));
