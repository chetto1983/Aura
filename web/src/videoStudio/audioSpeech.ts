import fvadInit from '@echogarden/fvad-wasm';
import fvadWasm from '@echogarden/fvad-wasm/fvad.wasm?url';
import { denoiseSamples, DENOISE_RATE } from './audioClean';
import { decodeMono } from './audioDecode';
import { speechFromFrames, type SpeechWindow } from './speechWindows';

// audioSpeech.ts — where a source has speech, by S4's pipeline (spikes/video-studio-audio/
// FINDINGS.md): RNNoise first, because WebRTC's VAD alone calls 10 dB-SNR noise speech from end to
// end; then the browser's resampler down to the VAD's 16 kHz; then libfvad mode 3 over 30 ms frames,
// smoothed. Edges within 95 ms on both fixtures. Reached only through import(), like audioClean.ts;
// the WASM is this origin's own file.

const VAD_RATE = 16000;
const FRAME_SECONDS = 0.03;
/** libfvad's most aggressive mode: the fewest false windows on the denoised copy (S4). */
const VAD_MODE = 3;

async function resampled(
  samples: Float32Array<ArrayBuffer>,
  from: number,
  to: number,
): Promise<Float32Array> {
  const context = new OfflineAudioContext(1, Math.ceil((samples.length * to) / from), to);
  const buffer = context.createBuffer(1, samples.length, from);
  buffer.copyToChannel(samples, 0);
  const source = context.createBufferSource();
  source.buffer = buffer;
  source.connect(context.destination);
  source.start();
  return (await context.startRendering()).getChannelData(0);
}

async function voicedFrames(samples: Float32Array): Promise<boolean[]> {
  const vad = await fvadInit({ locateFile: () => fvadWasm });
  const handle = vad._fvad_new();
  const frame = Math.round(VAD_RATE * FRAME_SECONDS);
  const pointer = vad._malloc(frame * 2);
  try {
    if (
      vad._fvad_set_sample_rate(handle, VAD_RATE) !== 0 ||
      vad._fvad_set_mode(handle, VAD_MODE) !== 0
    ) {
      throw new Error('videoStudio: the speech detector refused its settings');
    }
    // Read after the allocation: Emscripten replaces the view only when memory grows, and
    // processing a frame allocates nothing.
    const heap = vad.HEAP16;
    const voiced: boolean[] = [];
    for (let at = 0; at + frame <= samples.length; at += frame) {
      for (let index = 0; index < frame; index += 1) {
        const sample = Math.max(-1, Math.min(1, samples[at + index] ?? 0));
        heap[pointer / 2 + index] = Math.round(sample * 32767);
      }
      voiced.push(vad._fvad_process(handle, pointer, frame) === 1);
    }
    return voiced;
  } finally {
    vad._free(pointer);
    vad._fvad_free(handle);
  }
}

/** Where the sound at `url` has speech, in seconds of its source. */
export async function detectSpeech(url: string, signal?: AbortSignal): Promise<SpeechWindow[]> {
  const clean = await denoiseSamples(await decodeMono(url, DENOISE_RATE, signal), signal);
  signal?.throwIfAborted();
  const voiced = await voicedFrames(await resampled(clean, DENOISE_RATE, VAD_RATE));
  return speechFromFrames(voiced, FRAME_SECONDS);
}
