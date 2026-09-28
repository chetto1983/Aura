import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { detectSpeech } from '../audioSpeech';

// S4's pipeline with its WASM stood in for: RNNoise answers its input, the resampler answers the
// samples it was asked to render at 16 kHz, and the fake libfvad calls a frame voiced when its
// samples are loud — so what is judged is the framing, the heap copy and the order of the steps.

const RATE = 16000;

const steps = vi.hoisted(() => ({
  order: [] as string[],
  refuseMode: false,
  freed: [] as string[],
}));
const input = vi.hoisted(() => ({ samples: new Float32Array(0) }));

vi.mock('../audioDecode', () => ({
  decodeMono: (_url: string, rate: number) => {
    steps.order.push(`decode@${String(rate)}`);
    return Promise.resolve(input.samples);
  },
}));
vi.mock('../audioClean', () => ({
  DENOISE_RATE: 48000,
  denoiseSamples: (samples: Float32Array) => {
    steps.order.push('rnnoise');
    return Promise.resolve(samples);
  },
}));
vi.mock('@echogarden/fvad-wasm', () => ({
  default: () => {
    const heap = new Int16Array(4096);
    return Promise.resolve({
      HEAP16: heap,
      _fvad_new: () => 7,
      _fvad_free: () => {
        steps.freed.push('handle');
      },
      _fvad_set_sample_rate: (_handle: number, rate: number) => (rate === RATE ? 0 : -1),
      _fvad_set_mode: () => (steps.refuseMode ? -1 : 0),
      _malloc: () => 64,
      _free: () => {
        steps.freed.push('frame');
      },
      _fvad_process: (_handle: number, pointer: number, length: number) => {
        let loud = 0;
        for (let index = 0; index < length; index += 1) {
          loud = Math.max(loud, Math.abs(heap[pointer / 2 + index] ?? 0));
        }
        return loud > 1000 ? 1 : 0;
      },
    });
  },
}));

class Resampler {
  private samples = new Float32Array(0);
  readonly destination = {};
  constructor(_channels: number, _length: number, rate: number) {
    steps.order.push(`resample@${String(rate)}`);
  }
  createBuffer() {
    return {
      copyToChannel: (samples: Float32Array) => {
        // Every third sample: 48 kHz down to 16 kHz, as the browser's resampler answers it.
        this.samples = samples.filter((_, index) => index % 3 === 0);
      },
    };
  }
  createBufferSource() {
    return { buffer: null, connect: () => undefined, start: () => undefined };
  }
  startRendering() {
    return Promise.resolve({ getChannelData: () => this.samples });
  }
}

/** `seconds` of 48 kHz silence with a loud stretch from `from` to `to`. */
function withSpeech(seconds: number, from: number, to: number): Float32Array<ArrayBuffer> {
  return Float32Array.from({ length: seconds * 48000 }, (_, index) =>
    index >= from * 48000 && index < to * 48000 ? 0.5 * Math.sin(index / 7) : 0,
  );
}

beforeEach(() => {
  vi.stubGlobal('OfflineAudioContext', Resampler);
  steps.order.length = 0;
  steps.freed.length = 0;
  steps.refuseMode = false;
});

afterEach(() => {
  vi.unstubAllGlobals();
});

describe('detectSpeech', () => {
  it('denoises at 48 kHz, resamples to 16 kHz, and finds the loud stretch within a frame', async () => {
    input.samples = withSpeech(3, 1, 2);
    const found = await detectSpeech('/api/assets/s/download');
    expect(steps.order).toEqual(['decode@48000', 'rnnoise', 'resample@16000']);
    expect(found).toHaveLength(1);
    expect(found[0]?.[0]).toBeCloseTo(0.99, 1);
    expect(found[0]?.[1]).toBeCloseTo(2.01, 1);
    expect(steps.freed.sort()).toEqual(['frame', 'handle']);
  });

  it('answers no speech for silence', async () => {
    input.samples = new Float32Array(48000);
    await expect(detectSpeech('/api/assets/s/download')).resolves.toEqual([]);
  });

  it('is loud about a detector that refuses its settings, and frees what it took', async () => {
    steps.refuseMode = true;
    input.samples = withSpeech(1, 0, 0.5);
    await expect(detectSpeech('/api/assets/s/download')).rejects.toThrow(/refused/);
    expect(steps.freed.sort()).toEqual(['frame', 'handle']);
  });

  it('stops after the noise reduction when its signal is aborted', async () => {
    input.samples = withSpeech(1, 0, 0.5);
    const controller = new AbortController();
    controller.abort();
    await expect(detectSpeech('/api/assets/s/download', controller.signal)).rejects.toThrow();
    expect(steps.order).not.toContain('resample@16000');
  });
});
