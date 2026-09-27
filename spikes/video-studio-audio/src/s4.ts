import fvadInit from '@echogarden/fvad-wasm';
import fvadWasm from '@echogarden/fvad-wasm/fvad.wasm?url';
import { NonRealTimeVAD } from '@ricky0123/vad-web';
import { loadRnnoise, RnnoiseWorkletNode } from '@sapphi-red/web-noise-suppressor';
import rnnoiseWasm from '@sapphi-red/web-noise-suppressor/rnnoise.wasm?url';
import rnnoiseSimdWasm from '@sapphi-red/web-noise-suppressor/rnnoise_simd.wasm?url';
import rnnoiseWorklet from '@sapphi-red/web-noise-suppressor/rnnoiseWorklet.js?url';
// NonRealTimeVAD imports the default onnxruntime-web entry, which loads the JSEP (WebGPU) build
// with a dynamic import(); the Vite dev server refuses to serve /public files to import(), so
// the runtime gets the package's own files as bundler URLs (`env.wasm.wasmPaths` file form).
import ortMjs from 'onnxruntime-web/ort-wasm-simd-threaded.jsep.mjs?url';
import ortWasm from 'onnxruntime-web/ort-wasm-simd-threaded.jsep.wasm?url';
import { decodeMono } from './wav';

const RATE = 16000;
// Both detectors get the same smoothing so their edges are comparable: a pause shorter than
// MERGE_MS joins its neighbours, a window shorter than MIN_MS is dropped.
const MERGE_MS = 300;
const MIN_MS = 250;
type Window = [number, number];

function smooth(windows: Window[]): Window[] {
  const merged: Window[] = [];
  for (const w of windows) {
    const last = merged.at(-1);
    if (last !== undefined && w[0] - last[1] < MERGE_MS / 1000) last[1] = w[1];
    else merged.push([w[0], w[1]]);
  }
  return merged.filter(([s, e]) => e - s >= MIN_MS / 1000);
}

async function silero(samples: Float32Array): Promise<Window[]> {
  const vad = await NonRealTimeVAD.new({
    modelURL: '/vad/silero_vad_legacy.onnx',
    ortConfig: (ort) => {
      ort.env.wasm.wasmPaths = { mjs: ortMjs, wasm: ortWasm };
      ort.env.wasm.numThreads = 1;
    },
    redemptionMs: MERGE_MS,
    minSpeechMs: MIN_MS,
    preSpeechPadMs: 0,
  });
  const out: Window[] = [];
  for await (const { start, end } of vad.run(samples, RATE)) out.push([start / 1000, end / 1000]);
  return smooth(out);
}

async function webrtc(samples: Float32Array, mode: 0 | 1 | 2 | 3): Promise<Window[]> {
  const m = await fvadInit({ locateFile: () => fvadWasm });
  const handle = m._fvad_new();
  if (m._fvad_set_sample_rate(handle, RATE) !== 0) throw new Error('fvad: sample rate refused');
  if (m._fvad_set_mode(handle, mode) !== 0) throw new Error('fvad: mode refused');
  const frame = (RATE * 30) / 1000;
  const pointer = m._malloc(frame * 2);
  // The Emscripten heap view; its name is what this probe checks the build actually exports.
  const heap: Int16Array = m.HEAP16 ?? new Int16Array(m.HEAPU8.buffer);
  const windows: Window[] = [];
  for (let at = 0; at + frame <= samples.length; at += frame) {
    for (let i = 0; i < frame; i += 1) {
      heap[pointer / 2 + i] = Math.round(Math.max(-1, Math.min(1, samples[at + i] ?? 0)) * 32767);
    }
    if (m._fvad_process(handle, pointer, frame) === 1) windows.push([at / RATE, (at + frame) / RATE]);
  }
  m._free(pointer);
  m._fvad_free(handle);
  return smooth(windows);
}

// S3's RNNoise pass (48 kHz, 500 ms for the worklet's WASM init, its 992-sample delay trimmed),
// then the browser's own resampler down to the VAD's 16 kHz.
const DENOISE_RATE = 48000;
const RNNOISE_DELAY = 992;

async function denoised(bytes: Uint8Array): Promise<Float32Array> {
  const input = await decodeMono(bytes, DENOISE_RATE);
  const ctx = new OfflineAudioContext(1, input.length, DENOISE_RATE);
  const buffer = ctx.createBuffer(1, input.length, DENOISE_RATE);
  buffer.copyToChannel(input, 0);
  const source = ctx.createBufferSource();
  source.buffer = buffer;
  const wasmBinary = await loadRnnoise({ url: rnnoiseWasm, simdUrl: rnnoiseSimdWasm });
  await ctx.audioWorklet.addModule(rnnoiseWorklet);
  const node = new RnnoiseWorkletNode(ctx as unknown as AudioContext, { wasmBinary, maxChannels: 1 });
  source.connect(node);
  node.connect(ctx.destination);
  source.start();
  await new Promise((resolve) => setTimeout(resolve, 500));
  const clean = (await ctx.startRendering()).getChannelData(0).subarray(RNNOISE_DELAY);
  const down = new OfflineAudioContext(1, Math.ceil((clean.length * RATE) / DENOISE_RATE), RATE);
  const cleanBuffer = down.createBuffer(1, clean.length, DENOISE_RATE);
  cleanBuffer.copyToChannel(clean, 0);
  const cleanSource = down.createBufferSource();
  cleanSource.buffer = cleanBuffer;
  cleanSource.connect(down.destination);
  cleanSource.start();
  return (await down.startRendering()).getChannelData(0);
}

function edges(found: Window[], truth: Window[]) {
  const errors = truth.map(([ts, te]) => {
    const hit = found.find(([s, e]) => s < te && e > ts);
    return hit === undefined ? null : [Math.round((hit[0] - ts) * 1000), Math.round((hit[1] - te) * 1000)];
  });
  const falseWindows = found.filter(([s, e]) => !truth.some(([ts, te]) => s < te && e > ts)).length;
  return { errorsMs: errors, misses: errors.filter((e) => e === null).length, falseWindows };
}

async function main() {
  const truth = ((await (await fetch('/fixtures/speech.truth.json')).json()) as { windows: Window[] }).windows;
  const results = [];
  for (const file of ['speech.wav', 'speech-noisy.wav']) {
    const bytes = new Uint8Array(await (await fetch(`/fixtures/${file}`)).arrayBuffer());
    const samples = await decodeMono(bytes, RATE);
    results.push({ file, detector: 'silero', ...edges(await silero(samples), truth) });
    for (const mode of [0, 1, 2, 3] as const) {
      results.push({ file, detector: `webrtc-mode-${mode}`, ...edges(await webrtc(samples, mode), truth) });
    }
    const quiet = await denoised(bytes);
    for (const mode of [0, 1, 2, 3] as const) {
      results.push({ file, detector: `rnnoise+webrtc-mode-${mode}`, ...edges(await webrtc(quiet, mode), truth) });
    }
  }
  Object.assign(window, { s4Results: results });
}
void main();
