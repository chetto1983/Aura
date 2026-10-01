import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { exportProjectAudio, NoProjectAudioError } from '../videoflow_exportAudio';
import { ExportSourceError } from '../videoflow_media';
import { installFakes, layer, project, removeFakes, renderer, urls } from './videoflowFakes';

// The sound-only export: the same media and pre-decode as the film (videoflow_export.test.ts),
// then the mix written as a 16-bit WAVE by Mediabunny, which is stood in for here — jsdom has no
// AudioBuffer to encode. What is proven is what this module decides: when there is no file to
// write, when the file came out empty, and that closing the editor stops the encoder.

vi.mock('@videoflow/core', async () => {
  const fakes = await import('./videoflowFakes');
  return {
    default: fakes.FakeVideoFlow,
    get loadedMedia() {
      return fakes.media.cache;
    },
  };
});
vi.mock('@videoflow/renderer-browser', async () => ({
  default: (await import('./videoflowFakes')).FakeRenderer,
}));

const wav = vi.hoisted(() => ({
  outputs: [] as { state: string }[],
  /** The bytes the encoder leaves in its target: the file the export must hand back unchanged. */
  file: new Uint8Array([0x52, 0x49, 0x46, 0x46, 0x24, 0x00, 0x00, 0x00, 0x57, 0x41, 0x56, 0x45]),
  /** The encoder finishes without a file. */
  empty: false,
  /** When set, `start()` waits on it: the moment an abort can land mid-write. */
  started: null as Promise<void> | null,
  /** The encoder refuses the mix. */
  refuses: false,
}));

vi.mock('mediabunny', () => {
  class BufferTarget {
    buffer: ArrayBuffer | null = null;
  }
  class Output {
    state = 'pending';
    private readonly target: BufferTarget;
    constructor(options: { target: BufferTarget }) {
      this.target = options.target;
      wav.outputs.push(this);
    }
    addAudioTrack(): void {
      // The track is the source below; nothing to record.
    }
    async start(): Promise<void> {
      this.state = 'started';
      await wav.started;
    }
    finalize(): Promise<void> {
      this.state = 'finalized';
      if (!wav.empty) this.target.buffer = wav.file.slice().buffer;
      return Promise.resolve();
    }
    cancel(): Promise<void> {
      this.state = 'canceled';
      return Promise.resolve();
    }
  }
  return {
    BufferTarget,
    Output,
    WavOutputFormat: class {
      readonly container = 'wav';
    },
    AudioBufferSource: class {
      add(): Promise<void> {
        return wav.refuses ? Promise.reject(new Error('encoder refused')) : Promise.resolve();
      }
    },
  };
});

beforeEach(() => {
  installFakes();
  wav.outputs.length = 0;
  wav.empty = false;
  wav.started = null;
  wav.refuses = false;
});
afterEach(removeFakes);

describe('exportProjectAudio', () => {
  it('writes the mix as a WAVE file and destroys the renderer', async () => {
    renderer.layers = [layer('/api/assets/asset-a/download')];

    const blob = await exportProjectAudio(project(), urls);

    expect(blob.type).toBe('audio/wav');
    expect(new Uint8Array(await blob.arrayBuffer())).toEqual(wav.file);
    expect(renderer.instances[0]?.destroyed).toBe(1);
  });

  it('says there is nothing to write when the project has no sound', async () => {
    renderer.audio = null;

    await expect(exportProjectAudio(project(), urls)).rejects.toBeInstanceOf(NoProjectAudioError);
    expect(wav.outputs).toHaveLength(0);
  });

  it('stops on a source it cannot fetch, naming its asset, and writes nothing', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn((url: string) => Promise.resolve(new Response(url, { status: 404 }))),
    );
    renderer.layers = [layer('/api/assets/asset-a/download', { type: 'audio' })];

    const failure = exportProjectAudio(project(), urls);

    await expect(failure).rejects.toBeInstanceOf(ExportSourceError);
    await expect(failure).rejects.toMatchObject({ assetId: 'asset-a', failure: 'unreachable' });
    expect(wav.outputs).toHaveLength(0);
    expect(renderer.instances[0]?.destroyed).toBe(1);
  });

  it('refuses an encoder that finished without a file', async () => {
    wav.empty = true;

    await expect(exportProjectAudio(project(), urls)).rejects.toThrow(
      'WAVE encoder produced no file',
    );
  });

  it('cancels the encoder when writing the mix fails, so no half file is left open', async () => {
    wav.refuses = true;

    await expect(exportProjectAudio(project(), urls)).rejects.toThrow('encoder refused');
    expect(wav.outputs[0]?.state).toBe('canceled');
  });

  it('cancels the encoder when the editor closes while it writes', async () => {
    const controller = new AbortController();
    let resume: (() => void) | undefined;
    wav.started = new Promise((resolve) => {
      resume = resolve;
    });

    const running = exportProjectAudio(project(), urls, controller.signal);
    await vi.waitFor(() => {
      expect(wav.outputs[0]?.state).toBe('started');
    });
    controller.abort();
    resume?.();

    await expect(running).rejects.toThrow();
    expect(wav.outputs[0]?.state).toBe('canceled');
    expect(renderer.instances[0]?.destroyed).toBe(1);
  });
});
