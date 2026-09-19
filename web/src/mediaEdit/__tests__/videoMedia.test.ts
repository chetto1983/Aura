import { beforeEach, describe, expect, it, vi } from 'vitest';

// jsdom has no WebCodecs, so Mediabunny is replaced by a recorder. What is tested is Aura's glue:
// which options reach Conversion.init, what a discarded track does, how Cancel reaches the
// conversion, and what the caller gets back. The real library runs in the E2E suite.
const state = vi.hoisted(() => ({
  initOptions: undefined as unknown,
  discarded: [] as unknown[],
  valid: true,
  cancel: vi.fn(),
  executeGate: undefined as Promise<void> | undefined,
  disposed: 0,
  video: {
    getDisplayWidth: () => Promise.resolve(1280),
    getDisplayHeight: () => Promise.resolve(720),
    canDecode: () => Promise.resolve(true),
  } as unknown,
  audio: {} as unknown,
}));

vi.mock('mediabunny', () => {
  class Input {
    constructor(readonly options: unknown) {}
    getPrimaryVideoTrack = () => Promise.resolve(state.video);
    getPrimaryAudioTrack = () => Promise.resolve(state.audio);
    computeDuration = () => Promise.resolve(10);
    dispose = () => {
      state.disposed += 1;
    };
  }
  class Output {
    target: { buffer: ArrayBuffer | null };
    format: { mimeType: string };
    constructor(options: { format: { mimeType: string }; target: { buffer: ArrayBuffer | null } }) {
      this.target = options.target;
      this.format = options.format;
    }
  }
  class BufferTarget {
    buffer: ArrayBuffer | null = null;
  }
  class Mp4OutputFormat {
    mimeType = 'video/mp4';
  }
  class WebMOutputFormat {
    mimeType = 'video/webm';
  }
  class BlobSource {
    constructor(readonly blob: Blob) {}
  }
  class CanvasSink {
    // A plain generator: `for await` reads it like the library's async one.
    *canvasesAtTimestamps(stamps: number[]) {
      for (const timestamp of stamps) yield { canvas: { timestamp }, timestamp, duration: 0 };
    }
  }
  const Conversion = {
    init: (options: { output: Output }) => {
      state.initOptions = options;
      return Promise.resolve({
        isValid: state.valid,
        discardedTracks: state.discarded,
        onProgress: undefined as ((p: number) => void) | undefined,
        cancel: state.cancel,
        async execute(this: { onProgress?: (p: number) => void }) {
          this.onProgress?.(0.5);
          if (state.executeGate) await state.executeGate;
          options.output.target.buffer = new ArrayBuffer(8);
        },
      });
    },
  };
  return {
    ALL_FORMATS: [],
    BlobSource,
    BufferTarget,
    CanvasSink,
    Conversion,
    Input,
    Mp4OutputFormat,
    Output,
    WebMOutputFormat,
  };
});

const { exportVideo, filmstrip, probeVideo } = await import('../videoMedia');

beforeEach(() => {
  state.discarded = [];
  state.valid = true;
  state.executeGate = undefined;
  state.cancel.mockReset();
  state.disposed = 0;
  state.audio = {};
});

const EDIT = { start: 1, end: 3, rotation: 0 as const, mute: false };

describe('probeVideo', () => {
  it('reports duration, display size and audio, and releases the file', async () => {
    await expect(probeVideo(new Blob())).resolves.toEqual({
      duration: 10,
      width: 1280,
      height: 720,
      hasAudio: true,
    });
    state.audio = null;
    await expect(probeVideo(new Blob())).resolves.toMatchObject({ hasAudio: false });
    expect(state.disposed).toBe(2);
  });
});

describe('filmstrip', () => {
  it('samples the middle of each slot', async () => {
    const frames = await filmstrip(new Blob(), 4, 90);
    expect(frames.map((f) => (f as unknown as { timestamp: number }).timestamp)).toEqual([
      1.25, 3.75, 6.25, 8.75,
    ]);
  });
});

describe('exportVideo', () => {
  it('passes the cut to Mediabunny and returns the file with its type', async () => {
    const progress = vi.fn();
    const result = await exportVideo(
      new Blob(),
      'video/mp4',
      EDIT,
      progress,
      new AbortController().signal,
    );
    expect(result.kind).toBe('done');
    expect(result.kind === 'done' ? result.blob.type : '').toBe('video/mp4');
    expect(progress).toHaveBeenCalledWith(0.5);
    expect(state.initOptions).toMatchObject({
      trim: { start: 1, end: 3 },
      copy: { boundaryPolicy: 'expand' },
      showWarnings: false,
    });
  });

  it('writes WebM for a WebM source', async () => {
    const result = await exportVideo(
      new Blob(),
      'video/webm',
      EDIT,
      vi.fn(),
      new AbortController().signal,
    );
    expect(result.kind === 'done' ? result.blob.type : '').toBe('video/webm');
  });

  it('refuses a file the browser would strip of its video', async () => {
    state.discarded = [
      { track: { type: 'video', codec: 'hevc' }, reason: 'undecodable_source_codec' },
    ];
    await expect(
      exportVideo(new Blob(), 'video/mp4', EDIT, vi.fn(), new AbortController().signal),
    ).resolves.toEqual({
      kind: 'blocked',
      tracks: [{ type: 'video', codec: 'hevc', reason: 'undecodable_source_codec' }],
    });
  });

  it('cancels the running conversion when the signal aborts', async () => {
    let release: () => void = () => undefined;
    state.executeGate = new Promise<void>((resolve) => {
      release = resolve;
    });
    const controller = new AbortController();
    const running = exportVideo(new Blob(), 'video/mp4', EDIT, vi.fn(), controller.signal);
    await Promise.resolve();
    await Promise.resolve();
    controller.abort();
    release();
    await expect(running).resolves.toEqual({ kind: 'canceled' });
    expect(state.cancel).toHaveBeenCalledTimes(1);
  });
});
