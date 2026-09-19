import { beforeEach, describe, expect, it, vi } from 'vitest';

// jsdom has no WebCodecs, so Mediabunny is replaced by a recorder. What is tested is Aura's glue:
// which options reach Conversion.init, what a discarded track does, how Cancel reaches the
// conversion, and what the caller gets back. The real library runs in the E2E suite.
const state = vi.hoisted(() => ({
  initOptions: undefined as unknown,
  discarded: [] as unknown[],
  valid: true,
  cancel: vi.fn<() => Promise<void>>(),
  initGate: undefined as Promise<void> | undefined,
  executeGate: undefined as Promise<void> | undefined,
  executeError: undefined as Error | undefined,
  executed: 0,
  writesBytes: true,
  opened: 0,
  disposed: 0,
  readGate: undefined as Promise<void> | undefined,
  frameGate: undefined as Promise<void> | undefined,
  video: {
    getDisplayWidth: () => Promise.resolve(1280),
    getDisplayHeight: () => Promise.resolve(720),
    canDecode: () => Promise.resolve(true),
  } as unknown,
  audio: {} as unknown,
}));

vi.mock('mediabunny', () => {
  class Input {
    #disposed = false;
    constructor(readonly options: unknown) {
      state.opened += 1;
    }
    getPrimaryVideoTrack = async () => {
      if (state.readGate) await state.readGate;
      return state.video;
    };
    getPrimaryAudioTrack = () => Promise.resolve(state.audio);
    computeDuration = () => Promise.resolve(10);
    // Idempotent, like Mediabunny's own: a second call returns at once.
    dispose = () => {
      if (this.#disposed) return;
      this.#disposed = true;
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
    async *canvasesAtTimestamps(stamps: number[]) {
      for (const timestamp of stamps) {
        if (state.frameGate) await state.frameGate;
        yield { canvas: { timestamp }, timestamp, duration: 0 };
      }
    }
  }
  const Conversion = {
    init: async (options: { output: Output }) => {
      state.initOptions = options;
      if (state.initGate) await state.initGate;
      return {
        isValid: state.valid,
        discardedTracks: state.discarded,
        onProgress: undefined as ((p: number) => void) | undefined,
        cancel: state.cancel,
        async execute(this: { onProgress?: (p: number) => void }) {
          state.executed += 1;
          this.onProgress?.(0.5);
          if (state.executeGate) await state.executeGate;
          // Like Mediabunny's, a cancelled conversion's execute() rejects.
          if (state.cancel.mock.calls.length > 0) throw new Error('ConversionCanceledError');
          if (state.executeError) throw state.executeError;
          if (state.writesBytes) options.output.target.buffer = new ArrayBuffer(8);
        },
      };
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
  state.initOptions = undefined;
  state.discarded = [];
  state.valid = true;
  state.initGate = undefined;
  state.executeGate = undefined;
  state.executeError = undefined;
  state.executed = 0;
  state.writesBytes = true;
  state.cancel.mockReset().mockImplementation(() => Promise.resolve());
  state.opened = 0;
  state.disposed = 0;
  state.readGate = undefined;
  state.frameGate = undefined;
  state.audio = {};
});

/** Lets every pending microtask run. */
function settle(): Promise<void> {
  return new Promise((resolve) => {
    setTimeout(resolve, 0);
  });
}

function gate(): { readonly promise: Promise<void>; readonly open: () => void } {
  let open: () => void = () => undefined;
  const promise = new Promise<void>((resolve) => {
    open = resolve;
  });
  return { promise, open };
}

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

  it('lets go of the file the moment its signal aborts, before the read completes', async () => {
    const read = gate();
    state.readGate = read.promise;
    const controller = new AbortController();
    const probing = probeVideo(new Blob(), controller.signal);
    await settle();
    expect(state.disposed).toBe(0);
    controller.abort();
    expect(state.disposed).toBe(1);
    await expect(probing).rejects.toHaveProperty('name', 'AbortError');
    read.open();
    await settle();
    expect(state.disposed).toBe(1);
  });

  it('opens nothing when its signal is already aborted', async () => {
    const controller = new AbortController();
    controller.abort();
    await expect(probeVideo(new Blob(), controller.signal)).rejects.toHaveProperty(
      'name',
      'AbortError',
    );
    expect(state.opened).toBe(0);
  });
});

describe('filmstrip', () => {
  it('samples the middle of each slot', async () => {
    const frames = await filmstrip(new Blob(), 4, 90);
    expect(frames.map((f) => (f as unknown as { timestamp: number }).timestamp)).toEqual([
      1.25, 3.75, 6.25, 8.75,
    ]);
    expect(state.disposed).toBe(1);
  });

  it('stops decoding the moment its signal aborts, before the frames are drawn', async () => {
    const frame = gate();
    state.frameGate = frame.promise;
    const controller = new AbortController();
    const drawing = filmstrip(new Blob(), 4, 90, controller.signal);
    await settle();
    expect(state.disposed).toBe(0);
    controller.abort();
    expect(state.disposed).toBe(1);
    await expect(drawing).rejects.toHaveProperty('name', 'AbortError');
    frame.open();
    await settle();
    expect(state.disposed).toBe(1);
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

  it('opens nothing when the signal is already aborted', async () => {
    const controller = new AbortController();
    controller.abort();
    await expect(
      exportVideo(new Blob(), 'video/mp4', EDIT, vi.fn(), controller.signal),
    ).resolves.toEqual({ kind: 'canceled' });
    expect(state.initOptions).toBeUndefined();
    expect(state.disposed).toBe(0);
  });

  it('cancels before executing when the signal aborts while the conversion initialises', async () => {
    const init = gate();
    state.initGate = init.promise;
    const controller = new AbortController();
    const running = exportVideo(new Blob(), 'video/mp4', EDIT, vi.fn(), controller.signal);
    controller.abort();
    init.open();
    await expect(running).resolves.toEqual({ kind: 'canceled' });
    expect(state.cancel).toHaveBeenCalledTimes(1);
    expect(state.executed).toBe(0);
    expect(state.disposed).toBe(1);
  });

  it('lets go of the file only once the cancellation has released the output', async () => {
    const cancellation = gate();
    state.cancel.mockImplementation(() => cancellation.promise);
    const execution = gate();
    state.executeGate = execution.promise;
    const controller = new AbortController();
    let settled = false;
    const running = exportVideo(new Blob(), 'video/mp4', EDIT, vi.fn(), controller.signal).then(
      (result) => {
        settled = true;
        return result;
      },
    );
    await settle();
    controller.abort();
    execution.open();
    await settle();
    expect(settled).toBe(false);
    expect(state.disposed).toBe(0);
    cancellation.open();
    await expect(running).resolves.toEqual({ kind: 'canceled' });
    expect(state.disposed).toBe(1);
  });

  it('reports an export that wrote no bytes as empty, and still releases the file', async () => {
    state.writesBytes = false;
    await expect(
      exportVideo(new Blob(), 'video/mp4', EDIT, vi.fn(), new AbortController().signal),
    ).resolves.toEqual({ kind: 'empty' });
    expect(state.disposed).toBe(1);
  });

  it('passes an export error on, and still releases the file', async () => {
    state.executeError = new Error('encoder failed');
    await expect(
      exportVideo(new Blob(), 'video/mp4', EDIT, vi.fn(), new AbortController().signal),
    ).rejects.toThrow('encoder failed');
    expect(state.disposed).toBe(1);
  });
});
