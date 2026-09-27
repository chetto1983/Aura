import {
  GtcrnWorkletNode,
  loadGtcrn,
  loadRnnoise,
  loadSpeex,
  RnnoiseWorkletNode,
  SpeexWorkletNode,
} from '@sapphi-red/web-noise-suppressor';
import gtcrnWasm from '@sapphi-red/web-noise-suppressor/gtcrn.wasm?url';
import gtcrnWorklet from '@sapphi-red/web-noise-suppressor/gtcrnWorklet.js?url';
import rnnoiseWasm from '@sapphi-red/web-noise-suppressor/rnnoise.wasm?url';
import rnnoiseSimdWasm from '@sapphi-red/web-noise-suppressor/rnnoise_simd.wasm?url';
import rnnoiseWorklet from '@sapphi-red/web-noise-suppressor/rnnoiseWorklet.js?url';
import speexWasm from '@sapphi-red/web-noise-suppressor/speex.wasm?url';
import speexWorklet from '@sapphi-red/web-noise-suppressor/speexWorklet.js?url';
import { decodeMono, rmsDb } from './wav';

// RNNoise is a 48 kHz model; the context runs there and upsamples the 16 kHz fixture itself.
const RATE = 48000;
const MARGIN = 0.15;
const PARAMS = new URLSearchParams(location.search);
const MODE = PARAMS.get('mode') ?? 'prestart';
const WAIT_MS = Number(PARAMS.get('wait') ?? '500');
const ONLY = PARAMS.get('only');

// The nodes are typed for AudioContext; AudioWorkletNode itself takes any BaseAudioContext.
type Maker = (ctx: OfflineAudioContext) => Promise<AudioNode | null>;
const live = (ctx: OfflineAudioContext) => ctx as unknown as AudioContext;
const MAKERS: Record<string, Maker> = {
  none: () => Promise.resolve(null),
  rnnoise: async (ctx) => {
    const wasmBinary = await loadRnnoise({ url: rnnoiseWasm, simdUrl: rnnoiseSimdWasm });
    await ctx.audioWorklet.addModule(rnnoiseWorklet);
    return new RnnoiseWorkletNode(live(ctx), { wasmBinary, maxChannels: 1 });
  },
  speex: async (ctx) => {
    const wasmBinary = await loadSpeex({ url: speexWasm });
    await ctx.audioWorklet.addModule(speexWorklet);
    return new SpeexWorkletNode(live(ctx), { wasmBinary, maxChannels: 1 });
  },
  gtcrn: async (ctx) => {
    const wasmBinary = await loadGtcrn({ url: gtcrnWasm });
    await ctx.audioWorklet.addModule(gtcrnWorklet);
    return new GtcrnWorkletNode(live(ctx), { wasmBinary, maxChannels: 1 });
  },
};

async function fixture(name: string): Promise<Float32Array> {
  const bytes = new Uint8Array(await (await fetch(`/fixtures/${name}`)).arrayBuffer());
  return decodeMono(bytes, RATE);
}

// The suppressor's delay: the lag (0–100 ms) at which half a second of output best matches the input.
function latencyMs(input: Float32Array, out: Float32Array, fromSeconds: number): number {
  const start = Math.round(fromSeconds * RATE);
  const span = RATE / 2;
  let best = 0;
  let bestSum = -Infinity;
  for (let lag = 0; lag <= RATE / 10; lag += 1) {
    let sum = 0;
    for (let i = start; i < start + span; i += 1) sum += (input[i] ?? 0) * (out[i + lag] ?? 0);
    if (sum > bestSum) {
      bestSum = sum;
      best = lag;
    }
  }
  return Number(((best / RATE) * 1000).toFixed(2));
}

function score(out: Float32Array, windows: readonly [number, number][]) {
  const at = (seconds: number) => Math.round(seconds * RATE);
  const speech = windows.map(([s, e]) => rmsDb(out, at(s + 0.05), at(e - 0.05)));
  const gaps: number[] = [];
  let from = 0;
  for (const [s, e] of windows) {
    gaps.push(rmsDb(out, at(from + MARGIN), at(s - MARGIN)));
    from = e;
  }
  const mean = (xs: number[]) => xs.reduce((a, b) => a + b, 0) / xs.length;
  return { speechDb: Number(mean(speech).toFixed(2)), gapDb: Number(mean(gaps).toFixed(2)) };
}

async function process(file: string, maker: Maker, windows: readonly [number, number][]) {
  const input = await fixture(file);
  const ctx = new OfflineAudioContext(1, input.length, RATE);
  const buffer = ctx.createBuffer(1, input.length, RATE);
  buffer.copyToChannel(input, 0);
  const source = ctx.createBufferSource();
  source.buffer = buffer;
  const node = await maker(ctx);
  if (node === null) source.connect(ctx.destination);
  else {
    source.connect(node);
    node.connect(ctx.destination);
  }
  source.start();
  // The processors instantiate their WASM asynchronously in their constructor and leave the
  // output silent until then (`process()` skips while `!this.processor`), with no ready message.
  // An offline render outruns that init, so the render must wait for it: before starting, or at a
  // suspension on the first render quantum.
  const pause = () => new Promise((resolve) => setTimeout(resolve, WAIT_MS));
  const started = performance.now();
  if (MODE === 'prestart') await pause();
  else void ctx.suspend(128 / RATE).then(async () => (await pause(), ctx.resume()));
  const out = (await ctx.startRendering()).getChannelData(0);
  const seconds = (performance.now() - started) / 1000;
  return {
    ...score(out, windows),
    realtime: Number((input.length / RATE / seconds).toFixed(1)),
    latencyMs: latencyMs(input, out, (windows[0]?.[0] ?? 0) + 0.2),
  };
}

async function main() {
  const truth = (await (await fetch('/fixtures/speech.truth.json')).json()) as { windows: [number, number][] };
  const results = [];
  for (const [name, maker] of Object.entries(MAKERS)) {
    if (ONLY !== null && name !== 'none' && name !== ONLY) continue;
    results.push({
      name,
      noisy: await process('speech-noisy.wav', maker, truth.windows),
      clean: await process('speech.wav', maker, truth.windows),
    });
  }
  Object.assign(window, { s3Results: results });
}
void main();
