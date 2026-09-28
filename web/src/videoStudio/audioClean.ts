import { loadRnnoise, RnnoiseWorkletNode } from '@sapphi-red/web-noise-suppressor';
import rnnoiseWasm from '@sapphi-red/web-noise-suppressor/rnnoise.wasm?url';
import rnnoiseSimdWasm from '@sapphi-red/web-noise-suppressor/rnnoise_simd.wasm?url';
import rnnoiseWorklet from '@sapphi-red/web-noise-suppressor/rnnoiseWorklet.js?url';
import { AudioBufferSource, BufferTarget, OggOutputFormat, Output, Quality } from 'mediabunny';
import { decodeMono } from './audioDecode';

// audioClean.ts — noise reduction in the browser: RNNoise run offline over a source (S3,
// spikes/video-studio-audio/FINDINGS.md), and the result encoded as Ogg Opus by mediabunny so it is
// stored beside the original and played in its place. Reached only through import() from the
// control that asks for it: the RNNoise WASM is not the editor's first paint. The WASM and the
// worklet are this origin's own files (Vite `?url`), never a CDN's.
//
// RNNoise is a mono model, so a cleaned copy is mono: noise reduction is for speech.

/** RNNoise's model rate; the context runs there and the decoder resamples to it. */
export const DENOISE_RATE = 48000;
/** The worklet instantiates its WASM in its constructor and answers silence until it has, with no
 *  message when it is ready; an offline render outruns that. S3: 0 ms gives silence, 500 ms works. */
const WORKLET_WARMUP_MS = 500;
/** RNNoise's delay at 48 kHz, 20.67 ms (S3): trimmed, so a cleaned clip stays on its picture. */
const RNNOISE_DELAY = 992;

/**
 * Whether RNNoise answered nothing at all to a sound: the worklet whose WASM is not ready yet skips
 * `process()` and leaves its output at exact zeros (workletProcessor.js; S3). Only exact zeros:
 * quiet room tone lowered a further 37.7 dB is small, not silent, and must not be refused.
 */
export function answeredSilence(input: Float32Array, output: Float32Array): boolean {
  return input.some((sample) => sample !== 0) && output.every((sample) => sample === 0);
}

/** `samples` — mono, at DENOISE_RATE — with RNNoise run over them, as long as they were. */
export async function denoiseSamples(
  samples: Float32Array<ArrayBuffer>,
  signal?: AbortSignal,
): Promise<Float32Array<ArrayBuffer>> {
  // A delay longer than the input, or the trim below would cut its last 20 ms.
  const context = new OfflineAudioContext(1, samples.length + RNNOISE_DELAY, DENOISE_RATE);
  const buffer = context.createBuffer(1, samples.length, DENOISE_RATE);
  buffer.copyToChannel(samples, 0);
  const source = context.createBufferSource();
  source.buffer = buffer;
  const wasmBinary = await loadRnnoise({ url: rnnoiseWasm, simdUrl: rnnoiseSimdWasm });
  await context.audioWorklet.addModule(rnnoiseWorklet);
  // Typed for AudioContext; an AudioWorkletNode takes any BaseAudioContext (S3).
  const node = new RnnoiseWorkletNode(context as unknown as AudioContext, {
    wasmBinary,
    maxChannels: 1,
  });
  source.connect(node);
  node.connect(context.destination);
  source.start();
  await new Promise((resolve) => setTimeout(resolve, WORKLET_WARMUP_MS));
  signal?.throwIfAborted();
  const rendered = await context.startRendering();
  const clean = rendered.getChannelData(0).slice(RNNOISE_DELAY, RNNOISE_DELAY + samples.length);
  if (answeredSilence(samples, clean)) {
    throw new Error('videoStudio: noise reduction answered silence to a sound');
  }
  return clean;
}

/** What a cleaned copy is called in the library: its sound's own name without the extension, in
 *  letters, digits, `_` and `-` only — a label may be a whole sentence read aloud — and "sound"
 *  when the label leaves nothing. */
export function cleanedName(label: string): string {
  const safe = label
    .replace(/\.[\p{L}\p{N}]{1,5}$/u, '')
    .replace(/[^\p{L}\p{N}_-]+/gu, '-')
    .slice(0, 80)
    .replace(/^-+|-+$/g, '');
  return safe === '' ? 'sound' : safe;
}

/** A source's cleaned copy as a file to store: decoded, RNNoise, encoded as Ogg Opus, and named
 *  after `name`, the sound's label. */
export async function cleanedFile(url: string, name: string, signal?: AbortSignal): Promise<File> {
  const clean = await denoiseSamples(await decodeMono(url, DENOISE_RATE, signal), signal);
  const buffer = new AudioBuffer({
    length: clean.length,
    numberOfChannels: 1,
    sampleRate: DENOISE_RATE,
  });
  buffer.copyToChannel(clean, 0);
  const target = new BufferTarget();
  const output = new Output({ format: new OggOutputFormat(), target });
  const track = new AudioBufferSource({ codec: 'opus', quality: new Quality('high') });
  output.addAudioTrack(track);
  await output.start();
  await track.add(buffer);
  await output.finalize();
  if (target.buffer === null) throw new Error('videoStudio: the cleaned sound was not written');
  return new File([target.buffer], `${cleanedName(name)}.clean.ogg`, { type: 'audio/ogg' });
}
