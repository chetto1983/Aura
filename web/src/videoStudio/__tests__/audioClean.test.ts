import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import {
  answeredSilence,
  cleanedFile,
  cleanedName,
  denoiseSamples,
  DENOISE_RATE,
} from '../audioClean';

// RNNoise offline, with the browser stood in for: jsdom has no Web Audio and no WebCodecs. The fake
// renderer answers what RNNoise does to its input — the same samples 992 later (its delay, S3),
// scaled by `gain` the way it lowers noise — or, like a worklet whose WASM is not ready, exact
// zeros; it records when rendering started, so the trim, the silence refusal and the wait for the
// worklet's WASM are each judged against a known answer.

const DELAY = 992;
/** S3's measured noise-floor drop on the noisy-speech fixture. */
const FLOOR_DROP = 10 ** (-37.7 / 20);

const audio = vi.hoisted(() => ({
  silence: false,
  gain: 1,
  renderedAt: [] as number[],
  lengths: [] as number[],
}));

vi.mock('@sapphi-red/web-noise-suppressor', () => ({
  loadRnnoise: () => Promise.resolve(new ArrayBuffer(8)),
  RnnoiseWorkletNode: class {
    connect() {
      return undefined;
    }
  },
}));

const decode = vi.hoisted(() => ({ samples: new Float32Array(0), rates: [] as number[] }));
vi.mock('../audioDecode', () => ({
  decodeMono: (_url: string, rate: number) => {
    decode.rates.push(rate);
    return Promise.resolve(decode.samples);
  },
}));

const encoded = vi.hoisted(() => ({ buffers: [] as { length: number; sampleRate: number }[] }));
vi.mock('mediabunny', () => ({
  Quality: class {
    readonly level: string;
    constructor(level: string) {
      this.level = level;
    }
  },
  OggOutputFormat: class {
    readonly name = 'ogg';
  },
  BufferTarget: class {
    buffer: ArrayBuffer | null = null;
  },
  AudioBufferSource: class {
    add(buffer: { length: number; sampleRate: number }) {
      encoded.buffers.push(buffer);
      return Promise.resolve();
    }
  },
  Output: class {
    private readonly target: { buffer: ArrayBuffer | null };
    constructor(options: { target: { buffer: ArrayBuffer | null } }) {
      this.target = options.target;
    }
    addAudioTrack() {
      return undefined;
    }
    start() {
      return Promise.resolve();
    }
    finalize() {
      this.target.buffer = new Uint8Array([0x4f, 0x67, 0x67, 0x53]).buffer;
      return Promise.resolve();
    }
  },
}));

class FakeContext {
  private input = new Float32Array(0);
  readonly audioWorklet = { addModule: () => Promise.resolve() };
  readonly destination = {};
  constructor(_channels: number, length: number, _rate: number) {
    audio.lengths.push(length);
  }
  createBuffer(_channels: number, length: number) {
    const data = new Float32Array(length);
    return {
      copyToChannel: (samples: Float32Array) => {
        data.set(samples);
        this.input = data;
      },
    };
  }
  createBufferSource() {
    return { buffer: null, connect: () => undefined, start: () => undefined };
  }
  startRendering() {
    audio.renderedAt.push(Date.now());
    const out = new Float32Array(this.input.length + DELAY);
    if (!audio.silence)
      out.set(
        this.input.map((sample) => sample * audio.gain),
        DELAY,
      );
    return Promise.resolve({ getChannelData: () => out });
  }
}

class FakeAudioBuffer {
  readonly length: number;
  readonly sampleRate: number;
  constructor(options: { length: number; sampleRate: number }) {
    this.length = options.length;
    this.sampleRate = options.sampleRate;
  }
  copyToChannel() {
    return undefined;
  }
}

function ramp(length: number): Float32Array<ArrayBuffer> {
  return Float32Array.from({ length }, (_, index) => ((index % 100) - 50) / 100);
}

/** Noise peaking at `dbfs`, the same every run: room tone, a B-roll's hiss. */
function noise(length: number, dbfs: number): Float32Array<ArrayBuffer> {
  const peak = 10 ** (dbfs / 20);
  let seed = 1;
  return Float32Array.from({ length }, () => {
    seed = (seed * 16807) % 2147483647;
    return ((seed / 2147483647) * 2 - 1) * peak;
  });
}

beforeEach(() => {
  vi.useFakeTimers();
  vi.stubGlobal('OfflineAudioContext', FakeContext);
  vi.stubGlobal('AudioBuffer', FakeAudioBuffer);
  audio.silence = false;
  audio.gain = 1;
  audio.renderedAt.length = 0;
  audio.lengths.length = 0;
  decode.rates.length = 0;
  encoded.buffers.length = 0;
});

