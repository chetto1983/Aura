// Builds the speech fixtures from three TTS phrases: silence, phrase, gap, phrase, gap, phrase,
// silence. The windows are known to the sample, so they are the ground truth a VAD is scored on.
import { decodeMono, encodeWav, fromBase64, toBase64 } from './wav';

const RATE = 16000;
const LEAD = 1.0;
const GAP = 1.5;
const TAIL = 1.0;
const SNR_DB = 10;
const TRIM_DB = -50;

function trimSilence(samples: Float32Array): Float32Array {
  const floor = 10 ** (TRIM_DB / 20);
  let start = 0;
  while (start < samples.length && Math.abs(samples[start] ?? 0) < floor) start += 1;
  let end = samples.length;
  while (end > start && Math.abs(samples[end - 1] ?? 0) < floor) end -= 1;
  return samples.slice(start, end);
}

function mulberry32(seed: number): () => number {
  let state = seed;
  return () => {
    state = (state + 0x6d2b79f5) | 0;
    let t = Math.imul(state ^ (state >>> 15), 1 | state);
    t = (t + Math.imul(t ^ (t >>> 7), 61 | t)) ^ t;
    return ((t ^ (t >>> 14)) >>> 0) / 4294967296;
  };
}

/** Paul Kellet's economy pink filter over seeded white noise: reproducible, speech-like spectrum. */
function pinkNoise(length: number, seed: number): Float32Array {
  const random = mulberry32(seed);
  const out = new Float32Array(length);
  let b0 = 0;
  let b1 = 0;
  let b2 = 0;
  for (let i = 0; i < length; i += 1) {
    const white = random() * 2 - 1;
    b0 = 0.99765 * b0 + white * 0.099046;
    b1 = 0.963 * b1 + white * 0.2965164;
    b2 = 0.57 * b2 + white * 1.0526913;
    out[i] = b0 + b1 + b2 + white * 0.1848;
  }
  return out;
}

function rms(samples: Float32Array, from: number, to: number): number {
  let sum = 0;
  for (let i = from; i < to; i += 1) sum += (samples[i] ?? 0) ** 2;
  return Math.sqrt(sum / Math.max(1, to - from));
}

async function build(phrases: readonly string[]) {
  const voiced = await Promise.all(
    phrases.map(async (base64) => trimSilence(await decodeMono(fromBase64(base64), RATE))),
  );
  const silence = Math.round((LEAD + TAIL + GAP * (voiced.length - 1)) * RATE);
  const speech = new Float32Array(silence + voiced.reduce((n, v) => n + v.length, 0));
  const windows: [number, number][] = [];
  let at = Math.round(LEAD * RATE);
  voiced.forEach((phrase, index) => {
    speech.set(phrase, at);
    windows.push([at, at + phrase.length]);
    at += phrase.length + (index < voiced.length - 1 ? Math.round(GAP * RATE) : 0);
  });
  const peak = speech.reduce((max, s) => Math.max(max, Math.abs(s)), 0);
  const gain = 10 ** (-3 / 20) / peak;
  for (let i = 0; i < speech.length; i += 1) speech[i] = (speech[i] ?? 0) * gain;

  let sum = 0;
  let count = 0;
  for (const [start, end] of windows) {
    for (let i = start; i < end; i += 1) sum += (speech[i] ?? 0) ** 2;
    count += end - start;
  }
  const speechRms = Math.sqrt(sum / count);
  const noise = pinkNoise(speech.length, 20260927);
  const scale = speechRms / 10 ** (SNR_DB / 20) / rms(noise, 0, noise.length);
  const noisy = speech.map((s, i) => s + (noise[i] ?? 0) * scale);
  const noisyPeak = noisy.reduce((max, s) => Math.max(max, Math.abs(s)), 0);
  if (noisyPeak > 0.99) {
    for (let i = 0; i < noisy.length; i += 1) noisy[i] = ((noisy[i] ?? 0) * 0.99) / noisyPeak;
  }
  return {
    speech: toBase64(encodeWav(speech, RATE)),
    noisy: toBase64(encodeWav(noisy, RATE)),
    truth: {
      sampleRate: RATE,
      snrDb: SNR_DB,
      speechRmsDb: Number((20 * Math.log10(speechRms)).toFixed(2)),
      windows: windows.map(([s, e]) => [Number((s / RATE).toFixed(3)), Number((e / RATE).toFixed(3))]),
    },
  };
}

Object.assign(window, { buildFixtures: build });
