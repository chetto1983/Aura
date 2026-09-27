// Reads the committed fixtures back and prints the levels the README promises.
import { readFileSync } from 'node:fs';

const DIR = new URL('../../web/e2e/fixtures/video-studio/audio/', import.meta.url);
const pcm = (name) => {
  const b = readFileSync(new URL(name, DIR));
  const n = (b.length - 44) / 2;
  return Float32Array.from({ length: n }, (_, i) => b.readInt16LE(44 + i * 2) / 32768);
};
const db = (x, a, b) => {
  let s = 0;
  for (let i = a; i < b; i += 1) s += x[i] ** 2;
  return (10 * Math.log10(s / Math.max(1, b - a) + 1e-20)).toFixed(2);
};
const truth = JSON.parse(readFileSync(new URL('speech.truth.json', DIR), 'utf8'));
const rate = truth.sampleRate;
const music = pcm('music.wav');
console.log(`music RMS ${db(music, 0, music.length)} dBFS (want -18.24)`);
for (const name of ['speech.wav', 'speech-noisy.wav']) {
  const x = pcm(name);
  const windows = truth.windows.map(([s, e]) => db(x, Math.round(s * rate), Math.round(e * rate)));
  const gaps = [];
  let from = 0;
  for (const [s, e] of truth.windows) {
    gaps.push(db(x, Math.round((from + 0.15) * rate), Math.round((s - 0.15) * rate)));
    from = e;
  }
  console.log(`${name}: speech windows ${windows.join(', ')} dBFS; gaps ${gaps.join(', ')} dBFS`);
}