afterEach(() => {
  vi.useRealTimers();
  vi.unstubAllGlobals();
});

async function settle<T>(work: Promise<T>): Promise<T> {
  await vi.advanceTimersByTimeAsync(1000);
  return work;
}

describe('denoiseSamples', () => {
  it('waits half a second for the worklet’s WASM before it renders', async () => {
    const started = Date.now();
    const work = denoiseSamples(ramp(4800));
    await vi.advanceTimersByTimeAsync(499);
    expect(audio.renderedAt).toHaveLength(0);
    await vi.advanceTimersByTimeAsync(1);
    await work;
    expect((audio.renderedAt[0] ?? 0) - started).toBeGreaterThanOrEqual(500);
  });

  it('trims RNNoise’s delay, so the clean sound starts where the source does and keeps its length', async () => {
    const input = ramp(4800);
    const out = await settle(denoiseSamples(input));
    expect(out).toHaveLength(input.length);
    expect(Array.from(out.subarray(0, 200))).toEqual(Array.from(input.subarray(0, 200)));
    // The render runs a delay longer, or the trim would cut the source's last 20 ms.
    expect(audio.lengths).toEqual([input.length + DELAY]);
  });

  it('refuses silence answered to sound', async () => {
    audio.silence = true;
    const work = denoiseSamples(ramp(4800));
    const refused = expect(work).rejects.toThrow(/silence/);
    await vi.advanceTimersByTimeAsync(1000);
    await refused;
  });

  it('accepts silence answered to silence', async () => {
    audio.silence = true;
    const out = await settle(denoiseSamples(new Float32Array(4800)));
    expect(out).toHaveLength(4800);
  });

  it('keeps quiet noise it cleaned further still, which is not silence', async () => {
    audio.gain = FLOOR_DROP;
    const out = await settle(denoiseSamples(noise(4800, -55)));
    expect(out).toHaveLength(4800);
    expect(out.some((sample) => sample !== 0)).toBe(true);
  });

  it('stops before rendering when its signal is aborted during the wait', async () => {
    const controller = new AbortController();
    const work = denoiseSamples(ramp(4800), controller.signal);
    const refused = expect(work).rejects.toThrow();
    controller.abort();
    await vi.advanceTimersByTimeAsync(1000);
    await refused;
    expect(audio.renderedAt).toHaveLength(0);
  });
});

describe('answeredSilence', () => {
  it('is exact zeros answered to sound: the worklet whose WASM was not ready', () => {
    expect(answeredSilence(noise(4800, -55), new Float32Array(4800))).toBe(true);
  });

  it('is not quiet noise answered to quieter noise', () => {
    const input = noise(4800, -55);
    expect(
      answeredSilence(
        input,
        input.map((sample) => sample * FLOOR_DROP),
      ),
    ).toBe(false);
  });

  it('is not silence answered to silence', () => {
    expect(answeredSilence(new Float32Array(4800), new Float32Array(4800))).toBe(false);
  });
});

describe('cleanedName', () => {
  it('names the copy after the sound’s own file, without its extension', () => {
    expect(cleanedName('voice.mp3')).toBe('voice');
  });

  it('keeps letters, digits, dashes and underscores only', () => {
    expect(cleanedName('The river runs.')).toBe('The-river-runs');
    expect(cleanedName('Registrazione 12:04')).toBe('Registrazione-12-04');
    expect(cleanedName('../../etc/passwd')).toBe('etc-passwd');
    expect(cleanedName('così_è')).toBe('così_è');
  });

  it('keeps a long label to a file name’s length', () => {
    expect(cleanedName('a'.repeat(200))).toHaveLength(80);
  });

  it('is “sound” when the label leaves nothing', () => {
    expect(cleanedName('?!')).toBe('sound');
  });
});

describe('cleanedFile', () => {
  it('decodes at RNNoise’s rate and answers an Ogg Opus file named after its source', async () => {
    decode.samples = ramp(9600);
    const file = await settle(cleanedFile('/api/assets/a/download', 'src-a'));
    expect(decode.rates).toEqual([DENOISE_RATE]);
    expect(file.name).toBe('src-a.clean.ogg');
    expect(file.type).toBe('audio/ogg');
    expect(file.size).toBe(4);
    expect(encoded.buffers).toEqual([expect.objectContaining({ length: 9600, sampleRate: 48000 })]);
  });

  it('names the file after the sound it was asked for, in safe characters', async () => {
    decode.samples = ramp(9600);
    const file = await settle(cleanedFile('/api/assets/a/download', 'Voice over.mp3'));
    expect(file.name).toBe('Voice-over.clean.ogg');
  });
});
