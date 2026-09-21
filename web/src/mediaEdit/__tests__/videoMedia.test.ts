import { beforeEach, describe, expect, it, vi } from 'vitest';

const state = vi.hoisted(() => ({
  disposed: 0,
  video: {
    getDisplayWidth: () => Promise.resolve(1280),
    getDisplayHeight: () => Promise.resolve(720),
    canDecode: () => Promise.resolve(true),
  } as unknown,
  audio: {} as unknown,
  gate: undefined as Promise<void> | undefined,
}));

vi.mock('mediabunny', () => ({
  ALL_FORMATS: [],
  BlobSource: class {
    constructor(readonly blob: Blob) {}
  },
  Input: class {
    async getPrimaryVideoTrack() {
      if (state.gate) await state.gate;
      return state.video;
    }
    getPrimaryAudioTrack() {
      return Promise.resolve(state.audio);
    }
    computeDuration() {
      return Promise.resolve(10);
    }
    dispose() {
      state.disposed += 1;
    }
  },
}));

const { probeVideo } = await import('../videoMedia');

beforeEach(() => {
  state.disposed = 0;
  state.gate = undefined;
  state.audio = {};
  state.video = {
    getDisplayWidth: () => Promise.resolve(1280),
    getDisplayHeight: () => Promise.resolve(720),
    canDecode: () => Promise.resolve(true),
  };
});

describe('probeVideo', () => {
  it('reports the track facts and decoder support', async () => {
    await expect(probeVideo(new Blob())).resolves.toEqual({
      duration: 10,
      width: 1280,
      height: 720,
      hasAudio: true,
      decodable: true,
    });
    expect(state.disposed).toBe(1);
  });

  it('refuses a file without a video track', async () => {
    state.video = null;
    await expect(probeVideo(new Blob())).rejects.toThrow('no video track');
    expect(state.disposed).toBe(1);
  });

  it('aborts a probe that is still reading', async () => {
    state.gate = new Promise(() => undefined);
    const controller = new AbortController();
    const reading = probeVideo(new Blob(), controller.signal);
    controller.abort();
    await expect(reading).rejects.toHaveProperty('name', 'AbortError');
    expect(state.disposed).toBeGreaterThan(0);
  });
});
